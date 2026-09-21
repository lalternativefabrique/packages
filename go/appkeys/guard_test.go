package appkeys

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lalternative/packages/go/svcauth"
)

type signer struct {
	key *rsa.PrivateKey
	kid string
}

func newSigner(t *testing.T) *signer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	return &signer{key: key, kid: "ug-keys-test"}
}

func (s *signer) jwks() string {
	n := base64.RawURLEncoding.EncodeToString(s.key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes())
	doc, _ := json.Marshal(map[string]any{
		"keys": []map[string]string{
			{"kty": "RSA", "kid": s.kid, "alg": "RS256", "use": "sig", "n": n, "e": e},
		},
	})
	return string(doc)
}

type keyClaims struct {
	issuer   string
	audience string
	owner    string
	scopes   string
	jti      string
	expires  time.Time
}

func (s *signer) sign(t *testing.T, c keyClaims) string {
	t.Helper()
	if c.expires.IsZero() {
		c.expires = time.Now().Add(365 * 24 * time.Hour)
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   c.issuer,
		"aud":   c.audience,
		"sub":   "ak_test",
		"owner": c.owner,
		"scope": c.scopes,
		"jti":   c.jti,
		"exp":   c.expires.Unix(),
		"iat":   time.Now().Unix(),
	})
	token.Header["kid"] = s.kid
	raw, err := token.SignedString(s.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return raw
}

// guardFixture is a product wired against a fake urbangate that publishes a
// JWKS and a revocation list.
type guardFixture struct {
	keys    *Keys
	signer  *signer
	server  *httptest.Server
	revoked []string
	now     time.Time
}

func newGuardFixture(t *testing.T) *guardFixture {
	t.Helper()
	f := &guardFixture{signer: newSigner(t), now: time.Now()}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/machine/keys/jwks":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(f.signer.jwks()))
		case "/api/machine/revoked":
			entries := make([]map[string]string, 0, len(f.revoked))
			for _, jti := range f.revoked {
				entries = append(entries, map[string]string{"jti": jti, "revoked_at": "2026-09-20T08:00:00Z"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": entries,
				"as_of":   "2026-09-20T08:00:00Z",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.server.Close)

	k, err := New(Config{
		Product:     "tornad",
		Urbangate:   f.server.URL,
		Provisioner: staticToken("machine-token"),
		OwnerOf:     func(*http.Request) (string, bool) { return "8f3a", true },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	k.clock = func() time.Time { return f.now }
	f.keys = k
	return f
}

func (f *guardFixture) loaded(t *testing.T) *guardFixture {
	t.Helper()
	if err := f.keys.load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}
	return f
}

func (f *guardFixture) key(t *testing.T, c keyClaims) string {
	t.Helper()
	if c.issuer == "" {
		c.issuer = f.server.URL
	}
	if c.audience == "" {
		c.audience = "tornad"
	}
	if c.jti == "" {
		c.jti = "jti-1"
	}
	return KeyPrefix("tornad") + f.signer.sign(t, c)
}

func (f *guardFixture) guard(scopes ...string) http.Handler {
	return f.keys.Require(scopes...)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := ClaimsFrom(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"owner": claims.Owner, "jti": claims.JTI})
	}))
}

func call(h http.Handler, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGuardAcceptsAValidKeyAndExposesItsOwner(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	rec := call(f.guard("tornad:search"), f.key(t, keyClaims{
		owner: "8f3a-1c2d", scopes: "tornad:search", jti: "jti-live",
	}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var seen map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &seen)
	if seen["owner"] != "8f3a-1c2d" || seen["jti"] != "jti-live" {
		t.Errorf("claims = %v", seen)
	}
}

func TestGuardRefusesAKeyOfAnotherProduct(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	raw := f.signer.sign(t, keyClaims{issuer: f.server.URL, audience: "tornad", scopes: "tornad:search"})

	for name, bearer := range map[string]string{
		"another product's prefix": "spore_key_" + raw,
		"no prefix at all":         raw,
		"the prefix alone":         KeyPrefix("tornad"),
	} {
		if rec := call(f.guard(), bearer); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, rec.Code)
		}
	}
}

// A key issued for one product must not reach another, whoever signed it.
func TestGuardRefusesAnotherAudience(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	rec := call(f.guard(), f.key(t, keyClaims{audience: "spore", scopes: "spore:send"}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGuardRefusesAKeySignedByAnotherIssuer(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	other := newSigner(t)
	raw := other.sign(t, keyClaims{issuer: f.server.URL, audience: "tornad", scopes: "tornad:search"})

	if rec := call(f.guard(), KeyPrefix("tornad")+raw); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGuardRefusesAMissingScope(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	rec := call(f.guard("tornad:write"), f.key(t, keyClaims{scopes: "tornad:search"}))
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestGuardRefusesARevokedKey(t *testing.T) {
	f := newGuardFixture(t)
	f.revoked = []string{"jti-dead"}
	f.loaded(t)

	rec := call(f.guard(), f.key(t, keyClaims{scopes: "tornad:search", jti: "jti-dead"}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// A key this product cannot check is not a key it may call invalid: telling a
// customer their valid key is invalid sends them rotating it during an outage.
func TestGuardAnswers503WhileTheListHasNeverLoaded(t *testing.T) {
	f := newGuardFixture(t)
	rec := call(f.guard(), f.key(t, keyClaims{scopes: "tornad:search"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	if f.keys.Ready() {
		t.Error("Ready is true before the list ever loaded")
	}
}

// A poll that stopped succeeding must not quietly bring revoked keys back to
// life: past MaxStale the guard refuses rather than trusting an old list.
func TestGuardAnswers503OnceTheListIsStale(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	key := f.key(t, keyClaims{scopes: "tornad:search"})

	if rec := call(f.guard(), key); rec.Code != http.StatusOK {
		t.Fatalf("fresh list: status = %d", rec.Code)
	}
	f.now = f.now.Add(DefaultMaxStale + time.Second)
	if rec := call(f.guard(), key); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("stale list: status = %d, want 503", rec.Code)
	}
	if f.keys.Ready() {
		t.Error("Ready is true with a stale list")
	}
}

func TestGuardRefusesAnExpiredKey(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	rec := call(f.guard(), f.key(t, keyClaims{
		scopes: "tornad:search", expires: time.Now().Add(-time.Hour),
	}))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestGuardRefusesWithoutABearer(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	if rec := call(f.guard(), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestVerifyNamesWhyItRefused(t *testing.T) {
	f := newGuardFixture(t).loaded(t)
	ctx := context.Background()

	if _, err := f.keys.Verify(ctx, "spore_key_x"); err != ErrNotOurKey {
		t.Errorf("wrong prefix: err = %v, want ErrNotOurKey", err)
	}
	key := f.key(t, keyClaims{scopes: "tornad:search"})
	if _, err := f.keys.Verify(ctx, key, "tornad:write"); err != ErrMissingScope {
		t.Errorf("missing scope: err = %v, want ErrMissingScope", err)
	}

	cold := newGuardFixture(t)
	if _, err := cold.keys.Verify(ctx, cold.key(t, keyClaims{})); err != svcauth.ErrRevocationUnknown {
		t.Errorf("never loaded: err = %v, want ErrRevocationUnknown", err)
	}
}
