package botguard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type console struct {
	srv     *httptest.Server
	calls   atomic.Int32
	down    atomic.Bool
	mu      sync.Mutex
	reports []SyncRequest
}

func newConsole(t *testing.T, answer SyncResponse) *console {
	c := &console{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.calls.Add(1)
		if c.down.Load() || r.URL.Path != SyncPath || r.Header.Get("Authorization") != "Bearer app" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var req SyncRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.mu.Lock()
		c.reports = append(c.reports, req)
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func remote(c *console) (*RemoteSettings, *time.Time) {
	now := time.Unix(1000, 0)
	r := NewRemoteSettings(RemoteConfig{
		BaseURL: c.srv.URL,
		Guard:   "waitlist",
		Token:   func(context.Context) (string, error) { return "app", nil },
	})
	r.now = func() time.Time { return now }
	return r, &now
}

func waitCalls(t *testing.T, c *console, n int32) {
	t.Helper()
	for range 200 {
		if c.calls.Load() >= n {
			time.Sleep(5 * time.Millisecond)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("console got %d calls, want %d", c.calls.Load(), n)
}

func TestRemoteSettingsFetchesThenCaches(t *testing.T) {
	c := newConsole(t, SyncResponse{Enforce: true, PerMinute: 7, Burst: 3, Cost: 500, SyncSeconds: 30})
	r, _ := remote(c)
	for range 5 {
		p, err := r.Policy(t.Context())
		if err != nil || !p.Enforce || p.PerMinute != 7 || p.Burst != 3 || p.Cost != 500 {
			t.Fatalf("policy %+v err %v", p, err)
		}
	}
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("console called %d times", n)
	}
}

func TestRemoteSettingsKeepsLastPolicyWhileConsoleIsDown(t *testing.T) {
	c := newConsole(t, SyncResponse{Enforce: true, PerMinute: 7, Burst: 3, Cost: 500, SyncSeconds: 30})
	r, now := remote(c)
	if _, err := r.Policy(t.Context()); err != nil {
		t.Fatal(err)
	}
	c.down.Store(true)
	*now = now.Add(time.Minute)
	p, err := r.Policy(t.Context())
	if err != nil || !p.Enforce {
		t.Fatalf("stale policy dropped: %+v %v", p, err)
	}
	waitCalls(t, c, 2)
	if p, _ := r.Policy(t.Context()); !p.Enforce {
		t.Fatal("failed refresh dropped the policy")
	}
}

func TestRemoteSettingsNeverKnownFailsWithoutHammering(t *testing.T) {
	c := newConsole(t, SyncResponse{})
	c.down.Store(true)
	r, _ := remote(c)
	for range 10 {
		if _, err := r.Policy(t.Context()); err == nil {
			t.Fatal("no policy known, yet no error")
		}
	}
	if n := c.calls.Load(); n != 1 {
		t.Fatalf("console hammered: %d calls", n)
	}
}

func TestRemoteSettingsReportsCountersSinceLastSync(t *testing.T) {
	c := newConsole(t, SyncResponse{PerMinute: 1, Burst: 1, Cost: 1000, SyncSeconds: 30})
	r, now := remote(c)
	g, err := New(Config{Audience: "a", Secret: make([]byte, 32), Settings: r})
	if err != nil {
		t.Fatal(err)
	}
	r.Report(g)
	g.stats.challengesIssued.Add(3)
	if _, err := r.Policy(t.Context()); err != nil {
		t.Fatal(err)
	}
	g.stats.challengesIssued.Add(2)
	*now = now.Add(time.Minute)
	r.Policy(t.Context())
	waitCalls(t, c, 2)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.reports) != 2 || c.reports[0].Counters.ChallengesIssued != 3 || c.reports[1].Counters.ChallengesIssued != 2 || c.reports[0].Guard != "waitlist" {
		t.Fatalf("reports %+v", c.reports)
	}
}

func TestObserveModeCountsWhatEnforcementWouldRefuse(t *testing.T) {
	g, _ := New(Config{Audience: "a", Secret: make([]byte, 32), Settings: StaticSettings(Policy{PerMinute: 1, Burst: 1})})
	h := g.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for range 3 {
		r := httptest.NewRequest(http.MethodPost, "/x", nil)
		r.RemoteAddr = "203.0.113.1:1"
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	if c := g.Counters(); c.RefusedChallenge == 0 {
		t.Fatalf("counters %+v", c)
	}
}
