package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchStaticUsesCacheOnSecondCall(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(articleHTML))
	}))
	defer srv.Close()

	cache := NewMemoryCache(time.Minute)

	if _, err := FetchStatic(context.Background(), srv.URL, 6000, cache); err != nil {
		t.Fatalf("FetchStatic (1st): %v", err)
	}
	if _, err := FetchStatic(context.Background(), srv.URL, 6000, cache); err != nil {
		t.Fatalf("FetchStatic (2nd): %v", err)
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1 (2nd call should be served from cache)", hits)
	}
}

func TestFetchStaticCacheHonorsDifferentMaxRunesPerCall(t *testing.T) {
	srv := serveHTML(t, articleHTML)
	defer srv.Close()

	cache := NewMemoryCache(time.Minute)

	full, err := FetchStatic(context.Background(), srv.URL, 6000, cache)
	if err != nil {
		t.Fatalf("FetchStatic (full): %v", err)
	}
	short, err := FetchStatic(context.Background(), srv.URL, 10, cache)
	if err != nil {
		t.Fatalf("FetchStatic (truncated): %v", err)
	}
	if len([]rune(short.Text)) >= len([]rune(full.Text)) {
		t.Errorf("truncated call returned %d runes, want fewer than the full %d — the cache must store untruncated text", len([]rune(short.Text)), len([]rune(full.Text)))
	}
}

func TestFetchStaticCacheExpiresAfterTTL(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(articleHTML))
	}))
	defer srv.Close()

	cache := NewMemoryCache(1 * time.Millisecond)

	if _, err := FetchStatic(context.Background(), srv.URL, 6000, cache); err != nil {
		t.Fatalf("FetchStatic (1st): %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := FetchStatic(context.Background(), srv.URL, 6000, cache); err != nil {
		t.Fatalf("FetchStatic (2nd): %v", err)
	}
	if hits != 2 {
		t.Errorf("server hit %d times, want 2 (2nd call should miss the expired entry)", hits)
	}
}
