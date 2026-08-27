package fetch

import (
	"sync"
	"time"
)

// Cache stores a fetched page's full, untruncated content keyed by URL, so a
// second fetch of the same URL — a common pattern when an LLM agent re-reads
// a page across turns, or pages through a long article — skips the network
// round trip and the readability parse entirely.
//
// Callers apply their own maxRunes truncation after a cache hit: the cache
// holds the full page so that two calls with different maxRunes (or a
// Paginate call following an earlier one) do not fight over what was kept.
type Cache interface {
	Get(url string) (*Page, bool)
	Set(url string, p *Page)
}

// NewMemoryCache builds an in-process Cache. Entries older than ttl are
// treated as absent and overwritten on the next Set. There is no background
// eviction: a stale entry is only cleared out when something looks it up or
// replaces it, which is enough for a cache sized to one process's lifetime.
func NewMemoryCache(ttl time.Duration) Cache {
	return &memoryCache{ttl: ttl, entries: make(map[string]cacheEntry)}
}

type cacheEntry struct {
	page     *Page
	storedAt time.Time
}

type memoryCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]cacheEntry
}

func (c *memoryCache) Get(url string) (*Page, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[url]
	if !ok {
		return nil, false
	}
	if time.Since(entry.storedAt) > c.ttl {
		delete(c.entries, url)
		return nil, false
	}
	return entry.page, true
}

func (c *memoryCache) Set(url string, p *Page) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[url] = cacheEntry{page: p, storedAt: time.Now()}
}
