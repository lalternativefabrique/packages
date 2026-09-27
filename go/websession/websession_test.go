package websession

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"
)

type stub struct {
	claims svcauth.Claims
	err    error
}

func (s stub) Verify(context.Context, string) (svcauth.Claims, error) { return s.claims, s.err }

func token(t *testing.T, p map[string]any) string {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return "h." + base64.RawURLEncoding.EncodeToString(body) + ".s"
}

func serve(g *Guard, raw string) (int, User, bool) {
	var got User
	var seen bool
	h := g.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, seen = UserFrom(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if raw != "" {
		req.Header.Set("Authorization", "Bearer "+raw)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got, seen
}

func TestAWebSignedTokenNamesTheLocalUserAndItsIdentity(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Issuer: "https://tornad.dev", Subject: "u-1"}}, "tornad")
	code, u, seen := serve(g, token(t, map[string]any{"email": "ana@example", "name": "Ana", "role": "admin", "identityId": "8f3a"}))
	if code != http.StatusOK || !seen {
		t.Fatalf("code=%d seen=%v", code, seen)
	}
	if u.ID != "u-1" || u.IdentityID != "8f3a" || u.Role != "admin" || u.Email != "ana@example" || u.Issuer != "https://tornad.dev" {
		t.Fatalf("user = %+v", u)
	}
}

func TestAnUrbangateTokenNamesTheIdentityAndReadsTheProductRole(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Issuer: "https://id.urbangate.dev", Subject: "8f3a", Roles: []string{"spore:admin", "tornad:user"}}}, "tornad")
	_, u, _ := serve(g, token(t, map[string]any{"sub": "8f3a"}))
	if u.ID != "8f3a" || u.IdentityID != "8f3a" || u.Role != "user" {
		t.Fatalf("user = %+v", u)
	}
	g = NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", Roles: []string{"tornad:admin", "tornad:user"}}}, "tornad")
	if _, u, _ = serve(g, token(t, map[string]any{})); u.Role != "admin" {
		t.Fatalf("admin outranks user, got %q", u.Role)
	}
	g = NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", Roles: []string{"spore:admin"}}}, "tornad")
	if _, u, _ = serve(g, token(t, map[string]any{})); u.Role != "" {
		t.Fatalf("another product's role is no role here, got %q", u.Role)
	}
}

func TestRefusals(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Subject: "u-1"}}, "tornad")
	if code, _, seen := serve(g, ""); code != http.StatusUnauthorized || seen {
		t.Fatalf("no token: code=%d seen=%v", code, seen)
	}
	g = NewWith(stub{err: errors.New("bad signature")}, "tornad")
	if code, _, _ := serve(g, token(t, map[string]any{})); code != http.StatusUnauthorized {
		t.Fatalf("refused token: code=%d", code)
	}
	g = NewWith(stub{claims: svcauth.Claims{}}, "tornad")
	if code, _, _ := serve(g, token(t, map[string]any{})); code != http.StatusUnauthorized {
		t.Fatalf("no subject: code=%d", code)
	}
}

func TestNewNeedsAnIssuer(t *testing.T) {
	if _, err := New(Config{Product: "tornad"}); err == nil {
		t.Fatal("want an error with no issuer")
	}
	for _, cfg := range []Config{
		{Product: "tornad", Urbangate: "https://id.urbangate.dev/"},
		{Product: "tornad", Web: "http://web:5273"},
	} {
		if g, err := New(cfg); err != nil || g == nil {
			t.Fatalf("New(%+v): %v", cfg, err)
		}
	}
}

func TestAWebIssuerBesideUrbangateIsRefused(t *testing.T) {
	_, err := New(Config{Product: "tornad", Urbangate: "https://id.urbangate.dev", Web: "http://web:5273"})
	if !errors.Is(err, ErrWebBesideUrbangate) {
		t.Fatalf("want ErrWebBesideUrbangate, got %v", err)
	}
}

func TestAnIssuerThatCannotBeReachedIs503(t *testing.T) {
	g := NewWith(stub{err: fmt.Errorf("verify: %w", svcauth.ErrUnavailable)}, "tornad")
	code, _, seen := serve(g, token(t, map[string]any{}))
	if code != http.StatusServiceUnavailable || seen {
		t.Fatalf("code=%d seen=%v, want 503 and no handler", code, seen)
	}
}

func TestTheProductTokenCookieIsRead(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", Roles: []string{"tornad:user"}}}, "tornad")
	var got User
	h := g.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, _ = UserFrom(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "tornad_token", Value: token(t, map[string]any{})})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || got.ID != "8f3a" {
		t.Fatalf("code=%d user=%+v", rec.Code, got)
	}
}

func TestRequireRoleTurnsAwayAnyoneWithoutIt(t *testing.T) {
	serveAs := func(roles ...string) int {
		g := NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", Roles: roles}}, "tornad")
		h := g.RequireRole("admin")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token(t, map[string]any{}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := serveAs("tornad:admin"); code != http.StatusOK {
		t.Errorf("admin: code=%d", code)
	}
	if code := serveAs("tornad:user", "spore:admin"); code != http.StatusForbidden {
		t.Errorf("user here, admin elsewhere: code=%d, want 403", code)
	}
}

func TestConfigFromEnvReadsBothIssuers(t *testing.T) {
	env := map[string]string{
		"OIDC_AUDIENCE":       "messag",
		"OIDC_ISSUER_URL":     "https://id.urbangate.dev/",
		"OIDC_WEB_ISSUER_URL": "http://web:5273/",
	}
	got := ConfigFromEnv(func(k string) string { return env[k] })
	want := Config{Product: "messag", Urbangate: "https://id.urbangate.dev", Web: "http://web:5273"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOnlyTheProductsOwnClientMakesASession(t *testing.T) {
	own := NewWith(stub{claims: svcauth.Claims{Issuer: "https://id.urbangate.dev", Subject: "8f3a", ClientID: "partage-admin", Roles: []string{"partage:admin"}}}, "partage")
	if code, u, _ := serve(own, token(t, map[string]any{})); code != http.StatusOK || u.Role != "admin" {
		t.Fatalf("own client: code=%d user=%+v", code, u)
	}
	for _, client := range []string{"nakoda-connect", "lalter-core", "ak_7f3a", "lungor-admin"} {
		g := NewWith(stub{claims: svcauth.Claims{Issuer: "https://id.urbangate.dev", Subject: "8f3a", ClientID: client, Roles: []string{"partage:admin"}}}, "partage")
		if code, _, seen := serve(g, token(t, map[string]any{})); code != http.StatusUnauthorized || seen {
			t.Fatalf("%s made a session: code=%d", client, code)
		}
		if _, err := g.Resolve(context.Background(), token(t, map[string]any{})); !errors.Is(err, ErrNotASession) {
			t.Fatalf("%s: err = %v", client, err)
		}
	}
}

func serveDelegated(g *Guard, scope, raw string) (int, Grant, bool) {
	var got Grant
	var seen bool
	h := g.RequireDelegated(scope)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got, seen = GrantFrom(r.Context()) }))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, got, seen
}

func TestAnAppsGrantIsReadButNeverASession(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", ClientID: "nakoda-connect", Scopes: []string{"lungor:read"}}}, "lungor")
	raw := token(t, map[string]any{"resources": []string{"lungor:tenant:acme"}})
	code, gr, seen := serveDelegated(g, "lungor:read", raw)
	if code != http.StatusOK || !seen || gr.Subject != "8f3a" || gr.ClientID != "nakoda-connect" {
		t.Fatalf("code=%d grant=%+v", code, gr)
	}
	if !gr.Allows("lungor:app:crm", "lungor:tenant:acme") || gr.Allows("lungor:app:crm", "lungor:tenant:globex") {
		t.Fatalf("a granted tenant covers its apps and nothing else: %+v", gr)
	}
	if code, _, _ := serveDelegated(g, "lungor:write", raw); code != http.StatusForbidden {
		t.Fatalf("missing scope: code=%d", code)
	}
}

func TestNotAGrant(t *testing.T) {
	for name, claims := range map[string]svcauth.Claims{
		"own session":   {Subject: "8f3a", ClientID: "lungor-admin"},
		"service token": {Subject: "lalter-core", ClientID: "lalter-core"},
		"web token":     {Subject: "u-1"},
	} {
		g := NewWith(stub{claims: claims}, "lungor")
		if code, _, seen := serveDelegated(g, "", token(t, map[string]any{})); code != http.StatusUnauthorized || seen {
			t.Fatalf("%s: code=%d", name, code)
		}
	}
	g := NewWith(stub{err: fmt.Errorf("jwks: %w", ErrUnavailable)}, "lungor")
	if code, _, _ := serveDelegated(g, "", token(t, map[string]any{})); code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable: code=%d", code)
	}
}
