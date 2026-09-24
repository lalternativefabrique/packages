package svcauth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/lalternative/packages/go/svcauth"
)

type fakeIssuer struct {
	srv    *httptest.Server
	rsaKey *rsa.PrivateKey
	edKey  ed25519.PrivateKey
	hits   atomic.Int32
	fail   atomic.Bool
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	edPub, edKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{rsaKey: rsaKey, edKey: edKey}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if f.fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{
			{"kty": "RSA", "kid": "rsa-1", "alg": "RS256", "use": "sig",
				"n": base64.RawURLEncoding.EncodeToString(rsaKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(rsaKey.E)).Bytes())},
			{"kty": "OKP", "crv": "Ed25519", "kid": "ed-1", "alg": "EdDSA", "use": "sig",
				"x": base64.RawURLEncoding.EncodeToString(edPub)},
		}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) url() string { return f.srv.URL }

func (f *fakeIssuer) token(t *testing.T, kid string, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = f.url()
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Minute).Unix()
	}
	var tok *jwt.Token
	var key any
	switch kid {
	case "rsa-1":
		tok = jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		key = f.rsaKey
	case "ed-1":
		tok = jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		key = f.edKey
	default:
		tok = jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		key = f.rsaKey
	}
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func verifier(t *testing.T, issuers ...svcauth.Issuer) *svcauth.Verifier {
	t.Helper()
	v, err := svcauth.New(issuers)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHydraClientCredentialsTokenIsAccepted(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	raw := f.token(t, "rsa-1", jwt.MapClaims{
		"sub": "lalter-core", "client_id": "lalter-core",
		"aud": []string{"tornade"}, "scp": []string{"tornade:speak"},
	})
	c, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if c.Subject != "lalter-core" || c.ClientID != "lalter-core" || !c.HasScope("tornade:speak") || c.Issuer != f.url() {
		t.Fatalf("claims = %+v", c)
	}
}

func TestBetterAuthEdDSATokenIsAccepted(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Issuer{URL: f.url(), JWKSURL: f.url() + "/.well-known/jwks.json"})
	raw := f.token(t, "ed-1", jwt.MapClaims{"sub": "user-1", "scope": "read write", "roles": []string{"lungor:admin"}})
	c, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !c.HasScope("write") || !c.HasRole("lungor:admin") {
		t.Fatalf("claims = %+v", c)
	}
}

func TestOwnerClaimIsRead(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url()))
	top := f.token(t, "rsa-1", jwt.MapClaims{"sub": "ak_1", "client_id": "ak_1", "owner": "id-42"})
	c, err := v.Verify(context.Background(), top)
	if err != nil || c.Owner != "id-42" {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}
	ext := f.token(t, "rsa-1", jwt.MapClaims{"sub": "ak_1", "ext": map[string]any{"owner": "id-43"}})
	c, err = v.Verify(context.Background(), ext)
	if err != nil || c.Owner != "id-43" {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}
	none := f.token(t, "rsa-1", jwt.MapClaims{"sub": "lalter-core"})
	c, err = v.Verify(context.Background(), none)
	if err != nil || c.Owner != "" {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}
}

// The jti is what a revocation names a key by: a signed key verifies offline,
// so nothing in the token itself can say it was withdrawn.
func TestJTIIsRead(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url()))

	signed := f.token(t, "rsa-1", jwt.MapClaims{"sub": "ak_1", "jti": "a5a52f36"})
	c, err := v.Verify(context.Background(), signed)
	if err != nil || c.JTI != "a5a52f36" {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}

	// A token issued before signed keys carries none, and that is not an
	// error: it simply cannot be named by a revocation.
	none := f.token(t, "rsa-1", jwt.MapClaims{"sub": "lalter-core"})
	c, err = v.Verify(context.Background(), none)
	if err != nil || c.JTI != "" {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}
}

func TestRolesInHydraExtClaimAreRead(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url()))
	raw := f.token(t, "rsa-1", jwt.MapClaims{"sub": "u", "ext": map[string]any{"roles": []string{"tornade:admin"}}})
	c, err := v.Verify(context.Background(), raw)
	if err != nil || !c.HasRole("tornade:admin") {
		t.Fatalf("claims = %+v, err = %v", c, err)
	}
}

func TestRefusals(t *testing.T) {
	f := newFakeIssuer(t)
	other := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	past := time.Now().Add(-time.Minute).Unix()
	hs := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"iss": f.url(), "sub": "x", "aud": "tornade", "exp": time.Now().Add(time.Minute).Unix()})
	hs.Header["kid"] = "rsa-1"
	confused, _ := hs.SignedString([]byte("public-key-as-secret"))
	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"iss": f.url(), "sub": "x", "aud": "tornade", "exp": time.Now().Add(time.Minute).Unix()})
	unsigned, _ := none.SignedString(jwt.UnsafeAllowNoneSignatureType)

	cases := map[string]struct {
		raw  string
		want error
	}{
		"empty":            {"", svcauth.ErrNoToken},
		"garbage":          {"not.a.jwt", svcauth.ErrInvalidToken},
		"unknown issuer":   {other.token(t, "rsa-1", jwt.MapClaims{"sub": "x", "aud": "tornade"}), svcauth.ErrUnknownIssuer},
		"wrong audience":   {f.token(t, "rsa-1", jwt.MapClaims{"sub": "x", "aud": "spore"}), svcauth.ErrWrongAudience},
		"no audience":      {f.token(t, "rsa-1", jwt.MapClaims{"sub": "x"}), svcauth.ErrWrongAudience},
		"expired":          {f.token(t, "rsa-1", jwt.MapClaims{"sub": "x", "aud": "tornade", "exp": past}), svcauth.ErrInvalidToken},
		"no subject":       {f.token(t, "rsa-1", jwt.MapClaims{"aud": "tornade"}), svcauth.ErrMissingSubject},
		"wrong key signed": {other.token(t, "rsa-1", jwt.MapClaims{"iss": f.url(), "sub": "x", "aud": "tornade"}), svcauth.ErrInvalidToken},
		"alg confusion":    {confused, svcauth.ErrInvalidToken},
		"alg none":         {unsigned, svcauth.ErrInvalidToken},
	}
	for name, tc := range cases {
		_, err := v.Verify(context.Background(), tc.raw)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

func TestUnknownKidInsideTheCooldownDoesNotRefetch(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url()))
	if _, err := v.Verify(context.Background(), f.token(t, "rsa-1", jwt.MapClaims{"sub": "x"})); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := v.Verify(context.Background(), f.token(t, "rsa-9", jwt.MapClaims{"sub": "x"})); !errors.Is(err, svcauth.ErrInvalidToken) {
			t.Fatalf("unknown kid: err = %v", err)
		}
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("jwks fetched %d times, want 1: the keys were fetched seconds ago", got)
	}
}

func TestStaleKeysServeWhileIssuerIsDown(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url()))
	if _, err := v.Verify(context.Background(), f.token(t, "rsa-1", jwt.MapClaims{"sub": "x"})); err != nil {
		t.Fatal(err)
	}
	f.fail.Store(true)
	if _, err := v.Verify(context.Background(), f.token(t, "ed-1", jwt.MapClaims{"sub": "x"})); err != nil {
		t.Fatalf("cached key while issuer down: %v", err)
	}
}

func TestTwoIssuersAreToldApart(t *testing.T) {
	hydra := newFakeIssuer(t)
	better := newFakeIssuer(t)
	v := verifier(t,
		svcauth.Hydra(hydra.url(), "tornade"),
		svcauth.Issuer{URL: better.url(), JWKSURL: better.url() + "/.well-known/jwks.json", Audiences: []string{"tornade-admin"}},
	)
	if _, err := v.Verify(context.Background(), hydra.token(t, "rsa-1", jwt.MapClaims{"sub": "svc", "aud": "tornade"})); err != nil {
		t.Fatalf("hydra: %v", err)
	}
	if _, err := v.Verify(context.Background(), better.token(t, "ed-1", jwt.MapClaims{"sub": "user", "aud": "tornade-admin"})); err != nil {
		t.Fatalf("better auth: %v", err)
	}
	if _, err := v.Verify(context.Background(), better.token(t, "ed-1", jwt.MapClaims{"sub": "user", "aud": "tornade"})); !errors.Is(err, svcauth.ErrWrongAudience) {
		t.Fatalf("audience of another issuer: err = %v", err)
	}
}

func TestRequireMiddleware(t *testing.T) {
	f := newFakeIssuer(t)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	h := svcauth.Require(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := svcauth.ClaimsFrom(r.Context())
		if !ok {
			t.Error("claims missing from context")
		}
		w.Write([]byte(c.Subject))
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: status = %d", rec.Code)
	}
	req.Header.Set("Authorization", "Bearer "+f.token(t, "rsa-1", jwt.MapClaims{"sub": "svc", "aud": "tornade"}))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "svc" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
}

func TestIssuerDownBeforeAnyFetchIsUnavailable(t *testing.T) {
	f := newFakeIssuer(t)
	f.fail.Store(true)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	tok := f.token(t, "rsa-1", jwt.MapClaims{"sub": "x", "aud": "tornade"})
	for i := 0; i < 3; i++ {
		if _, err := v.Verify(context.Background(), tok); !errors.Is(err, svcauth.ErrUnavailable) {
			t.Fatalf("attempt %d: err = %v, want ErrUnavailable", i, err)
		}
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("jwks fetched %d times, want 1: a failed fetch is not retried inside the cooldown", got)
	}
}

func TestExpiredTokenIsInvalidEvenWhileIssuerIsDown(t *testing.T) {
	f := newFakeIssuer(t)
	f.fail.Store(true)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	tok := f.token(t, "rsa-1", jwt.MapClaims{"sub": "x", "aud": "tornade", "exp": time.Now().Add(-time.Minute).Unix()})
	if _, err := v.Verify(context.Background(), tok); !errors.Is(err, svcauth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestRequireAnswers503WhenIssuerIsDown(t *testing.T) {
	f := newFakeIssuer(t)
	f.fail.Store(true)
	v := verifier(t, svcauth.Hydra(f.url(), "tornade"))
	h := svcauth.Require(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler reached with an unchecked token")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+f.token(t, "rsa-1", jwt.MapClaims{"sub": "svc", "aud": "tornade"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("503 without Retry-After")
	}
}
