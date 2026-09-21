package appkeys

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lalternative/packages/go/svcauth"
)

// ErrListStale is a revocation list too old to be trusted. It is not a token
// problem: the caller's key may well be fine, and the answer is 503.
var ErrListStale = errors.New("appkeys: revocation list is stale")

type freshness struct {
	mu sync.RWMutex
	at time.Time
}

func (f *freshness) mark(now time.Time) {
	f.mu.Lock()
	f.at = now
	f.mu.Unlock()
}

func (f *freshness) age(now time.Time) (time.Duration, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.at.IsZero() {
		return 0, false
	}
	return now.Sub(f.at), true
}

// Run loads the revocation list and keeps it fresh until ctx ends. A product
// starts it before it serves, and Require refuses keys until it has answered
// once.
//
// svcauth's list knows whether it ever loaded; it does not say when. The
// staleness rule needs that, so each successful load is timestamped here.
func (k *Keys) Run(ctx context.Context) error {
	if err := k.load(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(k.refreshEvery())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// A failed refresh is not fatal: the list keeps serving until it
			// ages past MaxStale, which is what the guard then refuses on.
			_ = k.load(ctx)
		}
	}
}

func (k *Keys) load(ctx context.Context) error {
	if err := k.list.Load(ctx); err != nil {
		return err
	}
	k.fresh.mark(k.clock())
	return nil
}

func (k *Keys) refreshEvery() time.Duration {
	every := k.maxStale / 3
	if every < time.Second {
		every = time.Second
	}
	return every
}

// Ready reports whether this product can honour a customer key: the list has
// loaded and has not aged out. A healthcheck reads it.
func (k *Keys) Ready() bool { return k.listUsable() == nil }

func (k *Keys) listUsable() error {
	if !k.list.Loaded() {
		return svcauth.ErrRevocationUnknown
	}
	age, ok := k.fresh.age(k.clock())
	if !ok || age > k.maxStale {
		return ErrListStale
	}
	return nil
}

// Verify checks one key string as Require does, for a product that guards its
// own routes. The returned claims carry Owner, ClientID and Scopes.
func (k *Keys) Verify(ctx context.Context, key string, scopes ...string) (svcauth.Claims, error) {
	raw, ok := strings.CutPrefix(key, k.prefix)
	if !ok || raw == "" {
		return svcauth.Claims{}, ErrNotOurKey
	}
	// The list is consulted before the signature so an outage of it cannot be
	// mistaken for a bad key: refusing to answer and refusing the caller are
	// different answers, and only one of them is the caller's problem.
	if err := k.listUsable(); err != nil {
		return svcauth.Claims{}, err
	}
	claims, err := k.verifier.Verify(ctx, raw)
	if err != nil {
		return svcauth.Claims{}, err
	}
	if err := k.list.Check(claims); err != nil {
		return svcauth.Claims{}, err
	}
	for _, want := range scopes {
		if !claims.HasScope(want) {
			return svcauth.Claims{}, ErrMissingScope
		}
	}
	return claims, nil
}

// ErrNotOurKey is a bearer value that is not one of this product's keys: it
// carries another product's prefix, or none at all.
var ErrNotOurKey = errors.New("appkeys: not this product's key")

// ErrMissingScope is a valid key that was not granted what the route takes.
var ErrMissingScope = errors.New("appkeys: key lacks a required scope")

// Require guards the routes it wraps with a customer key carrying every scope
// named. ClaimsFrom reads the result.
//
// It answers 503 when the revocation list cannot be trusted, never 401: a key
// this product cannot check is not a key it may declare invalid, and telling a
// customer their valid key is invalid sends them rotating it during an outage.
func (k *Keys) Require(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := svcauth.BearerToken(r)
			if !ok {
				unauthorized(w)
				return
			}
			claims, err := k.Verify(r.Context(), raw, scopes...)
			switch {
			case err == nil:
			case errors.Is(err, ErrListStale), errors.Is(err, svcauth.ErrRevocationUnknown):
				writeError(w, http.StatusServiceUnavailable, "revocation_unknown")
				return
			case errors.Is(err, ErrMissingScope):
				writeError(w, http.StatusForbidden, "scope")
				return
			default:
				unauthorized(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, claims)))
		})
	}
}

type claimsKey struct{}

// ClaimsFrom reads what Require verified: Owner is the person the key belongs
// to, ClientID the key's own id.
//
// The key is this package's own, not svcauth's: a customer key and a service
// token are different credentials, and a handler must not read one where it
// expects the other.
func ClaimsFrom(ctx context.Context) (svcauth.Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(svcauth.Claims)
	return c, ok
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "invalid_key")
}
