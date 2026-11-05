package botguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// SyncPath is where a central console answers RemoteSettings.
const SyncPath = "/api/v1/machine/botguard/sync"

// SyncRequest reports a guard's counters since its last sync.
type SyncRequest struct {
	Guard    string   `json:"guard"`
	Counters Counters `json:"counters"`
}

// SyncResponse is the policy the console holds for that guard.
type SyncResponse struct {
	Enforce     bool    `json:"enforce"`
	PerMinute   float64 `json:"per_minute"`
	Burst       int     `json:"burst"`
	Cost        int     `json:"cost"`
	SyncSeconds int     `json:"sync_seconds"`
}

// Apply lays the console's settings over the defaults.
func (s SyncResponse) Apply() Policy {
	p := DefaultPolicy()
	p.Enforce = s.Enforce
	p.PerMinute = s.PerMinute
	p.Burst = s.Burst
	p.Cost = s.Cost
	return p.normalized()
}

type RemoteConfig struct {
	// BaseURL is the console's origin; the sync goes to BaseURL + SyncPath.
	BaseURL string
	// Guard names this guard within the application, e.g. "waitlist".
	Guard string
	// Token authenticates the application to the console.
	Token  func(context.Context) (string, error)
	Client *http.Client
	Logger *slog.Logger
}

// RemoteSettings reads the policy from a central console and reports the
// guard's counters to it. Requests never wait on the console once a policy
// is known: a stale one is refreshed in the background, and the last one
// known is kept while the console is unreachable.
type RemoteSettings struct {
	cfg RemoteConfig
	now func() time.Time

	mu      sync.Mutex
	policy  Policy
	known   bool
	next    time.Time
	syncing bool
	guard   *Guard
	sent    Counters
}

const (
	defaultSyncEvery = time.Minute
	retryEvery       = 15 * time.Second
)

func NewRemoteSettings(cfg RemoteConfig) *RemoteSettings {
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 5 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &RemoteSettings{cfg: cfg, now: time.Now}
}

// Report sends the guard's counters with each sync.
func (r *RemoteSettings) Report(g *Guard) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.guard = g
}

var errNoPolicy = errors.New("botguard: console unreachable, no policy known yet")

func (r *RemoteSettings) Policy(ctx context.Context) (Policy, error) {
	r.mu.Lock()
	due := !r.syncing && !r.now().Before(r.next)
	if due {
		r.syncing = true
	}
	known, policy := r.known, r.policy
	r.mu.Unlock()

	switch {
	case known && due:
		go r.sync(context.WithoutCancel(ctx))
	case !known && due:
		r.sync(ctx)
		r.mu.Lock()
		known, policy = r.known, r.policy
		r.mu.Unlock()
	}
	if !known {
		return Policy{}, errNoPolicy
	}
	return policy, nil
}

func (r *RemoteSettings) sync(ctx context.Context) {
	err := r.exchange(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.syncing = false
	if err != nil {
		r.next = r.now().Add(retryEvery)
		r.cfg.Logger.Warn("botguard: console sync failed", "guard", r.cfg.Guard, "err", err)
	}
}

func (r *RemoteSettings) exchange(ctx context.Context) error {
	r.mu.Lock()
	var reading, delta Counters
	if r.guard != nil {
		reading = r.guard.Counters()
		delta = reading.Since(r.sent)
	}
	r.mu.Unlock()

	token, err := r.cfg.Token(ctx)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}
	body, _ := json.Marshal(SyncRequest{Guard: r.cfg.Guard, Counters: delta})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.BaseURL+SyncPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := r.cfg.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("console answered %d", res.StatusCode)
	}
	var answer SyncResponse
	if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
		return err
	}
	every := time.Duration(answer.SyncSeconds) * time.Second
	if every <= 0 {
		every = defaultSyncEvery
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.policy, r.known = answer.Apply(), true
	r.next = r.now().Add(every)
	r.sent = reading
	return nil
}
