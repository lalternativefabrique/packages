package botguard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef")

func cheapPolicy() Policy {
	p := DefaultPolicy()
	p.PerMinute = 60
	p.Burst = 2
	p.CeilingFactor = 2
	p.Cost = 1
	p.MinCounter = 1
	p.MaxCounter = 20
	return p
}

func newGuard(t *testing.T, mutate func(*Config)) *Guard {
	t.Helper()
	cfg := Config{Audience: "tornad", Secret: testSecret, Settings: StaticSettings(cheapPolicy())}
	if mutate != nil {
		mutate(&cfg)
	}
	g, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func app(g *Guard) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(PathPrefix, g.Handler())
	mux.Handle("/", g.Protect(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	return mux
}

func call(h http.Handler, method, path, remote string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = remote
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func solvedPayload(t *testing.T, h http.Handler, remote string) string {
	t.Helper()
	w := call(h, http.MethodGet, PathPrefix+"challenge", remote, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("challenge: %d %s", w.Code, w.Body)
	}
	var c altcha.Challenge
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	sol, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: c, DeriveKey: altcha.DeriveKeyPBKDF2()})
	if err != nil || sol == nil {
		t.Fatalf("solve: %v", err)
	}
	raw, _ := json.Marshal(altcha.Payload{Challenge: c, Solution: *sol})
	return base64.StdEncoding.EncodeToString(raw)
}

func verify(h http.Handler, remote, payload string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, PathPrefix+"verify", strings.NewReader(`{"altcha":"`+payload+`"}`))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func tokenFrom(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil || v.Token == "" {
		t.Fatalf("no token in %d %s", w.Code, w.Body)
	}
	return v.Token
}

func withToken(tok string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set(HeaderName, tok) }
}

func TestBudgetThenChallengeThenToken(t *testing.T) {
	p := cheapPolicy()
	p.CeilingFactor = 10
	h := app(newGuard(t, func(c *Config) { c.Settings = StaticSettings(p) }))
	const remote = "203.0.113.7:4000"

	for i := range 2 {
		if w := call(h, http.MethodGet, "/search", remote, nil); w.Code != http.StatusOK {
			t.Fatalf("request %d within budget: %d", i, w.Code)
		}
	}
	w := call(h, http.MethodGet, "/search", remote, nil)
	if w.Code != http.StatusPreconditionRequired || !strings.Contains(w.Body.String(), "challenge_required") {
		t.Fatalf("over budget: %d %s", w.Code, w.Body)
	}

	vw := verify(h, remote, solvedPayload(t, h, remote))
	if vw.Code != http.StatusOK {
		t.Fatalf("verify: %d %s", vw.Code, vw.Body)
	}
	if c := vw.Result().Cookies(); len(c) != 1 || c[0].Name != CookieName || !c[0].HttpOnly || !c[0].Secure {
		t.Fatalf("cookie: %+v", c)
	}
	tok := tokenFrom(t, vw)

	if w := call(h, http.MethodGet, "/search", remote, withToken(tok)); w.Code != http.StatusOK {
		t.Fatalf("with token: %d", w.Code)
	}
	if w := call(h, http.MethodGet, "/search", remote, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	}); w.Code != http.StatusOK {
		t.Fatalf("with cookie: %d", w.Code)
	}
}

func TestCeilingRefusesTokenHolders(t *testing.T) {
	h := app(newGuard(t, nil))
	const remote = "203.0.113.8:1"
	tok := tokenFrom(t, verify(h, remote, solvedPayload(t, h, remote)))
	codes := map[int]int{}
	for range 6 {
		codes[call(h, http.MethodGet, "/search", remote, withToken(tok)).Code]++
	}
	if codes[http.StatusTooManyRequests] == 0 || codes[http.StatusOK] != 4 {
		t.Fatalf("codes: %v", codes)
	}
}

func TestSolutionIsSingleUse(t *testing.T) {
	h := app(newGuard(t, nil))
	const remote = "203.0.113.9:1"
	p := solvedPayload(t, h, remote)
	if w := verify(h, remote, p); w.Code != http.StatusOK {
		t.Fatalf("first: %d", w.Code)
	}
	if w := verify(h, remote, p); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "challenge_used") {
		t.Fatalf("replay: %d %s", w.Code, w.Body)
	}
}

func TestForgedAndMalformedSolutions(t *testing.T) {
	g := newGuard(t, nil)
	h := app(g)
	const remote = "203.0.113.10:1"

	other := app(newGuard(t, func(c *Config) { c.Secret = []byte("ffffffffffffffffffffffffffffffff") }))
	if w := verify(h, remote, solvedPayload(t, other, remote)); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("foreign challenge: %d %s", w.Code, w.Body)
	}
	if w := verify(h, remote, "not-base64!"); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed: %d", w.Code)
	}
}

func TestTokenIsBoundToAudienceNetworkAndTime(t *testing.T) {
	g := newGuard(t, nil)
	now := time.Unix(1_800_000_000, 0)
	tok := g.keys.issueToken("tornad", "203.0.113.0/24", time.Minute, now)

	cases := []struct {
		name, aud, net string
		at             time.Time
		want           error
	}{
		{"valid", "tornad", "203.0.113.0/24", now, nil},
		{"other app", "lalter", "203.0.113.0/24", now, errTokenAudience},
		{"other network", "tornad", "198.51.100.0/24", now, errTokenNetwork},
		{"expired", "tornad", "203.0.113.0/24", now.Add(time.Minute), errTokenExpired},
	}
	for _, c := range cases {
		if err := g.keys.checkToken(tok, c.aud, c.net, c.at); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
	tampered := strings.Replace(tok, tok[:4], "AAAA", 1)
	if err := g.keys.checkToken(tampered, "tornad", "203.0.113.0/24", now); !errors.Is(err, errTokenSignature) {
		t.Errorf("tampered: %v", err)
	}
	if err := g.keys.checkToken("garbage", "tornad", "203.0.113.0/24", now); !errors.Is(err, errTokenMalformed) {
		t.Errorf("garbage: %v", err)
	}
}

func TestTokenFromAnotherNetworkIsRefused(t *testing.T) {
	h := app(newGuard(t, nil))
	tok := tokenFrom(t, verify(h, "203.0.113.11:1", solvedPayload(t, h, "203.0.113.11:1")))
	const elsewhere = "198.51.100.1:1"
	for range 2 {
		call(h, http.MethodGet, "/", elsewhere, nil)
	}
	if w := call(h, http.MethodGet, "/", elsewhere, withToken(tok)); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("token replayed elsewhere: %d", w.Code)
	}
}

func TestObserveModeRefusesNothing(t *testing.T) {
	p := cheapPolicy()
	p.Enforce = false
	h := app(newGuard(t, func(c *Config) { c.Settings = StaticSettings(p) }))
	for i := range 10 {
		if w := call(h, http.MethodGet, "/", "203.0.113.12:1", nil); w.Code != http.StatusOK {
			t.Fatalf("request %d: %d", i, w.Code)
		}
	}
}

func TestExemptAndSubject(t *testing.T) {
	h := app(newGuard(t, func(c *Config) {
		c.Exempt = func(r *http.Request) bool { return r.Header.Get("Authorization") != "" }
		c.Subject = func(r *http.Request) string { return r.Header.Get("X-Account") }
	}))
	for range 10 {
		if w := call(h, http.MethodGet, "/", "203.0.113.13:1", func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer x")
		}); w.Code != http.StatusOK {
			t.Fatalf("exempt: %d", w.Code)
		}
	}
	as := func(acct string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("X-Account", acct) }
	}
	for range 2 {
		call(h, http.MethodGet, "/", "203.0.113.14:1", as("alice"))
	}
	if w := call(h, http.MethodGet, "/", "203.0.113.14:1", as("bob")); w.Code != http.StatusOK {
		t.Fatalf("bob shares alice's budget: %d", w.Code)
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Take(context.Context, string, Policy) (Spend, error) {
	return Spend{}, errors.New("store down")
}

func TestFailOpen(t *testing.T) {
	g := newGuard(t, func(c *Config) { c.Limiter = brokenLimiter{} })
	if w := call(app(g), http.MethodGet, "/", "203.0.113.15:1", nil); w.Code != http.StatusOK {
		t.Fatalf("fail open: %d", w.Code)
	}
	var b strings.Builder
	g.WriteMetrics(&b)
	if !strings.Contains(b.String(), `botguard_fail_open_total{audience="tornad"} 1`) {
		t.Fatalf("metrics:\n%s", b.String())
	}
}

func TestInterstitialForBrowserNavigation(t *testing.T) {
	h := app(newGuard(t, func(c *Config) { c.Interstitial = true }))
	html := func(r *http.Request) { r.Header.Set("Accept", "text/html,application/xhtml+xml") }
	for range 2 {
		call(h, http.MethodGet, "/", "203.0.113.16:1", html)
	}
	w := call(h, http.MethodGet, "/", "203.0.113.16:1", html)
	if w.Code != http.StatusPreconditionRequired || !strings.Contains(w.Body.String(), "<altcha-widget") {
		t.Fatalf("interstitial: %d %s", w.Code, w.Body)
	}
	if js := call(h, http.MethodGet, PathPrefix+"widget.js", "203.0.113.16:1", nil); js.Code != http.StatusOK || js.Body.Len() < 10_000 {
		t.Fatalf("widget: %d %d bytes", js.Code, js.Body.Len())
	}
}

func TestClientAddr(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cases := []struct {
		name, remote, xff, want string
	}{
		{"direct client ignores header", "203.0.113.1:1", "1.2.3.4", "203.0.113.1"},
		{"trusted proxy", "10.0.0.2:1", "198.51.100.9", "198.51.100.9"},
		{"spoofed left hop skipped", "10.0.0.2:1", "1.2.3.4, 198.51.100.9, 10.0.0.3", "198.51.100.9"},
		{"garbage header", "10.0.0.2:1", "nope", "10.0.0.2"},
		{"ipv4-mapped", "[::ffff:203.0.113.5]:1", "", "203.0.113.5"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := clientAddr(r, trusted).String(); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	if n := network(netip.MustParseAddr("2001:db8:1:2:3::4")); n != "2001:db8:1:2::/64" {
		t.Errorf("ipv6 network: %s", n)
	}
}

func TestCheckForm(t *testing.T) {
	g := newGuard(t, nil)
	base := time.Unix(1_800_000_000, 0)
	g.now = func() time.Time { return base }
	stamp := g.FormStamp()

	form := func(v url.Values) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_ = r.ParseForm()
		return r
	}
	g.now = func() time.Time { return base.Add(time.Second) }
	if err := g.CheckForm(form(url.Values{StampField: {stamp}})); !errors.Is(err, ErrTooFast) {
		t.Errorf("too fast: %v", err)
	}
	g.now = func() time.Time { return base.Add(5 * time.Second) }
	if err := g.CheckForm(form(url.Values{StampField: {stamp}})); err != nil {
		t.Errorf("human: %v", err)
	}
	if err := g.CheckForm(form(url.Values{StampField: {stamp}, HoneypotField: {"x"}})); !errors.Is(err, ErrHoneypot) {
		t.Errorf("honeypot: %v", err)
	}
	if err := g.CheckForm(form(url.Values{StampField: {"1.forged"}})); !errors.Is(err, ErrStamp) {
		t.Errorf("forged: %v", err)
	}
}

func TestNewRejectsWeakConfig(t *testing.T) {
	if _, err := New(Config{Audience: "x", Secret: []byte("short")}); err == nil {
		t.Error("short secret accepted")
	}
	if _, err := New(Config{Secret: testSecret}); err == nil {
		t.Error("empty audience accepted")
	}
}

func TestClientAddrOverride(t *testing.T) {
	fromHeader := AddrFrom(func(r *http.Request) string { return r.Header.Get("X-Real-Client") })
	h := app(newGuard(t, func(c *Config) { c.ClientAddr = fromHeader }))
	as := func(ip string) func(*http.Request) {
		return func(r *http.Request) { r.Header.Set("X-Real-Client", ip) }
	}
	for range 2 {
		call(h, http.MethodGet, "/", "10.0.0.1:1", as("203.0.113.20"))
	}
	if w := call(h, http.MethodGet, "/", "10.0.0.1:1", as("203.0.113.20")); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("same client over budget: %d", w.Code)
	}
	if w := call(h, http.MethodGet, "/", "10.0.0.1:1", as("198.51.100.20")); w.Code != http.StatusOK {
		t.Fatalf("other client behind the same proxy: %d", w.Code)
	}
}

type brokenSettings struct{}

func (brokenSettings) Policy(context.Context) (Policy, error) { return Policy{}, errors.New("db down") }

func TestUnreadableSettingsNeverEnforce(t *testing.T) {
	h := app(newGuard(t, func(c *Config) { c.Settings = brokenSettings{} }))
	for i := range 50 {
		if w := call(h, http.MethodGet, "/", "203.0.113.30:1", nil); w.Code != http.StatusOK {
			t.Fatalf("request %d refused while settings are unreadable: %d", i, w.Code)
		}
	}
}
