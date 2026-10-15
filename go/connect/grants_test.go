package connect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

type memStore struct {
	pending map[string]Pending
	conns   map[string]Connection
	rotated int
}

func newMemStore() *memStore {
	return &memStore{pending: map[string]Pending{}, conns: map[string]Connection{}}
}

func key(owner, product string) string { return owner + "|" + product }

func (m *memStore) AddPending(_ context.Context, p Pending) error {
	m.pending[p.State] = p
	return nil
}

func (m *memStore) TakePending(_ context.Context, state string) (Pending, error) {
	p, ok := m.pending[state]
	if !ok {
		return p, ErrUnknownState
	}
	delete(m.pending, state)
	return p, nil
}

func (m *memStore) Save(_ context.Context, c Connection) error {
	k := key(c.Owner, c.Product)
	c.Resources = m.conns[k].Resources
	m.conns[k] = c
	return nil
}

func (m *memStore) Connection(_ context.Context, owner, product string) (Connection, error) {
	c, ok := m.conns[key(owner, product)]
	if !ok {
		return c, ErrNotConnected
	}
	return c, nil
}

func (m *memStore) Connections(_ context.Context, owner string) ([]Connection, error) {
	var out []Connection
	for _, c := range m.conns {
		if c.Owner == owner {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memStore) Rotate(ctx context.Context, owner, product string, fn func(Connection) (Connection, error)) (Connection, error) {
	m.rotated++
	c, err := m.Connection(ctx, owner, product)
	if err != nil {
		return c, err
	}
	out, err := fn(c)
	if err != nil {
		return c, err
	}
	return out, m.Save(ctx, out)
}

func (m *memStore) MarkRevoked(_ context.Context, owner, product string) error {
	k := key(owner, product)
	c := m.conns[k]
	c.Status, c.RefreshToken, c.AccessToken = StatusRevoked, "", ""
	m.conns[k] = c
	return nil
}

func (m *memStore) Delete(_ context.Context, owner, product string) error {
	delete(m.conns, key(owner, product))
	return nil
}

func (m *memStore) Reached(_ context.Context, c Connection, ids []string) error {
	k := key(c.Owner, c.Product)
	stored := m.conns[k]
	stored.Resources = ids
	m.conns[k] = stored
	return nil
}

func (m *memStore) Purge(_ context.Context, owner string) error {
	for k, c := range m.conns {
		if c.Owner == owner {
			delete(m.conns, k)
		}
	}
	return nil
}

type memLinker struct {
	entities []Entity
	links    map[string]Link
	scopes   map[string]string
}

func (l *memLinker) Entities(context.Context, string) ([]Entity, error) { return l.entities, nil }

func (l *memLinker) Entity(_ context.Context, _, id string) (Entity, error) {
	for _, e := range l.entities {
		if e.ID == id {
			return e, nil
		}
	}
	return Entity{}, ErrUnknownEntity
}

func (l *memLinker) Create(_ context.Context, _ string, r Resource) (Entity, error) {
	e := Entity{ID: strings.ToLower(r.Label), Name: r.Label, Domains: []string{Host(r)}}
	for _, x := range l.entities {
		if x.ID == e.ID {
			return Entity{}, ErrEntityExists
		}
	}
	l.entities = append(l.entities, e)
	return e, nil
}

func (l *memLinker) Links(_ context.Context, scope, _ string) (map[string]Link, error) {
	out := map[string]Link{}
	for id, link := range l.links {
		if l.scopes[id] == scope {
			out[id] = link
		}
	}
	return out, nil
}

func (l *memLinker) LinkedElsewhere(_ context.Context, scope, resourceID string) (bool, error) {
	s, ok := l.scopes[resourceID]
	return ok && s != scope && !l.links[resourceID].Ignored, nil
}

func (l *memLinker) SetLink(_ context.Context, scope, _ string, link Link) error {
	l.links[link.ResourceID], l.scopes[link.ResourceID] = link, scope
	return nil
}

func (l *memLinker) ClearLink(_ context.Context, _, _, resourceID string) error {
	delete(l.links, resourceID)
	delete(l.scopes, resourceID)
	return nil
}

type fakeTokens struct {
	refresh   func(string) (Tokens, error)
	exchange  func(code, verifier string) (Tokens, error)
	revokeErr error
	revoked   []string
}

func (f *fakeTokens) Exchange(_ context.Context, code, verifier string) (Tokens, error) {
	return f.exchange(code, verifier)
}

func (f *fakeTokens) Refresh(_ context.Context, refresh string) (Tokens, error) {
	return f.refresh(refresh)
}

func (f *fakeTokens) Revoke(_ context.Context, refresh string) error {
	f.revoked = append(f.revoked, refresh)
	return f.revokeErr
}

type fakeReader struct {
	answer func(access string) (Grant, error)
	calls  []string
}

func (f *fakeReader) Resources(_ context.Context, _ Product, access string) (Grant, error) {
	f.calls = append(f.calls, access)
	return f.answer(access)
}

type harness struct {
	grants  *Grants
	handler *Handler
	store   *memStore
	tokens  *fakeTokens
	reader  *fakeReader
	linker  *memLinker
	owner   string
	now     time.Time
}

const owner = "org-1/ada"

func newHarness(cfg Config, hcfg HandlerConfig) *harness {
	h := &harness{
		store:  newMemStore(),
		tokens: &fakeTokens{},
		reader: &fakeReader{},
		linker: &memLinker{entities: []Entity{{ID: "boutique", Name: "Boutique", Domains: []string{"partagg.fr"}}}, links: map[string]Link{}, scopes: map[string]string{}},
		owner:  owner,
		now:    time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
	}
	cfg.Issuer, cfg.ClientID = "https://id.urbangate.dev", "host-connect"
	cfg.RedirectURI = "https://host.example/api/connect/callback"
	if cfg.Products == nil {
		cfg.Products = Products{{ID: "partage", Label: "Partage", CoreURL: "https://api.partagg.fr"}}
	}
	h.grants = NewGrants(cfg, h.store, h.tokens, ProductClient{}).WithReader(h.reader).WithClock(func() time.Time { return h.now })
	hcfg.Identify = func(echo.Context) (Who, error) { return Who{Owner: owner, Scope: "org-1"}, nil }
	if hcfg.Linker == nil {
		hcfg.Linker = h.linker
	}
	h.handler = NewHandler(h.grants, hcfg)
	return h
}

func defaultHarness() *harness { return newHarness(Config{}, HandlerConfig{}) }

func (h *harness) call(method, target, body string, handler echo.HandlerFunc, product string) *httptest.ResponseRecorder {
	e := echo.New()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if product != "" {
		c.SetParamNames("product")
		c.SetParamValues(product)
	}
	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

func (h *harness) connect(product, access string, expires time.Time) {
	h.store.conns[key(owner, product)] = Connection{
		Owner: owner, Product: product, Status: StatusActive,
		RefreshToken: "r1", AccessToken: access, AccessExpiresAt: expires,
		Scopes: []string{product + ":read"}, ConnectedAt: h.now,
	}
}

func (h *harness) conn() Connection { return h.store.conns[key(owner, "partage")] }

var brand = Resource{ID: "partage:brand:1", Type: "brand", Label: "Partagg", URL: "https://www.partagg.fr"}

func TestAFreshAccessTokenReadsWithoutRefreshing(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(10*time.Minute))
	h.tokens.refresh = func(string) (Tokens, error) { t.Fatal("refreshed"); return Tokens{}, nil }
	h.reader.answer = func(string) (Grant, error) { return Grant{Resources: []Resource{brand}}, nil }

	rec := h.call(http.MethodGet, "/", "", h.handler.Resources, "partage")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out ResourcesDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Resources) != 1 || out.Resources[0].Suggested != "boutique" {
		t.Fatalf("resources = %+v", out.Resources)
	}
	if got := h.conn().Resources; len(got) != 1 || got[0] != brand.ID {
		t.Fatalf("reached = %v", got)
	}
}

func TestAnExpiredAccessTokenIsRefreshedUnderTheLockAndTheRotatedRefreshTokenKept(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(-time.Minute))
	h.tokens.refresh = func(r string) (Tokens, error) {
		if r != "r1" {
			t.Fatalf("refreshed with %s", r)
		}
		return Tokens{Access: "a2", Refresh: "r2", ExpiresAt: h.now.Add(15 * time.Minute)}, nil
	}
	h.reader.answer = func(string) (Grant, error) { return Grant{}, nil }

	if rec := h.call(http.MethodGet, "/", "", h.handler.Resources, "partage"); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if c := h.conn(); c.RefreshToken != "r2" || c.AccessToken != "a2" || h.store.rotated != 1 {
		t.Fatalf("connection = %+v rotated %d", c, h.store.rotated)
	}
	if len(h.reader.calls) != 1 || h.reader.calls[0] != "a2" {
		t.Fatalf("product called with %v", h.reader.calls)
	}
}

func TestTheRefreshedAccessTokenIsKeptInMemory(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "", time.Time{})
	refreshes := 0
	h.tokens.refresh = func(string) (Tokens, error) {
		refreshes++
		return Tokens{Access: "a2", ExpiresAt: h.now.Add(time.Hour)}, nil
	}
	h.reader.answer = func(string) (Grant, error) { return Grant{}, nil }
	for range 2 {
		c := h.conn()
		c.AccessToken = ""
		h.store.conns[key(owner, "partage")] = c
		if _, err := h.grants.Resources(context.Background(), owner, "partage"); err != nil {
			t.Fatal(err)
		}
	}
	if refreshes != 1 {
		t.Fatalf("refreshed %d times", refreshes)
	}
}

func TestInvalidGrantOnRefreshShowsTheAccessRemoved(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "", time.Time{})
	h.tokens.refresh = func(string) (Tokens, error) { return Tokens{}, ErrGrantRefused }

	rec := h.call(http.MethodGet, "/", "", h.handler.Resources, "partage")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "access_removed") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if c := h.conn(); c.Status != StatusRevoked || c.RefreshToken != "" {
		t.Fatalf("connection = %+v", c)
	}
}

func TestInvalidGrantForgetsTheGrantWhenConfigured(t *testing.T) {
	h := newHarness(Config{ForgetRemoved: true}, HandlerConfig{})
	h.connect("partage", "", time.Time{})
	h.tokens.refresh = func(string) (Tokens, error) { return Tokens{}, ErrGrantRefused }
	err := h.grants.Call(context.Background(), owner, "partage", http.MethodGet, "/x", nil, nil)
	if !errors.Is(err, ErrNotConnected) || len(h.store.conns) != 0 {
		t.Fatalf("err %v conns %v", err, h.store.conns)
	}
}

func TestAnOutageKeepsTheConnection(t *testing.T) {
	for name, fail := range map[string]func(h *harness){
		"urbangate": func(h *harness) {
			h.tokens.refresh = func(string) (Tokens, error) {
				return Tokens{}, fmt.Errorf("%w: urbangate 503", ErrUnavailable)
			}
		},
		"product": func(h *harness) {
			h.tokens.refresh = func(string) (Tokens, error) {
				return Tokens{Access: "a2", Refresh: "r2", ExpiresAt: h.now.Add(time.Hour)}, nil
			}
			h.reader.answer = func(string) (Grant, error) {
				return Grant{}, fmt.Errorf("%w: partage answered 503", ErrUnavailable)
			}
		},
	} {
		h := defaultHarness()
		h.connect("partage", "", time.Time{})
		fail(h)
		rec := h.call(http.MethodGet, "/", "", h.handler.Resources, "partage")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d: %s", name, rec.Code, rec.Body)
		}
		if c := h.conn(); c.Status != StatusActive || c.RefreshToken == "" {
			t.Fatalf("%s: connection = %+v", name, c)
		}
	}
}

func TestACachedTokenTheProductRefusesIsRefreshedOnce(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(10*time.Minute))
	h.tokens.refresh = func(string) (Tokens, error) {
		return Tokens{Access: "a2", Refresh: "r2", ExpiresAt: h.now.Add(time.Hour)}, nil
	}
	h.reader.answer = func(access string) (Grant, error) {
		if access == "a1" {
			return Grant{}, ErrTokenRefused
		}
		return Grant{Resources: []Resource{brand}}, nil
	}
	if rec := h.call(http.MethodGet, "/", "", h.handler.Resources, "partage"); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(h.reader.calls) != 2 {
		t.Fatalf("product called %v", h.reader.calls)
	}
}

func TestAFreshTokenStillRefusedMeansTheGrantIsGone(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(time.Hour))
	h.tokens.refresh = func(string) (Tokens, error) {
		return Tokens{Access: "a2", ExpiresAt: h.now.Add(time.Hour)}, nil
	}
	err := h.grants.WithAccess(context.Background(), owner, "partage", func(Product, string) error { return ErrTokenRefused })
	if !errors.Is(err, ErrAccessRemoved) || h.conn().Status != StatusRevoked {
		t.Fatalf("err %v conn %+v", err, h.conn())
	}
}

func (h *harness) start(t *testing.T) Pending {
	t.Helper()
	rec := h.call(http.MethodPost, "/", "", h.handler.Start, "partage")
	if rec.Code != http.StatusOK {
		t.Fatalf("start %d: %s", rec.Code, rec.Body)
	}
	for _, p := range h.store.pending {
		return p
	}
	t.Fatal("no pending state")
	return Pending{}
}

func (h *harness) callback(query string) CallbackDTO {
	rec := h.call(http.MethodGet, "/?"+query, "", h.handler.Callback, "")
	var out CallbackDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestTheCallbackExchangesTheCodeWithTheStateVerifier(t *testing.T) {
	h := defaultHarness()
	p := h.start(t)
	h.tokens.exchange = func(code, verifier string) (Tokens, error) {
		if code != "c1" || verifier != p.Verifier {
			t.Fatalf("exchange %s %s", code, verifier)
		}
		return Tokens{Access: "a1", Refresh: "r1", ExpiresAt: h.now.Add(time.Hour), Scopes: []string{"offline_access", "partage:read"}}, nil
	}
	if out := h.callback("state=" + p.State + "&code=c1"); out.Outcome != "connected" || out.Product != "partage" {
		t.Fatalf("outcome = %+v", out)
	}
	if c := h.conn(); c.Status != StatusActive || c.RefreshToken != "r1" {
		t.Fatalf("connection = %+v", c)
	}
	if out := h.callback("state=" + p.State + "&code=c1"); out.Outcome != "invalid" {
		t.Fatalf("a state served twice: %+v", out)
	}
}

func TestTheCallbackRedirectsToTheAppWhenConfigured(t *testing.T) {
	h := newHarness(Config{}, HandlerConfig{CallbackRedirect: "/settings/connections"})
	p := h.start(t)
	rec := h.call(http.MethodGet, "/?state="+p.State+"&error=access_denied", "", h.handler.Callback, "")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/settings/connections?partage=error" {
		t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestARefusalOnUrbangateStoresNothing(t *testing.T) {
	h := defaultHarness()
	p := h.start(t)
	if out := h.callback("state=" + p.State + "&error=access_denied"); out.Outcome != "refused" {
		t.Fatalf("outcome = %+v", out)
	}
	if len(h.store.conns) != 0 {
		t.Fatalf("stored %+v", h.store.conns)
	}
}

func TestDisconnectingRevokesAtUrbangateFirst(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(time.Hour))

	h.tokens.revokeErr = fmt.Errorf("%w: urbangate 503", ErrUnavailable)
	if rec := h.call(http.MethodDelete, "/", "", h.handler.Disconnect, "partage"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d", rec.Code)
	}
	if h.conn().RefreshToken != "r1" {
		t.Fatal("an unrevoked grant was forgotten")
	}

	h.tokens.revokeErr = nil
	if rec := h.call(http.MethodDelete, "/", "", h.handler.Disconnect, "partage"); rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if len(h.store.conns) != 0 {
		t.Fatalf("conns %v", h.store.conns)
	}
}

func TestPurgeRevokesAndErasesEveryGrant(t *testing.T) {
	h := newHarness(Config{Products: Products{
		{ID: "partage", CoreURL: "https://p"}, {ID: "lungor", CoreURL: "https://l"},
	}}, HandlerConfig{})
	h.connect("partage", "a1", h.now.Add(time.Hour))
	h.connect("lungor", "a1", h.now.Add(time.Hour))
	h.tokens.revokeErr = fmt.Errorf("%w: down", ErrUnavailable)
	if err := h.grants.Purge(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	if len(h.store.conns) != 0 || len(h.tokens.revoked) != 2 {
		t.Fatalf("conns %v revoked %v", h.store.conns, h.tokens.revoked)
	}
}

func TestCreatingAnEntityFromAResourceLinksIt(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(time.Hour))
	app := Resource{ID: "lungor:app:9", Type: "app", Label: "Atelier", URL: "https://atelier.example.org/"}
	h.reader.answer = func(string) (Grant, error) { return Grant{Resources: []Resource{app}}, nil }

	rec := h.call(http.MethodPut, "/", `{"resource_id":"lungor:app:9","choice":"create"}`, h.handler.SetLink, "partage")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"app":"atelier"`) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if l := h.linker.links["lungor:app:9"]; l.Entity != "atelier" {
		t.Fatalf("link %+v", l)
	}
}

func TestAResourceFeedsTheEntitiesOfOneScope(t *testing.T) {
	h := defaultHarness()
	h.connect("partage", "a1", h.now.Add(time.Hour))
	h.linker.links[brand.ID], h.linker.scopes[brand.ID] = Link{ResourceID: brand.ID, Entity: "autre"}, "org-2"
	h.reader.answer = func(string) (Grant, error) { return Grant{Resources: []Resource{brand}}, nil }

	rec := h.call(http.MethodPut, "/", `{"resource_id":"partage:brand:1","choice":"link","app":"boutique"}`, h.handler.SetLink, "partage")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "linked_elsewhere") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestAnUnmappableResourceIsListedButNotChoosable(t *testing.T) {
	h := newHarness(Config{}, HandlerConfig{Mappable: notTenant})
	h.connect("partage", "a1", h.now.Add(time.Hour))
	tenant := Resource{ID: "t", Type: "tenant", URL: "https://partagg.fr"}
	h.reader.answer = func(string) (Grant, error) { return Grant{Resources: []Resource{tenant}}, nil }

	var out ResourcesDTO
	_ = json.Unmarshal(h.call(http.MethodGet, "/", "", h.handler.Resources, "partage").Body.Bytes(), &out)
	if out.Resources[0].Mappable || out.Resources[0].Suggested != "" {
		t.Fatalf("resource = %+v", out.Resources[0])
	}
	rec := h.call(http.MethodPut, "/", `{"resource_id":"t","choice":"ignore"}`, h.handler.SetLink, "partage")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "not_an_app") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestWithoutAStoreEveryRouteButTheListIsUnavailable(t *testing.T) {
	g := NewGrants(Config{Products: Products{{ID: "partage", Label: "Partage", CoreURL: "https://p"}}}, nil, nil, ProductClient{})
	h := NewHandler(g, HandlerConfig{Identify: func(echo.Context) (Who, error) { return Who{Owner: owner}, nil }})
	hn := &harness{}
	rec := hn.call(http.MethodGet, "/", "", h.List, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("list %d: %s", rec.Code, rec.Body)
	}
	if rec := hn.call(http.MethodPost, "/", "", h.Start, "partage"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("start %d", rec.Code)
	}
	if rec := hn.call(http.MethodPost, "/", "", h.Start, "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown product %d", rec.Code)
	}
}
