package svcauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func listServer(t *testing.T, pages ...revocationPage) (*httptest.Server, *[]string) {
	t.Helper()
	var calls int32
	cursors := make([]string, 0, len(pages))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursors = append(cursors, r.URL.Query().Get("as_of"))
		i := int(atomic.AddInt32(&calls, 1)) - 1
		if i >= len(pages) {
			i = len(pages) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pages[i])
	}))
	t.Cleanup(srv.Close)
	return srv, &cursors
}

func newList(t *testing.T, url string) *RevocationList {
	t.Helper()
	l, err := NewRevocationList(url, staticToken("t"))
	if err != nil {
		t.Fatalf("NewRevocationList: %v", err)
	}
	return l
}

// A list that has never loaded cannot tell a live key from a revoked one, and
// an empty set looks exactly like a failed fetch. Answering "not revoked"
// would bring every withdrawn key back at the worst moment — a service that
// just restarted during an incident.
func TestRevokedBeforeAnyLoad(t *testing.T) {
	l := newList(t, "https://example.test/revoked")

	revoked, err := l.Revoked("jti-1")
	if !errors.Is(err, ErrRevocationUnknown) {
		t.Fatalf("err = %v, want ErrRevocationUnknown", err)
	}
	if revoked {
		t.Fatal("must not claim a key is revoked when nothing is known")
	}
	if l.Loaded() {
		t.Fatal("Loaded must be false before the first fetch")
	}
	if !errors.Is(l.Check(Claims{JTI: "jti-1"}), ErrRevocationUnknown) {
		t.Fatal("Check must surface the same refusal")
	}
}

func TestLoadThenCheck(t *testing.T) {
	srv, _ := listServer(t, revocationPage{
		AsOf:    "2026-09-20T12:00:00Z",
		Entries: []revocationEntry{{JTI: "dead", ClientID: "ak_1"}},
	})
	l := newList(t, srv.URL)

	if err := l.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !l.Loaded() {
		t.Fatal("Loaded must be true after a successful fetch")
	}
	if err := l.Check(Claims{JTI: "dead"}); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("Check(dead) = %v, want ErrKeyRevoked", err)
	}
	if err := l.Check(Claims{JTI: "alive"}); err != nil {
		t.Fatalf("Check(alive) = %v, want nil", err)
	}
}

// The first read asks for everything, later ones send the cursor and receive
// only what changed — so a steady state transfers nothing.
func TestLoadSendsTheCursorItWasGiven(t *testing.T) {
	srv, cursors := listServer(t,
		revocationPage{AsOf: "T1", Entries: []revocationEntry{{JTI: "a"}}},
		revocationPage{AsOf: "T2", Entries: []revocationEntry{{JTI: "b"}}},
	)
	l := newList(t, srv.URL)
	ctx := context.Background()

	if err := l.Load(ctx); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if err := l.Load(ctx); err != nil {
		t.Fatalf("second Load: %v", err)
	}

	if got := *cursors; len(got) != 2 || got[0] != "" || got[1] != "T1" {
		t.Fatalf("cursors = %q, want [\"\", \"T1\"]", got)
	}
	// The delta added to what was held rather than replacing it.
	for _, jti := range []string{"a", "b"} {
		if err := l.Check(Claims{JTI: jti}); !errors.Is(err, ErrKeyRevoked) {
			t.Fatalf("%s should be revoked after the delta", jti)
		}
	}
}

// Without a cursor the issuer sends everything it still enforces, so an entry
// held and no longer listed has expired on its own. Keeping it would grow the
// set forever.
func TestFullLoadReplacesTheSet(t *testing.T) {
	srv, _ := listServer(t,
		revocationPage{AsOf: "", Entries: []revocationEntry{{JTI: "old"}}},
		revocationPage{AsOf: "", Entries: []revocationEntry{{JTI: "new"}}},
	)
	l := newList(t, srv.URL)
	ctx := context.Background()

	_ = l.Load(ctx)
	_ = l.Load(ctx)

	if err := l.Check(Claims{JTI: "old"}); err != nil {
		t.Fatalf("old should have been dropped, got %v", err)
	}
	if err := l.Check(Claims{JTI: "new"}); !errors.Is(err, ErrKeyRevoked) {
		t.Fatal("new should be revoked")
	}
}

// A failed refresh leaves the last good copy in place: verification keeps
// working through an outage of the issuer, which is the whole point of
// holding a projection rather than asking on every request.
func TestFailedRefreshKeepsWhatWasLoaded(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			_ = json.NewEncoder(w).Encode(revocationPage{
				AsOf: "T1", Entries: []revocationEntry{{JTI: "dead"}},
			})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	l := newList(t, srv.URL)
	ctx := context.Background()
	if err := l.Load(ctx); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if err := l.Load(ctx); err == nil {
		t.Fatal("a 500 must be reported")
	}
	if err := l.Check(Claims{JTI: "dead"}); !errors.Is(err, ErrKeyRevoked) {
		t.Fatal("the last good copy must survive a failed refresh")
	}
}

// A token with no jti predates signed keys. It is not revoked, and asking
// about it must not be an error once a list exists.
func TestEmptyJTIIsNotRevoked(t *testing.T) {
	srv, _ := listServer(t, revocationPage{AsOf: "T1"})
	l := newList(t, srv.URL)
	if err := l.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := l.Check(Claims{}); err != nil {
		t.Fatalf("Check with no jti = %v, want nil", err)
	}
}

func TestRejectsARelativeURL(t *testing.T) {
	if _, err := NewRevocationList("/revoked", nil); err == nil {
		t.Fatal("want an error for a url that is not absolute")
	}
}

// The product authenticates to the list as it does to every machine route.
func TestSendsTheBearer(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(revocationPage{AsOf: "T1"})
	}))
	defer srv.Close()

	l := newList(t, srv.URL)
	if err := l.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if h := <-got; h != "Bearer t" {
		t.Fatalf("Authorization = %q", h)
	}
}
