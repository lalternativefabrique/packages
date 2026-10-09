// Package entitlement enforces what a subject's Lungor plan allows, on the
// routes that create something. Plans, units and quotas are declared per app in
// Lungor; this package never knows what is sold.
package entitlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/lungor/policy"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
	"golang.org/x/sync/singleflight"
)

// Unlimited is the allocation Lungor stores for a unit a plan does not cap.
// Any limit at or above it is treated as uncapped.
const Unlimited int64 = 1_000_000_000

// DefaultRefresh is how old a snapshot may get before it is re-read in the
// background.
const DefaultRefresh = 24 * time.Hour

const ledgerTimeout = 10 * time.Second

// ErrLedgerUnavailable means a metered spend could not be recorded, so it was
// refused.
var ErrLedgerUnavailable = errors.New("entitlement: ledger unavailable")

// Ledger is the subset of *sdk.Client the gate uses.
type Ledger interface {
	Entitlement(ctx context.Context, externalUserID string, units ...string) (sdk.Entitlement, error)
	ListPlans(ctx context.Context) (sdk.Plans, error)
	Consume(ctx context.Context, in sdk.Usage) (sdk.Decision, error)
	Release(ctx context.Context, in sdk.Usage) (sdk.Decision, error)
}

// Counter reports how many of a capacity unit the subject currently holds.
type Counter func(ctx context.Context, subject string) (int64, error)

// Config wires a Gate.
type Config struct {
	Ledger Ledger
	Store  Store
	// Grant provisions a subject Lungor has never heard of. Optional.
	Grant *policy.Grant
	// Fallback is the plan applied when Lungor holds no active subscription
	// for the subject, or cannot be reached and no snapshot exists.
	Fallback string
	Subject  func(c echo.Context) (string, error)
	Counters map[string]Counter
	Refresh  time.Duration
}

// Limits is a subject's plan and its per-unit allocations.
type Limits struct {
	Plan   string           `json:"plan"`
	Limits map[string]int64 `json:"limits"`
}

// Limit returns the allocation of a unit; a unit the plan does not list is 0.
func (l Limits) Limit(unit string) int64 { return l.Limits[unit] }

// Gate enforces plan limits.
type Gate struct {
	cfg Config
	now func() time.Time

	flight singleflight.Group
	bg     sync.WaitGroup

	mu      sync.RWMutex
	cache   map[string]Snapshot
	plans   sdk.Plans
	plansAt time.Time
}

// New builds a Gate. It panics on a missing Ledger or Store, which is a wiring
// bug caught at boot.
func New(cfg Config) *Gate {
	if cfg.Ledger == nil || cfg.Store == nil {
		panic("entitlement: Ledger and Store are required")
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = DefaultRefresh
	}
	return &Gate{cfg: cfg, now: time.Now, cache: map[string]Snapshot{}}
}

// Limits returns the subject's plan and allocations.
func (g *Gate) Limits(ctx context.Context, subject string) (Limits, error) {
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return Limits{}, err
	}
	return Limits{Plan: s.Plan, Limits: s.Limits}, nil
}

// Allow reports whether the subject may take n more of unit. With a Counter
// declared for the unit, what is already held counts against the limit.
// A refusal is a *LimitError.
func (g *Gate) Allow(ctx context.Context, subject, unit string, n int64) error {
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return err
	}
	limit := s.Limits[unit]
	if limit >= Unlimited {
		return nil
	}
	var used int64
	if count, ok := g.cfg.Counters[unit]; ok {
		if used, err = count(ctx, subject); err != nil {
			return fmt.Errorf("entitlement: count %s: %w", unit, err)
		}
	}
	if used+n > limit {
		return &LimitError{Unit: unit, Limit: limit, Used: used, Plan: s.Plan}
	}
	return nil
}

func (g *Gate) allowFeature(ctx context.Context, subject, unit string) error {
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return err
	}
	if s.Limits[unit] < 1 {
		return &LimitError{Unit: unit, Limit: s.Limits[unit], Plan: s.Plan}
	}
	return nil
}

func (g *Gate) snapshot(ctx context.Context, subject string) (Snapshot, error) {
	g.mu.RLock()
	s, ok := g.cache[subject]
	g.mu.RUnlock()
	if ok {
		g.revalidateIfStale(subject, s)
		return s, nil
	}

	s, ok, err := g.cfg.Store.Get(ctx, subject)
	if err != nil {
		return Snapshot{}, fmt.Errorf("entitlement: read store: %w", err)
	}
	if ok {
		g.remember(subject, s)
		g.revalidateIfStale(subject, s)
		return s, nil
	}

	s, err = g.load(ctx, subject)
	if err != nil {
		slog.Warn("entitlement: ledger unreachable, applying fallback plan", "subject", subject, "err", err)
		return g.fallbackSnapshot(), nil
	}
	return s, nil
}

func (g *Gate) revalidateIfStale(subject string, s Snapshot) {
	if g.now().Sub(s.RefreshedAt) < g.cfg.Refresh {
		return
	}
	g.bg.Add(1)
	go func() {
		defer g.bg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), ledgerTimeout)
		defer cancel()
		if _, err := g.load(ctx, subject); err != nil {
			slog.Warn("entitlement: background refresh failed, keeping snapshot", "subject", subject, "err", err)
		}
	}()
}

func (g *Gate) load(ctx context.Context, subject string) (Snapshot, error) {
	v, err, _ := g.flight.Do(subject, func() (any, error) {
		s, err := g.fetch(ctx, subject)
		if err != nil {
			return Snapshot{}, err
		}
		if err := g.cfg.Store.Put(ctx, subject, s); err != nil {
			slog.Error("entitlement: snapshot not persisted", "subject", subject, "err", err)
		}
		g.remember(subject, s)
		return s, nil
	})
	return v.(Snapshot), err
}

func (g *Gate) fetch(ctx context.Context, subject string) (Snapshot, error) {
	plan := g.cfg.Fallback
	if !policy.IsAnon(subject) {
		ent, err := g.cfg.Ledger.Entitlement(ctx, subject)
		if err != nil {
			return Snapshot{}, err
		}
		if ent.Status == sdk.StatusNoSubscription {
			if ent, err = g.provision(ctx, subject, ent); err != nil {
				return Snapshot{}, err
			}
		}
		if ent.Entitled && ent.PlanCode != "" {
			plan = ent.PlanCode
		}
	}
	plans, err := g.catalogue(ctx, true)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Plan: plan, Limits: limitsOf(plans, plan), RefreshedAt: g.now()}, nil
}

func (g *Gate) provision(ctx context.Context, subject string, ent sdk.Entitlement) (sdk.Entitlement, error) {
	if g.cfg.Grant == nil {
		return ent, nil
	}
	granted, err := g.cfg.Grant.Ensure(ctx, subject, "")
	if err != nil {
		slog.Error("entitlement: provisioning failed, applying fallback plan", "subject", subject, "err", err)
		return ent, nil
	}
	if !granted {
		return ent, nil
	}
	return g.cfg.Ledger.Entitlement(ctx, subject)
}

func (g *Gate) catalogue(ctx context.Context, reachable bool) (sdk.Plans, error) {
	g.mu.RLock()
	plans, at := g.plans, g.plansAt
	g.mu.RUnlock()
	if !reachable || (plans != nil && g.now().Sub(at) < g.cfg.Refresh) {
		return plans, nil
	}
	fresh, err := g.cfg.Ledger.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	if fresh == nil {
		fresh = sdk.Plans{}
	}
	g.mu.Lock()
	g.plans, g.plansAt = fresh, g.now()
	g.mu.Unlock()
	return fresh, nil
}

func (g *Gate) fallbackSnapshot() Snapshot {
	plans, _ := g.catalogue(context.Background(), false)
	return Snapshot{Plan: g.cfg.Fallback, Limits: limitsOf(plans, g.cfg.Fallback)}
}

func (g *Gate) remember(subject string, s Snapshot) {
	g.mu.Lock()
	g.cache[subject] = s
	g.mu.Unlock()
}

func (g *Gate) forgetCatalogue() {
	g.mu.Lock()
	g.plans, g.plansAt = nil, time.Time{}
	g.mu.Unlock()
}

func limitsOf(plans sdk.Plans, code string) map[string]int64 {
	limits := map[string]int64{}
	p, ok := plans.ByCode(code)
	if !ok {
		return limits
	}
	for _, a := range p.Allocations {
		limits[a.Unit] = a.Amount
	}
	return limits
}

// LimitError is a refusal: the plan does not allow what was asked.
type LimitError struct {
	Unit  string
	Limit int64
	Used  int64
	Plan  string
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("entitlement: plan %q allows %d %s, %d used", e.Plan, e.Limit, e.Unit, e.Used)
}
