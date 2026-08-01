package sdk

import (
	"context"
	"sync"
	"time"
)

// DefaultTTL is how long a cached verdict is served.
//
// An entitlement changes when someone subscribes, cancels, or lapses — a few
// times a month per user, and never mid-request. A minute of staleness is
// invisible to a customer and turns a per-request HTTP call into a per-minute
// one. It is deliberately short enough that a fresh subscriber is not left
// staring at the free tier while wondering what they paid for.
const DefaultTTL = time.Minute

// Cache wraps a Client and serves recent verdicts from memory.
//
// It exists because entitlement is read on request paths — rate limiting, quota
// gates — where an HTTP round trip per call is not affordable. Without it, every
// consumer would grow its own cache, each with a different idea of how stale is
// too stale.
//
// Per process, not shared: a stale entry is bounded by TTL, so several replicas
// disagreeing for under a minute costs nothing that matters, and a shared cache
// would buy consistency nobody needs at the price of another dependency.
type Cache struct {
	client *Client
	ttl    time.Duration
	now    func() time.Time

	mu      sync.RWMutex
	entries map[string]entry
}

type entry struct {
	value   Entitlement
	expires time.Time
}

// NewCache wraps a client. A ttl of zero uses DefaultTTL.
func NewCache(c *Client, ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{
		client:  c,
		ttl:     ttl,
		now:     time.Now,
		entries: map[string]entry{},
	}
}

// Entitlement returns the user's entitlement, from cache when fresh.
//
// Only successful lookups are cached. An error is never stored: caching
// ErrUnavailable would extend a transient outage into a minute of certain
// failure, and caching ErrUnauthorized would keep answering from a
// misconfiguration after it was fixed.
//
// Balances are NOT served from cache — a balance moves on every metered
// operation, so a cached one is wrong almost immediately. Asking for units
// bypasses the cache entirely and goes to Lungor, which is the honest behaviour
// for a number that must be current.
func (c *Cache) Entitlement(ctx context.Context, externalUserID string, units ...string) (Entitlement, error) {
	if len(units) > 0 {
		return c.client.Entitlement(ctx, externalUserID, units...)
	}

	now := c.now()
	c.mu.RLock()
	e, ok := c.entries[externalUserID]
	c.mu.RUnlock()
	if ok && now.Before(e.expires) {
		return e.value, nil
	}

	v, err := c.client.Entitlement(ctx, externalUserID)
	if err != nil {
		return Entitlement{}, err
	}

	c.mu.Lock()
	c.entries[externalUserID] = entry{value: v, expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return v, nil
}

// Invalidate drops a user's cached verdict.
//
// Call it the moment the product knows the answer changed — right after a
// checkout returns, or when a webhook lands — so a customer who has just paid
// is served the tier they bought instead of waiting out the TTL.
func (c *Cache) Invalidate(externalUserID string) {
	c.mu.Lock()
	delete(c.entries, externalUserID)
	c.mu.Unlock()
}

// Purge drops every cached verdict. For tests, and for the rare operational
// case where the billing state was changed out of band.
func (c *Cache) Purge() {
	c.mu.Lock()
	c.entries = map[string]entry{}
	c.mu.Unlock()
}
