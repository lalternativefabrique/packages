package sdk

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// counting spins up a stub that records how many times it was called, which is
// the only thing worth asserting about a cache.
func counting(t *testing.T, status int, payload any) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// The reason this type exists: entitlement is read on request paths, where an
// HTTP round trip per call is not affordable.
func TestCache_ServesRepeatedReadsFromMemory(t *testing.T) {
	srv, calls := counting(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	for i := 0; i < 5; i++ {
		got, err := c.Entitlement(ctx(), "user-1")
		if err != nil || !got.Entitled {
			t.Fatalf("call %d: %+v %v", i, got, err)
		}
	}

	if *calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", *calls)
	}
}

func TestCache_KeysByUser(t *testing.T) {
	srv, calls := counting(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	_, _ = c.Entitlement(ctx(), "user-1")
	_, _ = c.Entitlement(ctx(), "user-2")

	if *calls != 2 {
		t.Fatalf("upstream calls = %d, want one per user", *calls)
	}
}

func TestCache_RefetchesOnceTheTTLHasPassed(t *testing.T) {
	srv, calls := counting(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	now := time.Now()
	c.now = func() time.Time { return now }
	_, _ = c.Entitlement(ctx(), "user-1")

	now = now.Add(time.Minute + time.Second)
	_, _ = c.Entitlement(ctx(), "user-1")

	if *calls != 2 {
		t.Fatalf("upstream calls = %d, want a refetch past the TTL", *calls)
	}
}

// A customer who has just paid must not wait out the TTL staring at the free
// tier — the caller invalidates the moment it knows the answer changed.
func TestCache_InvalidateForcesTheNextReadThrough(t *testing.T) {
	srv, calls := counting(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	_, _ = c.Entitlement(ctx(), "user-1")
	c.Invalidate("user-1")
	_, _ = c.Entitlement(ctx(), "user-1")

	if *calls != 2 {
		t.Fatalf("upstream calls = %d, want the invalidated read to go through", *calls)
	}
}

// Caching a failure would extend a transient outage into a full TTL of certain
// failure, and would keep answering from a misconfiguration after it was fixed.
func TestCache_NeverCachesAFailure(t *testing.T) {
	srv, calls := counting(t, http.StatusBadGateway, nil)
	c := NewCache(New(srv.URL, "k"), time.Minute)

	for i := 0; i < 3; i++ {
		if _, err := c.Entitlement(ctx(), "user-1"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("call %d: err = %v", i, err)
		}
	}

	if *calls != 3 {
		t.Fatalf("upstream calls = %d, want every failed read retried", *calls)
	}
}

// A balance moves on every metered operation, so a cached one is wrong almost
// immediately. Asking for units must reach Lungor.
func TestCache_BypassedWhenBalancesAreRequested(t *testing.T) {
	srv, calls := counting(t, 200, Entitlement{
		Entitled: true, Status: "active",
		Balances: map[string]int64{"credit": 10},
	})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	_, _ = c.Entitlement(ctx(), "user-1", "credit")
	_, _ = c.Entitlement(ctx(), "user-1", "credit")

	if *calls != 2 {
		t.Fatalf("upstream calls = %d, want balances always fetched", *calls)
	}
}

func TestCache_ZeroTTLFallsBackToTheDefault(t *testing.T) {
	if got := NewCache(New("http://x", "k"), 0).ttl; got != DefaultTTL {
		t.Fatalf("ttl = %v, want DefaultTTL", got)
	}
}

// The cache is read from concurrent request handlers by construction.
func TestCache_IsSafeUnderConcurrentReads(t *testing.T) {
	srv, _ := counting(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := NewCache(New(srv.URL, "k"), time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Entitlement(ctx(), "user-1")
		}()
	}
	wg.Wait()
}
