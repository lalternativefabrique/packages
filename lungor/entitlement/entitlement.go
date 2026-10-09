// Package entitlement enforces what a subject's Lungor plan allows, on the
// routes that create something. Plans, units and quotas are declared per app in
// Lungor; this package never knows what is sold.
package entitlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/lungor/policy"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Unlimited is the allocation Lungor stores for a unit a plan does not cap.
// Any limit at or above it is treated as uncapped.
const Unlimited int64 = 1_000_000_000

// DefaultRefresh is how old a snapshot may get before it is re-read in the
// background.
const DefaultRefresh = 24 * time.Hour

const ledgerTimeout = 10 * time.Second

const balanceConcurrency = 4

// ErrLedgerUnavailable means a metered spend could not be recorded, so it was
// refused.
var ErrLedgerUnavailable = errors.New("entitlement: ledger unavailable")

// DefaultEntitledStatuses are the subscription statuses whose plan limits apply
// when Lungor alone decides. Apps extend the list for grace (see Config).
var DefaultEntitledStatuses = []string{"active", "trialing"}

// MissingUnitPolicy says what a unit the plan does not allocate is worth.
type MissingUnitPolicy int

const (
	// MissingUnitDenied gives an unallocated unit a limit of 0.
	MissingUnitDenied MissingUnitPolicy = iota
	// MissingUnitUnlimited leaves an unallocated unit uncapped: only the units
	// a plan names are enforced.
	MissingUnitUnlimited
)

// OutagePolicy says what Admit answers when the ledger cannot be read.
type OutagePolicy int

const (
	// Deny refuses the admission with ErrLedgerUnavailable.
	Deny OutagePolicy = iota
	// Allow admits and logs: the app prefers serving over enforcing.
	Allow
)

// Ledger is the subset of *sdk.Client the gate uses.
type Ledger interface {
	Entitlement(ctx context.Context, externalUserID string, units ...string) (sdk.Entitlement, error)
	ListPlans(ctx context.Context) (sdk.Plans, error)
	Balance(ctx context.Context, externalUserID, unit string) (sdk.Balance, error)
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
	// Units lists gated units read on top of the Counters keys and the units
	// named by Require, RequireRoom and Spend.
	Units   []string
	Refresh time.Duration

	// MissingUnit is what a unit the subject's plan does not allocate is
	// worth. Default MissingUnitDenied.
	MissingUnit MissingUnitPolicy
	// Bypass reports subjects every check passes for, such as platform
	// admins. Optional; an error fails the check.
	Bypass func(ctx context.Context, subject string) (bool, error)
	// EntitledStatuses are the subscription statuses that keep a subject on
	// its plan's limits even when Lungor reports Entitled=false (grace).
	// Default DefaultEntitledStatuses.
	EntitledStatuses []string
	// AdmitTTL caches a live Balance read for Admit. Default 0: every Admit
	// reads the ledger.
	AdmitTTL time.Duration
	// AdmitOnOutage is what Admit answers when the ledger cannot be read.
	// Default Deny. Spend, Consume and Record always deny.
	AdmitOnOutage OutagePolicy
	// AnonLedger answers for subjects policy.IsAnon reports, instead of
	// Ledger. Optional: without it anonymous subjects get the Fallback plan
	// and their debits go to Ledger.
	AnonLedger Ledger
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

	mu       sync.RWMutex
	cache    map[string]Snapshot
	balances map[string]cachedBalance
	units    map[string]struct{}
	plans    sdk.Plans
	plansAt  time.Time
	open     bool
}

// New builds a Gate. A nil Ledger is dev mode: every check passes, nothing is
// metered, and a warning is logged at boot. With a Ledger, a nil Store is a
// wiring bug and panics.
func New(cfg Config) *Gate {
	open := cfg.Ledger == nil
	if open {
		slog.Warn("entitlement: no Ledger configured, every check passes and nothing is metered")
		cfg.Ledger = AllowAll()
		if cfg.Store == nil {
			cfg.Store = NewMemoryStore()
		}
	}
	if cfg.Store == nil {
		panic("entitlement: Store is required")
	}
	if cfg.Refresh <= 0 {
		cfg.Refresh = DefaultRefresh
	}
	if cfg.EntitledStatuses == nil {
		cfg.EntitledStatuses = DefaultEntitledStatuses
	}
	g := &Gate{
		cfg: cfg, now: time.Now, open: open,
		cache: map[string]Snapshot{}, balances: map[string]cachedBalance{}, units: map[string]struct{}{},
	}
	for unit := range cfg.Counters {
		g.track(unit)
	}
	for _, unit := range cfg.Units {
		g.track(unit)
	}
	return g
}

func (g *Gate) track(unit string) {
	g.mu.Lock()
	g.units[unit] = struct{}{}
	g.mu.Unlock()
}

func (g *Gate) trackedUnits() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	units := make([]string, 0, len(g.units))
	for unit := range g.units {
		units = append(units, unit)
	}
	return units
}

// Invalidate re-reads the subject from Lungor and replaces its snapshot, for
// callers that changed the subject's plan themselves (Grant, ChangePlan).
func (g *Gate) Invalidate(ctx context.Context, subject string) error {
	g.forgetBalances(subject, "")
	if _, err := g.load(ctx, subject); err != nil {
		return fmt.Errorf("entitlement: invalidate %s: %w", subject, err)
	}
	return nil
}

// passes reports whether the subject skips every check: dev mode, or a
// Bypass subject.
func (g *Gate) passes(ctx context.Context, subject string) (bool, error) {
	if g.open {
		return true, nil
	}
	if g.cfg.Bypass == nil {
		return false, nil
	}
	ok, err := g.cfg.Bypass(ctx, subject)
	if err != nil {
		return false, fmt.Errorf("entitlement: bypass %s: %w", subject, err)
	}
	return ok, nil
}

func (g *Gate) ledgerFor(subject string) Ledger {
	if g.cfg.AnonLedger != nil && policy.IsAnon(subject) {
		return g.cfg.AnonLedger
	}
	return g.cfg.Ledger
}

func (g *Gate) limitOf(s Snapshot, unit string) int64 {
	limit, ok := s.Limits[unit]
	if !ok {
		return g.missingLimit()
	}
	return limit
}

func (g *Gate) missingLimit() int64 {
	if g.cfg.MissingUnit == MissingUnitUnlimited {
		return Unlimited
	}
	return 0
}

func (g *Gate) entitled(ent sdk.Entitlement) bool {
	return ent.Entitled || slices.Contains(g.cfg.EntitledStatuses, ent.Status)
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
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return err
	}
	limit := g.limitOf(s, unit)
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
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return err
	}
	if limit := g.limitOf(s, unit); limit < 1 {
		return &LimitError{Unit: unit, Limit: limit, Plan: s.Plan}
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
	anon := policy.IsAnon(subject)
	if anon && g.cfg.AnonLedger == nil {
		return g.fetchFallback(ctx)
	}
	ledger := g.ledgerFor(subject)
	ent, err := ledger.Entitlement(ctx, subject)
	if err != nil {
		return Snapshot{}, err
	}
	if ent.Status == sdk.StatusNoSubscription && !anon {
		if ent, err = g.provision(ctx, subject, ent); err != nil {
			return Snapshot{}, err
		}
	}
	if !g.entitled(ent) || ent.PlanCode == "" {
		return g.fetchFallback(ctx)
	}
	limits, err := g.subjectLimits(ctx, ledger, subject)
	if err != nil {
		return Snapshot{}, err
	}
	if _, err := g.catalogue(ctx, true); err != nil {
		slog.Warn("entitlement: fallback catalogue not refreshed", "err", err)
	}
	return Snapshot{Plan: ent.PlanCode, Limits: limits, RefreshedAt: g.now()}, nil
}

func (g *Gate) fetchFallback(ctx context.Context) (Snapshot, error) {
	plans, err := g.catalogue(ctx, true)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Plan: g.cfg.Fallback, Limits: limitsOf(plans, g.cfg.Fallback), RefreshedAt: g.now()}, nil
}

func (g *Gate) subjectLimits(ctx context.Context, ledger Ledger, subject string) (map[string]int64, error) {
	units := g.trackedUnits()
	amounts := make([]int64, len(units))
	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(balanceConcurrency)
	for i, unit := range units {
		eg.Go(func() error {
			b, err := ledger.Balance(ctx, subject, unit)
			if missingUnit(b, err) {
				amounts[i] = g.missingLimit()
				return nil
			}
			if err != nil {
				return fmt.Errorf("balance %s: %w", unit, err)
			}
			amounts[i] = b.Limit
			if b.Unlimited {
				amounts[i] = Unlimited
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	limits := make(map[string]int64, len(units))
	for i, unit := range units {
		limits[unit] = amounts[i]
	}
	return limits, nil
}

func (g *Gate) provision(ctx context.Context, subject string, ent sdk.Entitlement) (sdk.Entitlement, error) {
	if g.cfg.Grant == nil || g.open {
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

// missingUnit reports a unit the subject's plan does not allocate: Lungor
// answers ErrNotFound, or a balance that is neither periodic, nor capacity,
// nor unlimited, with no ceiling at all.
func missingUnit(b sdk.Balance, err error) bool {
	if err != nil {
		return errors.Is(err, sdk.ErrNotFound)
	}
	return !b.Periodic && !b.Unlimited && b.Kind != sdk.UnitCapacity && b.Limit == 0 && b.Remaining == 0
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
