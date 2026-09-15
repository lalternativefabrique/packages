// Package svcauth verifies bearer tokens issued by the identity providers a
// service trusts. A service holds a list of issuers, each with the JWKS it
// publishes and the audiences the service answers to; a token is accepted when
// one of them signed it for one of those audiences.
package svcauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Issuer struct {
	// URL is the value of the token's iss claim.
	URL string
	// JWKSURL is where the issuer publishes its public keys.
	JWKSURL string
	// Audiences the service accepts from this issuer. Empty accepts any
	// token the issuer signed, whatever it was meant for.
	Audiences []string
}

// Hydra describes an Ory Hydra issuer by its public URL.
func Hydra(issuerURL string, audiences ...string) Issuer {
	base := strings.TrimRight(issuerURL, "/")
	return Issuer{URL: base, JWKSURL: base + "/.well-known/jwks.json", Audiences: audiences}
}

type Claims struct {
	Issuer   string
	Subject  string
	ClientID string
	// Owner is the identity that created the client, when the issuer says so.
	// Hydra's client_credentials tokens have the client id as subject; the
	// suite's token hook adds the owner of a personal key as this claim.
	Owner    string
	Audience []string
	Scopes   []string
	Roles    []string
	Expires  time.Time
}

func (c Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (c Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

var (
	ErrNoToken         = errors.New("svcauth: no bearer token")
	ErrUnknownIssuer   = errors.New("svcauth: unknown issuer")
	ErrInvalidToken    = errors.New("svcauth: invalid token")
	ErrWrongAudience   = errors.New("svcauth: token not meant for this service")
	ErrMissingSubject  = errors.New("svcauth: token has no subject")
	ErrUnsupportedAlg  = errors.New("svcauth: unsupported signing algorithm")
	errUnknownKeyID    = errors.New("svcauth: unknown key id")
	acceptedAlgorithms = []string{jwt.SigningMethodRS256.Alg(), jwt.SigningMethodEdDSA.Alg()}
)

type Verifier struct {
	issuers map[string]*issuerKeys
	now     func() time.Time
}

type Option func(*Verifier)

// WithHTTPClient replaces the client that fetches every issuer's JWKS.
func WithHTTPClient(c *http.Client) Option {
	return func(v *Verifier) {
		for _, ik := range v.issuers {
			ik.client = c
		}
	}
}

func New(issuers []Issuer, opts ...Option) (*Verifier, error) {
	if len(issuers) == 0 {
		return nil, errors.New("svcauth: at least one issuer is required")
	}
	v := &Verifier{issuers: map[string]*issuerKeys{}, now: time.Now}
	for _, is := range issuers {
		url := strings.TrimRight(is.URL, "/")
		if url == "" || is.JWKSURL == "" {
			return nil, fmt.Errorf("svcauth: issuer %q needs a URL and a JWKS URL", is.URL)
		}
		if _, dup := v.issuers[url]; dup {
			return nil, fmt.Errorf("svcauth: issuer %q listed twice", url)
		}
		v.issuers[url] = newIssuerKeys(is)
	}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

type rawClaims struct {
	ClientID  string   `json:"client_id"`
	Scope     string   `json:"scope"`
	ScopeList []string `json:"scp"`
	Roles     []string `json:"roles"`
	Owner     string   `json:"owner"`
	Ext       struct {
		Roles []string `json:"roles"`
		Owner string   `json:"owner"`
	} `json:"ext"`
	jwt.RegisteredClaims
}

// Verify checks the signature, issuer, expiry and audience of a compact JWT.
func (v *Verifier) Verify(ctx context.Context, raw string) (Claims, error) {
	if raw == "" {
		return Claims{}, ErrNoToken
	}
	unverified := jwt.RegisteredClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, &unverified); err != nil {
		return Claims{}, ErrInvalidToken
	}
	ik, ok := v.issuers[strings.TrimRight(unverified.Issuer, "/")]
	if !ok {
		return Claims{}, ErrUnknownIssuer
	}

	var rc rawClaims
	keyFunc := func(refresh bool) jwt.Keyfunc {
		return func(t *jwt.Token) (any, error) {
			kid, _ := t.Header["kid"].(string)
			key, err := ik.keyFor(ctx, kid, refresh)
			if err != nil {
				return nil, err
			}
			if !key.accepts(t.Method) {
				return nil, ErrUnsupportedAlg
			}
			return key.public, nil
		}
	}
	parser := jwt.NewParser(
		jwt.WithValidMethods(acceptedAlgorithms),
		jwt.WithIssuer(unverified.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(v.now),
	)
	_, err := parser.ParseWithClaims(raw, &rc, keyFunc(false))
	if errors.Is(err, errUnknownKeyID) {
		rc = rawClaims{}
		_, err = parser.ParseWithClaims(raw, &rc, keyFunc(true))
	}
	if err != nil {
		if errors.Is(err, ErrUnsupportedAlg) {
			return Claims{}, ErrUnsupportedAlg
		}
		return Claims{}, ErrInvalidToken
	}
	if rc.Subject == "" {
		return Claims{}, ErrMissingSubject
	}
	if !audienceAccepted(ik.audiences, rc.Audience) {
		return Claims{}, ErrWrongAudience
	}
	return Claims{
		Issuer:   ik.url,
		Subject:  rc.Subject,
		ClientID: rc.ClientID,
		Owner:    ownerOf(rc),
		Audience: rc.Audience,
		Scopes:   scopesOf(rc),
		Roles:    rolesOf(rc),
		Expires:  rc.ExpiresAt.Time,
	}, nil
}

func audienceAccepted(want []string, got jwt.ClaimStrings) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		for _, g := range got {
			if w == g {
				return true
			}
		}
	}
	return false
}

func scopesOf(rc rawClaims) []string {
	if len(rc.ScopeList) > 0 {
		return rc.ScopeList
	}
	if rc.Scope == "" {
		return nil
	}
	return strings.Fields(rc.Scope)
}

func ownerOf(rc rawClaims) string {
	if rc.Owner != "" {
		return rc.Owner
	}
	return rc.Ext.Owner
}

func rolesOf(rc rawClaims) []string {
	if len(rc.Roles) > 0 {
		return rc.Roles
	}
	return rc.Ext.Roles
}

// BearerToken reads the compact token of an Authorization: Bearer header.
func BearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	return strings.TrimSpace(h[len(prefix):]), true
}

type claimsKey struct{}

// Require is middleware that refuses a request without a valid bearer token
// and stores its claims in the context for the handler behind it.
func Require(v *Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := BearerToken(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer`)
				http.Error(w, "missing bearer token", http.StatusUnauthorized)
				return
			}
			c, err := v.Verify(r.Context(), raw)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
				http.Error(w, "invalid bearer token", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, c)))
		})
	}
}

// ClaimsFrom returns the claims Require stored, if the request passed it.
func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}
