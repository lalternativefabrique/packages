package connect

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

// Store keeps the pending consents and the grants. Implementations seal the
// tokens at rest.
type Store interface {
	AddPending(ctx context.Context, p Pending) error
	// TakePending consumes a state: whatever happens next, it is never read
	// twice. An unknown state is ErrUnknownState.
	TakePending(ctx context.Context, state string) (Pending, error)
	Save(ctx context.Context, c Connection) error
	// Connection is ErrNotConnected when the owner holds no grant.
	Connection(ctx context.Context, owner, product string) (Connection, error)
	// Connections may leave the tokens out.
	Connections(ctx context.Context, owner string) ([]Connection, error)
	// Rotate runs fn on the connection under a row lock and saves what it
	// returns in the same transaction; an error from fn saves nothing.
	Rotate(ctx context.Context, owner, product string, fn func(Connection) (Connection, error)) (Connection, error)
	// MarkRevoked keeps the connection, revoked and without tokens.
	MarkRevoked(ctx context.Context, owner, product string) error
	Delete(ctx context.Context, owner, product string) error
	// Reached records what the grant reaches now.
	Reached(ctx context.Context, c Connection, resourceIDs []string) error
	Purge(ctx context.Context, owner string) error
}

type Config struct {
	Issuer      string
	ClientID    string
	RedirectURI string
	Products    Products
	// ForgetRemoved deletes a grant urbangate refuses, and the call answers
	// ErrNotConnected; otherwise it is kept revoked and the call answers
	// ErrAccessRemoved.
	ForgetRemoved bool
}

// Grants obtains, refreshes and uses owners' grants. It is unavailable, and
// every call but List answers ErrUnavailable, without a store or a token
// endpoint.
type Grants struct {
	cfg    Config
	store  Store
	tokens TokenEndpoint
	client ProductClient
	reader ResourceReader
	now    func() time.Time
	cache  sync.Map
}

func NewGrants(cfg Config, store Store, tokens TokenEndpoint, client ProductClient) *Grants {
	return &Grants{cfg: cfg, store: store, tokens: tokens, client: client, reader: client, now: time.Now}
}

// WithReader replaces how /connect/v1/resources is read, for tests.
func (g *Grants) WithReader(r ResourceReader) *Grants {
	g.reader = r
	return g
}

// WithClock replaces time.Now, for tests.
func (g *Grants) WithClock(now func() time.Time) *Grants {
	g.now = now
	return g
}

func (g *Grants) Available() bool { return g.store != nil && g.tokens != nil }

func (g *Grants) Products() Products { return g.cfg.Products }

func (g *Grants) Product(id string) (Product, error) {
	p, ok := g.cfg.Products.Find(id)
	if !ok {
		return p, ErrUnknownProduct
	}
	if !g.Available() {
		return p, ErrUnavailable
	}
	return p, nil
}

// Connections is the owner's grants by product, none when unavailable.
func (g *Grants) Connections(ctx context.Context, owner string) (map[string]Connection, error) {
	out := map[string]Connection{}
	if !g.Available() {
		return out, nil
	}
	conns, err := g.store.Connections(ctx, owner)
	if err != nil {
		return nil, err
	}
	for _, c := range conns {
		out[c.Product] = c
	}
	return out, nil
}

// Start records a consent in flight and answers where to send the person.
func (g *Grants) Start(ctx context.Context, owner, product string) (string, error) {
	p, err := g.Product(product)
	if err != nil {
		return "", err
	}
	pending, err := NewPending(owner, p.ID, g.now())
	if err != nil {
		return "", err
	}
	if err := g.store.AddPending(ctx, pending); err != nil {
		return "", err
	}
	return AuthorizeURL(g.cfg.Issuer, g.cfg.ClientID, g.cfg.RedirectURI, pending, p), nil
}

type Outcome string

const (
	OutcomeConnected   Outcome = "connected"
	OutcomeRefused     Outcome = "refused"
	OutcomeInvalid     Outcome = "invalid"
	OutcomeUnavailable Outcome = "unavailable"
)

// Finish turns what urbangate sent to the redirect URI into a grant. Only a
// store failure is an error; everything else is an outcome.
func (g *Grants) Finish(ctx context.Context, owner, state, code, authErr string) (string, Outcome, error) {
	if !g.Available() {
		return "", OutcomeUnavailable, nil
	}
	pending, err := g.store.TakePending(ctx, state)
	if errors.Is(err, ErrUnknownState) {
		return "", OutcomeInvalid, nil
	}
	if err != nil {
		return "", "", err
	}
	if err := pending.Check(owner, g.now()); err != nil {
		return pending.Product, OutcomeInvalid, nil
	}
	if authErr != "" {
		if authErr == "access_denied" {
			return pending.Product, OutcomeRefused, nil
		}
		log.Printf("connect: %s authorization answered %s", pending.Product, authErr)
		return pending.Product, OutcomeInvalid, nil
	}
	tokens, err := g.tokens.Exchange(ctx, code, pending.Verifier)
	if errors.Is(err, ErrGrantRefused) {
		return pending.Product, OutcomeInvalid, nil
	}
	if err != nil {
		log.Printf("connect: exchange for %s: %v", pending.Product, err)
		return pending.Product, OutcomeUnavailable, nil
	}
	conn := Connection{
		Owner: owner, Product: pending.Product, Status: StatusActive,
		RefreshToken: tokens.Refresh, AccessToken: tokens.Access, AccessExpiresAt: tokens.ExpiresAt,
		Scopes: tokens.Scopes, ConnectedAt: g.now().UTC(),
	}
	if conn.Scopes == nil {
		conn.Scopes = []string{}
	}
	if err := g.store.Save(ctx, conn); err != nil {
		if rerr := g.tokens.Revoke(ctx, tokens.Refresh); rerr != nil {
			log.Printf("connect: %s grant not recorded and not revoked: %v", pending.Product, rerr)
		}
		return pending.Product, "", err
	}
	g.remember(conn)
	return pending.Product, OutcomeConnected, nil
}

// Disconnect revokes the grant at urbangate, then forgets it. A grant
// urbangate could not revoke is kept, and the error is ErrRevokeFailed.
func (g *Grants) Disconnect(ctx context.Context, owner, product string) error {
	p, err := g.Product(product)
	if err != nil {
		return err
	}
	conn, err := g.store.Connection(ctx, owner, p.ID)
	if errors.Is(err, ErrNotConnected) {
		return nil
	}
	if err != nil {
		return err
	}
	if conn.RefreshToken != "" {
		if err := g.tokens.Revoke(ctx, conn.RefreshToken); err != nil && !errors.Is(err, ErrGrantRefused) {
			return fmt.Errorf("%w: %v", ErrRevokeFailed, err)
		}
	}
	g.forget(owner, p.ID)
	return g.store.Delete(ctx, owner, p.ID)
}

// Purge erases every grant of the owner, revoking each at urbangate on a best
// effort: the erasure must not wait on urbangate.
func (g *Grants) Purge(ctx context.Context, owner string) error {
	if !g.Available() {
		return nil
	}
	conns, err := g.store.Connections(ctx, owner)
	if err != nil {
		return err
	}
	for _, c := range conns {
		full, err := g.store.Connection(ctx, owner, c.Product)
		if err == nil && full.RefreshToken != "" {
			if rerr := g.tokens.Revoke(ctx, full.RefreshToken); rerr != nil && !errors.Is(rerr, ErrGrantRefused) {
				log.Printf("connect: purge: revoke %s: %v", c.Product, rerr)
			}
		}
		g.forget(owner, c.Product)
	}
	return g.store.Purge(ctx, owner)
}

// Resources reads what the owner's grant reaches and records it.
func (g *Grants) Resources(ctx context.Context, owner, product string) (Grant, error) {
	p, err := g.Product(product)
	if err != nil {
		return Grant{}, err
	}
	var out Grant
	conn, err := g.withAccess(ctx, owner, p, func(access string) (err error) {
		out, err = g.reader.Resources(ctx, p, access)
		return err
	})
	if err != nil {
		return Grant{}, err
	}
	return out, g.store.Reached(ctx, conn, out.IDs())
}

// Call relays a JSON call to the product with the owner's grant. See
// ProductClient.Do for the errors.
func (g *Grants) Call(ctx context.Context, owner, product, method, path string, in, out any) error {
	return g.WithAccess(ctx, owner, product, func(p Product, access string) error {
		return g.client.Do(ctx, p, access, method, path, in, out)
	})
}

// WithAccess calls the product with the owner's access token, refreshing it
// when it is stale or refused once. call must answer ErrTokenRefused on a 401.
func (g *Grants) WithAccess(ctx context.Context, owner, product string, call func(p Product, access string) error) error {
	p, err := g.Product(product)
	if err != nil {
		return err
	}
	_, err = g.withAccess(ctx, owner, p, func(access string) error { return call(p, access) })
	return err
}

func (g *Grants) withAccess(ctx context.Context, owner string, p Product, call func(access string) error) (Connection, error) {
	conn, err := g.store.Connection(ctx, owner, p.ID)
	if err != nil {
		return conn, err
	}
	if conn.Status == StatusRevoked || conn.RefreshToken == "" {
		return conn, ErrAccessRemoved
	}
	access := g.cached(owner, p.ID)
	if access == "" {
		access = conn.FreshAccess(g.now())
	}
	cached := access != ""
	for {
		if access == "" {
			conn, err = g.refresh(ctx, owner, p.ID)
			switch AfterRefresh(err) {
			case StepDisconnected:
				return conn, g.removed(ctx, owner, p.ID)
			case StepOutage:
				return conn, err
			}
			access = conn.AccessToken
		}
		err := call(access)
		switch AfterProductCall(err, cached) {
		case StepDone:
			return conn, nil
		case StepRefreshAndRetry:
			g.forget(owner, p.ID)
			access, cached = "", false
		case StepDisconnected:
			return conn, g.removed(ctx, owner, p.ID)
		default:
			return conn, err
		}
	}
}

// refresh rotates the refresh token under the row lock: Hydra rotates it on
// every use, and two concurrent refreshes would race to store it.
func (g *Grants) refresh(ctx context.Context, owner, product string) (Connection, error) {
	conn, err := g.store.Rotate(ctx, owner, product, func(locked Connection) (Connection, error) {
		if locked.Status == StatusRevoked || locked.RefreshToken == "" {
			return locked, ErrGrantRefused
		}
		t, err := g.tokens.Refresh(ctx, locked.RefreshToken)
		if err != nil {
			return locked, err
		}
		if t.Refresh != "" {
			locked.RefreshToken = t.Refresh
		}
		locked.AccessToken, locked.AccessExpiresAt = t.Access, t.ExpiresAt
		if len(t.Scopes) > 0 {
			locked.Scopes = t.Scopes
		}
		return locked, nil
	})
	if err == nil {
		g.remember(conn)
	}
	return conn, err
}

func (g *Grants) removed(ctx context.Context, owner, product string) error {
	g.forget(owner, product)
	if g.cfg.ForgetRemoved {
		if err := g.store.Delete(ctx, owner, product); err != nil {
			return err
		}
		return ErrNotConnected
	}
	if err := g.store.MarkRevoked(ctx, owner, product); err != nil {
		return err
	}
	return ErrAccessRemoved
}

type cachedAccess struct {
	token     string
	expiresAt time.Time
}

func cacheKey(owner, product string) string { return owner + "\x00" + product }

func (g *Grants) remember(c Connection) {
	if c.AccessToken == "" {
		return
	}
	g.cache.Store(cacheKey(c.Owner, c.Product), cachedAccess{token: c.AccessToken, expiresAt: c.AccessExpiresAt})
}

func (g *Grants) forget(owner, product string) { g.cache.Delete(cacheKey(owner, product)) }

func (g *Grants) cached(owner, product string) string {
	v, ok := g.cache.Load(cacheKey(owner, product))
	if !ok {
		return ""
	}
	a := v.(cachedAccess)
	return Connection{AccessToken: a.token, AccessExpiresAt: a.expiresAt}.FreshAccess(g.now())
}
