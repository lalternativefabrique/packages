// Package metering is a shared monthly-quota engine over an append-only usage
// ledger, extracted from Lungor. A service (transcript-api, core) imports it with
// a fixed AppID and its own usage_ledger/usage_units tables, declares its units,
// and calls Consume — the monthly plan grant is applied lazily and idempotently.
package metering

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/lungor/core/metering/application/consume"
	"github.com/lalternative/packages/lungor/core/metering/application/declareunit"
	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// Meter is the composed entry point: it caps consumption at the plan's
// allocation FOR THE CURRENT BILLING PERIOD. It is the only type a consuming
// service needs to wire.
//
// There is no stored balance and no monthly grant. The ledger is a pure
// append-only journal of debits, and the quota is derived on read as
// consumed_this_period vs the plan's allowance — so nothing rolls over and an
// exhausted period never blocks the next one.
type Meter struct {
	appID    uuid.UUID
	tenantID uuid.UUID
	units    domain.UsageUnitRepository
	consume  *consume.Handler
	resolver domain.CustomerResolver
	declare  *declareunit.Handler
	now      func() time.Time
	// ledger is held directly for period-bounded reads (ConsumedBetween), which
	// are aggregations rather than state changes and so have no command handler.
	ledger domain.LedgerRepository
	// periods decides the window consumption is counted over. Defaults to the
	// UTC calendar month; the subscription anchor replaces it later (FR4).
	periods PeriodResolver
}

// Config wires a Meter over a service's own ledger + unit repositories.
type Config struct {
	AppID    uuid.UUID
	TenantID uuid.UUID
	Units    domain.UsageUnitRepository
	Ledger   domain.LedgerRepository
	// Now overrides the clock (tests). Defaults to time.Now.
	Now func() time.Time
	// Periods overrides how the billing window is resolved. Defaults to the UTC
	// calendar month — the fail-closed fallback required by NFR4.
	Periods PeriodResolver
}

// New builds a Meter with a StaticResolver (no customers table needed).
func New(cfg Config) *Meter {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Periods == nil {
		cfg.Periods = calendarMonthResolver{}
	}
	resolver := domain.NewStaticResolver(cfg.TenantID)
	return &Meter{
		appID:    cfg.AppID,
		tenantID: cfg.TenantID,
		units:    cfg.Units,
		consume:  consume.NewHandler(cfg.Units, cfg.Ledger, resolver),
		resolver: resolver,
		declare:  declareunit.NewHandler(cfg.Units),
		now:      cfg.Now,
		ledger:   cfg.Ledger,
		periods:  cfg.Periods,
	}
}

// DeclareUnit registers a meterable unit (idempotent). Call once per unit at
// boot; Consume refuses an undeclared unit.
func (m *Meter) DeclareUnit(ctx context.Context, code, name string) error {
	_, err := m.declare.Handle(ctx, declareunit.Input{
		TenantID: m.tenantID,
		AppID:    m.appID,
		Code:     code,
		Name:     name,
	})
	return err
}

// ListUnits returns the meterable units declared for this app, so an admin
// surface can show what is provisioned (and spot a wiped units table).
func (m *Meter) ListUnits(ctx context.Context) ([]*domain.UsageUnit, error) {
	return m.units.ListByApp(ctx, m.appID)
}

// UnitDecl is a (code, display name) pair to declare. A consuming service holds
// the canonical list and passes it to ReprovisionUnits.
type UnitDecl struct{ Code, Name string }

// ReprovisionUnits (re)declares every unit in decls. Each DeclareUnit is
// idempotent, so this is safe to call at boot and to re-run from an admin action
// to heal a wiped usage_units table without a restart.
func (m *Meter) ReprovisionUnits(ctx context.Context, decls []UnitDecl) error {
	for _, d := range decls {
		if err := m.DeclareUnit(ctx, d.Code, d.Name); err != nil {
			return fmt.Errorf("declare %s: %w", d.Code, err)
		}
	}
	return nil
}

// Period exposes the current billing-period key ("YYYY-MM", UTC) for admin
// surfaces that report usage against it.
func (m *Meter) Period() string { return m.period() }

// period is the current billing period key "YYYY-MM" in UTC.
func (m *Meter) period() string {
	return m.now().UTC().Format("2006-01")
}

// PeriodWindow is the half-open interval [Start, End) a quota is counted over.
// Consumption is what was debited inside it; nothing before Start counts.
type PeriodWindow struct {
	Start time.Time
	End   time.Time
}

// PeriodResolver decides which window a tenant's quota is counted over. It is
// the seam the subscription anchor (`renews_at`, FR3/FR4) plugs into later:
// swapping the calendar-month default for an anchor-derived window is then a
// wiring change here, not a change to every consumption read.
//
// A resolver that cannot determine an anchor MUST return the calendar month
// (see CalendarMonth) — never an open-ended or absent window, which would read
// as unlimited quota (NFR4).
type PeriodResolver interface {
	// ResolvePeriod returns the window containing `now` for one external user.
	ResolvePeriod(ctx context.Context, externalUserID string, now time.Time) PeriodWindow
}

// CalendarMonth is the fail-closed default window: [1st 00:00 UTC, next 1st).
// It is what a tenant with no subscription anchor is counted over.
func CalendarMonth(now time.Time) PeriodWindow {
	u := now.UTC()
	start := time.Date(u.Year(), u.Month(), 1, 0, 0, 0, 0, time.UTC)
	return PeriodWindow{Start: start, End: start.AddDate(0, 1, 0)}
}

// calendarMonthResolver is the default PeriodResolver: every tenant is counted
// over the UTC calendar month. Used until the renewal anchor lands.
type calendarMonthResolver struct{}

func (calendarMonthResolver) ResolvePeriod(_ context.Context, _ string, now time.Time) PeriodWindow {
	return CalendarMonth(now)
}

// CurrentPeriod reports the window the tenant's quota is currently counted over.
func (m *Meter) CurrentPeriod(ctx context.Context, externalUserID string) PeriodWindow {
	return m.periods.ResolvePeriod(ctx, externalUserID, m.now())
}

// ConsumedThisPeriod returns how much of `unit` the tenant has spent inside the
// current billing period — the quota reading, as opposed to Remaining's
// lifetime balance. Only debits inside the window count, so consumption from a
// past period (or a past plan) never weighs on it.
func (m *Meter) ConsumedThisPeriod(ctx context.Context, externalUserID, unit string) (int64, error) {
	res, err := m.resolver.Resolve(ctx, m.appID, externalUserID)
	if err != nil {
		return 0, err
	}
	w := m.CurrentPeriod(ctx, externalUserID)
	consumed, err := m.ledger.ConsumedBetween(ctx, m.appID, res.CustomerID, unit, w.Start, w.End)
	if err != nil {
		return 0, err
	}
	// A period-bounded consumption is a sum of debits: it can never be negative.
	// Credits (positive deltas) inside the window are excluded by the query, so a
	// negative here would mean a corrupt ledger — clamp rather than report it.
	if consumed < 0 {
		return 0, nil
	}
	return consumed, nil
}

// ConsumeQuota debits `qty` against the tenant's allowance FOR THE CURRENT
// BILLING PERIOD. It is the hard cap of the quota model: the debit is refused
// (Allowed=false, nothing recorded) when
//
//	consumed_this_period + qty > a.Amount
//
// Only debits inside the window count, so a tenant who exhausted a past period
// is served again as soon as it rolls over — no grant, no top-up, no manual
// intervention. That is the whole point: the prepaid Consume below refuses on a
// LIFETIME balance, which means a tenant who spent their allowance last month
// can never submit again, however untouched the current period is.
//
// The check and the append happen in one serialized transaction, so concurrent
// debits cannot both observe the pre-debit total and both pass — the same
// guarantee the prepaid path gets from AppendIfBalance. idemKey dedupes retries
// of the SAME logical debit (e.g. a job id + attempt); a replay writes nothing.
//
// An unlimited allocation bypasses the ceiling but is still recorded, so the
// ledger stays a complete consumption journal.
func (m *Meter) ConsumeQuota(ctx context.Context, externalUserID string, a domain.Allocation, qty int64, idemKey string) (domain.Decision, error) {
	now := m.now()
	w := m.periods.ResolvePeriod(ctx, externalUserID, now)
	limit := a.Amount
	if a.IsUnlimited() {
		limit = domain.Unlimited
	}
	return m.consume.Handle(ctx, consume.Input{
		Scope:          domain.Scope{AppID: m.appID, ExternalUserID: externalUserID},
		Unit:           a.Unit,
		Quantity:       qty,
		IdempotencyKey: idemKey,
		Period:         &consume.Window{From: w.Start, To: w.End},
		Limit:          limit,
		OccurredAt:     now,
	})
}

// RemainingThisPeriod is how much of the allowance is left in the current
// billing period: limit - consumed, clamped at zero. It never reports credits
// carried over from a past period, and never reports zero because a past period
// was exhausted.
//
// It replaced a prepaid Remaining that read the LIFETIME ledger balance after
// applying a monthly grant. That balance stayed at zero once a period was spent
// until the next grant landed, and stacked unspent allocations month after month
// — the roll-over and the lock-out this quota model removes (FR5).
func (m *Meter) RemainingThisPeriod(ctx context.Context, externalUserID string, a domain.Allocation) (int64, error) {
	consumed, err := m.ConsumedThisPeriod(ctx, externalUserID, a.Unit)
	if err != nil {
		return 0, err
	}
	rem := a.Amount - consumed
	if rem < 0 {
		return 0, nil
	}
	return rem, nil
}
