// Package websession tells a product's core who the signed-in person is.
//
// A person reaches a core with a bearer token that one of two issuers
// signed: urbangate, whose access token names the product as audience and
// carries the roles the token hook put there (ADR 0009), or the product's
// own web, which signs a short token from its Better Auth session with the
// key set it publishes at /api/auth/jwks. The second is the one every core
// verified before ADR 0009; it stays accepted while sessions opened under it
// live, and a core drops it by removing an issuer from its configuration.
package websession

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/lalternative/packages/go/svcauth"
)

// User is the person a request was made for.
type User struct {
	// ID is what the token names as subject: the Kratos identity id for a
	// token urbangate signed, the product's local user id for one the web
	// signed.
	ID string
	// IdentityID is the person's id at urbangate whichever issuer signed,
	// empty for a web-signed token of an account never enrolled there. It
	// is what a customer key is issued against.
	IdentityID string
	Email      string
	Name       string
	// Role is admin, user or empty, for this product.
	Role string
	// Issuer is the URL of whoever signed, so a handler that must tell a
	// web session from an urbangate one can.
	Issuer string
}

// Config names the issuers a core trusts for people and the product whose
// roles it reads.
type Config struct {
	// Product is this product's id, the audience urbangate's tokens carry and
	// the prefix of the roles that count here: "<product>:admin".
	Product string
	// Urbangate is the identity provider's issuer URL. Empty accepts no token
	// it signed.
	Urbangate string
	// Web is the product's own web app, issuer of the tokens it signs from its
	// session, and WebJWKS where its key set is read; empty derives
	// Web + "/api/auth/jwks". An empty Web accepts no such token.
	Web     string
	WebJWKS string
}

// Verifier is what checks a raw token. *svcauth.Verifier is the one New
// wires; a test hands a stub.
type Verifier interface {
	Verify(ctx context.Context, raw string) (svcauth.Claims, error)
}

// ErrUnavailable is a token Resolve could not check because the issuer's
// keys could not be read; Require answers it 503, never 401.
var ErrUnavailable = svcauth.ErrUnavailable

// Guard answers 401 to a request that carries no token one of the issuers
// signed, 503 when it cannot check one, and hands the User to the handlers
// behind it.
type Guard struct {
	verifier Verifier
	product  string
}

// New builds the guard from the issuers Config names. It returns an error
// rather than a guard that refuses everyone when no issuer is configured: a
// core with no one to trust is a deployment mistake, not a policy.
func New(cfg Config) (*Guard, error) {
	var issuers []svcauth.Issuer
	if u := strings.TrimRight(cfg.Urbangate, "/"); u != "" {
		issuers = append(issuers, svcauth.Hydra(u, cfg.Product))
	}
	if w := strings.TrimRight(cfg.Web, "/"); w != "" {
		jwks := cfg.WebJWKS
		if jwks == "" {
			jwks = w + "/api/auth/jwks"
		}
		issuers = append(issuers, svcauth.Issuer{URL: w, JWKSURL: jwks})
	}
	if len(issuers) == 0 {
		return nil, errors.New("websession: no issuer configured")
	}
	v, err := svcauth.New(issuers)
	if err != nil {
		return nil, err
	}
	return &Guard{verifier: v, product: cfg.Product}, nil
}

// NewWith builds the guard on a verifier of the caller's choosing.
func NewWith(v Verifier, product string) *Guard {
	return &Guard{verifier: v, product: product}
}

type ctxKey struct{}

// Require is the middleware.
func (g *Guard) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := g.tokenFrom(r)
		if raw == "" {
			unauthenticated(w)
			return
		}
		u, err := g.Resolve(r.Context(), raw)
		if errors.Is(err, ErrUnavailable) {
			unavailable(w)
			return
		}
		if err != nil {
			unauthenticated(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// UserFrom returns the User Require resolved for this request.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

// Resolve verifies one raw token and reads the person out of it.
func (g *Guard) Resolve(ctx context.Context, raw string) (User, error) {
	claims, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return User{}, err
	}
	if claims.Subject == "" {
		return User{}, errors.New("websession: token has no subject")
	}
	// The profile claims svcauth does not model are read off the payload,
	// which is safe only after Verify checked the signature of this string.
	p, err := payload(raw)
	if err != nil {
		return User{}, err
	}
	u := User{ID: claims.Subject, Email: p.Email, Name: p.Name, Issuer: claims.Issuer}
	switch {
	case p.IdentityID != "":
		u.IdentityID = p.IdentityID
	case len(claims.Roles) > 0 || p.Role == "":
		// urbangate's token: the subject is the identity itself.
		u.IdentityID = claims.Subject
	}
	u.Role = p.Role
	if u.Role == "" {
		u.Role = roleOf(claims.Roles, g.product)
	}
	return u, nil
}

// roleOf reads this product's role out of the roles claim urbangate's token
// hook writes: "<product>:admin" makes an admin, "<product>:user" a user.
func roleOf(roles []string, product string) string {
	role := ""
	for _, r := range roles {
		switch r {
		case product + ":admin":
			return "admin"
		case product + ":user":
			role = "user"
		}
	}
	return role
}

type profile struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	IdentityID string `json:"identityId"`
}

func payload(raw string) (profile, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return profile{}, errors.New("websession: token is not a JWT")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return profile{}, err
	}
	var p profile
	if err := json.Unmarshal(body, &p); err != nil {
		return profile{}, err
	}
	return p, nil
}

// tokenFrom reads the bearer header, then the cookie @lalternative/auth sets
// for this product, then the "token" cookie of the web-signed era.
func (g *Guard) tokenFrom(r *http.Request) string {
	if raw, ok := svcauth.BearerToken(r); ok {
		return raw
	}
	for _, name := range []string{g.product + "_token", "token"} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthenticated"}`))
}

func unavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", svcauth.RetryAfter)
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(`{"error":"identity_provider_unavailable"}`))
}
