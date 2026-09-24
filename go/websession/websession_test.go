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
	g, err := New(Config{Product: "tornad", Urbangate: "https://id.urbangate.dev/", Web: "https://tornad.dev"})
	if err != nil || g == nil {
		t.Fatalf("New: %v", err)
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
