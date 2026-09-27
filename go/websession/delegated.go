package websession

import (
	"context"
	"errors"
	"net/http"
	"slices"
)

// Grant is what a person let an app do in this product (ADR 0012): the
// scopes and resources they checked on urbangate's consent screen. It never
// makes a User; a handler that reads it acts for Subject only within it.
type Grant struct {
	// Subject is the person who consented, their Kratos identity id.
	Subject string
	// ClientID is the app that holds the grant.
	ClientID  string
	Scopes    []string
	Resources []string
}

func (gr Grant) HasScope(scope string) bool { return slices.Contains(gr.Scopes, scope) }

// Allows reports whether the grant reaches a resource given as its own id
// followed by its ancestors': a granted tenant covers its apps, including the
// ones created after the consent. The product passes the chain it reads from
// its own data, so a resource the person no longer holds is never reached.
func (gr Grant) Allows(chain ...string) bool {
	for _, id := range chain {
		if slices.Contains(gr.Resources, id) {
			return true
		}
	}
	return false
}

// ErrNotDelegated is a token that is not an app's grant: the product's own
// session, or a service token whose subject is the client itself.
var ErrNotDelegated = errors.New("websession: token is not a grant a person gave an app")

// ResolveDelegated verifies one raw token and reads the grant out of it.
func (g *Guard) ResolveDelegated(ctx context.Context, raw string) (Grant, error) {
	claims, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return Grant{}, err
	}
	if claims.ClientID == "" || claims.ClientID == g.SessionClient() ||
		claims.Subject == "" || claims.Subject == claims.ClientID {
		return Grant{}, ErrNotDelegated
	}
	p, err := payload(raw)
	if err != nil {
		return Grant{}, err
	}
	return Grant{
		Subject:   claims.Subject,
		ClientID:  claims.ClientID,
		Scopes:    claims.Scopes,
		Resources: p.Resources,
	}, nil
}

type grantKey struct{}

// RequireDelegated is the middleware for the routes an app reaches with a
// person's grant: 401 for anything else, the product's own session included,
// and 403 when the grant lacks scope.
func (g *Guard) RequireDelegated(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := g.tokenFrom(r)
			if raw == "" {
				unauthenticated(w)
				return
			}
			gr, err := g.ResolveDelegated(r.Context(), raw)
			if errors.Is(err, ErrUnavailable) {
				unavailable(w)
				return
			}
			if err != nil {
				unauthenticated(w)
				return
			}
			if scope != "" && !gr.HasScope(scope) {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), grantKey{}, gr)))
		})
	}
}

// GrantFrom returns the Grant RequireDelegated resolved for this request.
func GrantFrom(ctx context.Context) (Grant, bool) {
	gr, ok := ctx.Value(grantKey{}).(Grant)
	return gr, ok
}
