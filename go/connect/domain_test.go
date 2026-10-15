package connect

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

)

func TestChallengeIsRFC7636S256(t *testing.T) {
	got := Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %s", got)
	}
}

func TestAPendingIsBoundToItsOwnerAndTime(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	p, err := NewPending("org/ada", "partage", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.State) < 43 || len(p.Verifier) < 43 || p.State == p.Verifier {
		t.Fatalf("state %q verifier %q: 32 random bytes each, distinct", p.State, p.Verifier)
	}
	other, _ := NewPending("org/ada", "partage", now)
	if other.State == p.State {
		t.Fatal("two states are the same")
	}
	if err := p.Check("org/ada", now.Add(9*time.Minute)); err != nil {
		t.Fatalf("own state within ttl: %v", err)
	}
	if err := p.Check("org/bob", now); !errors.Is(err, ErrStateOfAnother) {
		t.Fatalf("another owner: %v", err)
	}
	if err := p.Check("org/ada", now.Add(10*time.Minute)); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("after ttl: %v", err)
	}
}

func TestTheAuthorizeURLAsksForOneProduct(t *testing.T) {
	p := Pending{State: "st", Verifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", Product: "lungor"}
	raw := AuthorizeURL("https://id.urbangate.dev/", "host-connect", "https://host.example/api/connect/callback", p, Product{ID: p.Product})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme+"://"+u.Host+u.Path != "https://id.urbangate.dev/oauth2/auth" {
		t.Fatalf("endpoint = %s", raw)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":             "host-connect",
		"response_type":         "code",
		"redirect_uri":          "https://host.example/api/connect/callback",
		"audience":              "lungor",
		"scope":                 "offline_access lungor:read",
		"state":                 "st",
		"code_challenge":        "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		"code_challenge_method": "S256",
		"prompt":                "login consent",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("code_verifier") {
		t.Error("the verifier left the host")
	}
}

func TestAProductNamesItsOwnAudienceAndScopes(t *testing.T) {
	p := Pending{State: "st", Verifier: "v", Product: "partage"}
	raw := AuthorizeURL("https://id.urbangate.dev", "c", "https://h/cb", p, Product{ID: "partage", Audience: "partage-api", Scopes: []string{"partage:publish"}})
	u, _ := url.Parse(raw)
	if u.Query().Get("audience") != "partage-api" || u.Query().Get("scope") != "offline_access partage:publish" {
		t.Fatalf("query = %v", u.Query())
	}
}

func TestOnlyProductsWithACoreAreOffered(t *testing.T) {
	ps := Products{{ID: "partage", CoreURL: "https://api.partagg.fr"}, {ID: "lungor"}}
	if _, ok := ps.Find("lungor"); ok {
		t.Fatal("lungor without a core url is offered")
	}
	if _, ok := ps.Find("messag"); ok {
		t.Fatal("an undeclared product is offered")
	}
	if p, ok := ps.Find("partage"); !ok || p.CoreURL == "" {
		t.Fatal("partage is not offered")
	}
	if got := ps.Offered(); len(got) != 1 || got[0].ID != "partage" {
		t.Fatalf("offered = %+v", got)
	}
}

func TestAnAccessTokenIsRefreshedBeforeItExpires(t *testing.T) {
	now := time.Now()
	c := Connection{AccessToken: "at", AccessExpiresAt: now.Add(10 * time.Minute)}
	if c.FreshAccess(now) != "at" {
		t.Fatal("a token valid ten more minutes is refreshed")
	}
	c.AccessExpiresAt = now.Add(20 * time.Second)
	if c.FreshAccess(now) != "" {
		t.Fatal("a token about to expire is sent")
	}
	if (Connection{}).FreshAccess(now) != "" {
		t.Fatal("no token is sent as one")
	}
}

func ids(ps []Placed) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, fmt.Sprintf("%s@%d", p.ID, p.Depth))
	}
	return out
}

func TestResourcesStandUnderTheirParent(t *testing.T) {
	got := Tree([]Resource{
		{ID: "lungor:app:a1", Type: "app", Parent: "lungor:tenant:t1"},
		{ID: "lungor:tenant:t2", Type: "tenant"},
		{ID: "lungor:tenant:t1", Type: "tenant"},
		{ID: "lungor:app:b1", Type: "app", Parent: "lungor:tenant:t2"},
		{ID: "lungor:app:a2", Type: "app", Parent: "lungor:tenant:t1"},
		{ID: "lungor:app:orphan", Type: "app", Parent: "lungor:tenant:unshared"},
	})
	want := []string{
		"lungor:tenant:t2@0", "lungor:app:b1@1",
		"lungor:tenant:t1@0", "lungor:app:a1@1", "lungor:app:a2@1",
		"lungor:app:orphan@0",
	}
	if !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("tree = %v, want %v", ids(got), want)
	}
}

func TestAParentCycleStillListsEveryResourceOnce(t *testing.T) {
	got := Tree([]Resource{{ID: "a", Parent: "b"}, {ID: "b", Parent: "a"}, {ID: "c", Parent: "c"}})
	if len(got) != 3 {
		t.Fatalf("tree = %v", ids(got))
	}
}

func TestTheSuggestedEntityIsTheOneServingTheResourceSite(t *testing.T) {
	entities := []Entity{
		{ID: "shop", Domains: []string{"shop.example.com"}},
		{ID: "partagg", Domains: []string{"other.fr", "partagg.fr"}},
	}
	cases := []struct {
		name string
		r    Resource
		want string
	}{
		{"www and path folded", Resource{Type: "brand", URL: "https://www.Partagg.fr/blog"}, "partagg"},
		{"subdomain is another site", Resource{Type: "app", URL: "https://app.partagg.fr"}, ""},
		{"no site", Resource{Type: "app"}, ""},
		{"exact host", Resource{Type: "app", URL: "https://shop.example.com:8443"}, "shop"},
	}
	for _, c := range cases {
		if got := Suggest(c.r, entities); got != c.want {
			t.Errorf("%s: suggested %q, want %q", c.name, got, c.want)
		}
	}
}

func notTenant(r Resource) bool { return r.Type != "tenant" }

func TestAChoiceIsMadeOnlyOnAMappableSharedResource(t *testing.T) {
	g := Grant{Resources: []Resource{
		{ID: "lungor:tenant:t", Type: "tenant"},
		{ID: "lungor:app:a", Type: "app", Parent: "lungor:tenant:t"},
	}}
	if _, err := Choosable(g, "lungor:app:a", notTenant); err != nil {
		t.Fatalf("shared app: %v", err)
	}
	if _, err := Choosable(g, "lungor:tenant:t", notTenant); !errors.Is(err, ErrNotMappable) {
		t.Fatalf("tenant: %v", err)
	}
	if _, err := Choosable(g, "lungor:tenant:t", nil); err != nil {
		t.Fatalf("no mappable rule: %v", err)
	}
	if _, err := Choosable(g, "lungor:app:gone", notTenant); !errors.Is(err, ErrUnknownResource) {
		t.Fatalf("resource no longer shared: %v", err)
	}
}

func TestRefusedGrantsDisconnectAndOutagesKeepTheConnection(t *testing.T) {
	outage := fmt.Errorf("%w: partage answered 503", ErrUnavailable)
	cases := []struct {
		name string
		got  Step
		want Step
	}{
		{"refresh ok", AfterRefresh(nil), StepDone},
		{"refresh invalid_grant", AfterRefresh(ErrGrantRefused), StepDisconnected},
		{"refresh outage", AfterRefresh(outage), StepOutage},
		{"product ok", AfterProductCall(nil, true), StepDone},
		{"product 401 on a cached token", AfterProductCall(ErrTokenRefused, true), StepRefreshAndRetry},
		{"product 401 on a fresh token", AfterProductCall(ErrTokenRefused, false), StepDisconnected},
		{"product 503", AfterProductCall(outage, false), StepOutage},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: step %d, want %d", c.name, c.got, c.want)
		}
	}
}

func TestOnlyInvalidGrantIsTheGrantGone(t *testing.T) {
	cases := []struct {
		status int
		body   string
		gone   bool
	}{
		{400, `{"error":"invalid_grant"}`, true},
		{401, `{"error":"invalid_grant"}`, true},
		{401, `{"error":"invalid_client"}`, false},
		{400, `{"error":"invalid_request"}`, false},
		{503, `{"error":"invalid_grant"}`, false},
		{500, `oops`, false},
	}
	for _, c := range cases {
		err := classifyTokenError(c.status, []byte(c.body))
		if errors.Is(err, ErrGrantRefused) != c.gone {
			t.Errorf("%d %s: %v", c.status, c.body, err)
		}
		if !c.gone && !errors.Is(err, ErrUnavailable) {
			t.Errorf("%d %s is not an outage: %v", c.status, c.body, err)
		}
	}
}

func TestASealedTokenOpensOnlyOnItsRow(t *testing.T) {
	s, err := NewAESGCM("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal("refresh-token", "org/ada/partage/refresh")
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == "refresh-token" {
		t.Fatal("not sealed")
	}
	if got, err := s.Open(sealed, "org/ada/partage/refresh"); err != nil || got != "refresh-token" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := s.Open(sealed, "org/bob/partage/refresh"); err == nil {
		t.Fatal("opened on another row")
	}
	if _, err := NewAESGCM("c2hvcnQ="); !errors.Is(err, ErrBadSealKey) {
		t.Fatalf("short key: %v", err)
	}
}

func TestLostIsWhatTheGrantNoLongerReaches(t *testing.T) {
	got := Lost([]string{"a", "b", "c"}, []string{"c", "d"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("lost = %v", got)
	}
	if Lost(nil, []string{"a"}) != nil {
		t.Fatal("nothing reached before, nothing lost")
	}
}
