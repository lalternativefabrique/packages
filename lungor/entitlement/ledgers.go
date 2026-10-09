package entitlement

import (
	"context"
	"maps"
	"sync"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

// AllowAllPlan is the plan code AllowAll reports.
const AllowAllPlan = "allow_all"

// AllowAll is a Ledger for development: every unit is unlimited, nothing is
// recorded. New treats it as dev mode and short-circuits every check.
func AllowAll() Ledger { return allowAll{} }

type allowAll struct{}

func (allowAll) Entitlement(context.Context, string, ...string) (sdk.Entitlement, error) {
	return sdk.Entitlement{Entitled: true, Status: "active", PlanCode: AllowAllPlan}, nil
}

func (allowAll) ListPlans(context.Context) (sdk.Plans, error) { return sdk.Plans{}, nil }

func (allowAll) Balance(_ context.Context, _, unit string) (sdk.Balance, error) {
	return sdk.Balance{Unit: unit, Limit: Unlimited, Remaining: Unlimited, Unlimited: true}, nil
}

func (allowAll) Consume(context.Context, sdk.Usage) (sdk.Decision, error) {
	return sdk.Decision{Allowed: true}, nil
}

func (allowAll) Release(context.Context, sdk.Usage) (sdk.Decision, error) {
	return sdk.Decision{Allowed: true}, nil
}

// StaticLedger is an in-memory Ledger putting every subject on one plan with
// the given limits, metered per subject and deduplicated on the idempotency
// key. It is the local free plan an app runs without Lungor, and the anonymous
// engine of Config.AnonLedger. Consumption does not survive a restart.
func StaticLedger(plan string, limits map[string]int64) Ledger {
	return &staticLedger{plan: plan, limits: maps.Clone(limits), used: map[string]int64{}, seen: map[string]struct{}{}}
}

type staticLedger struct {
	plan   string
	limits map[string]int64

	mu   sync.Mutex
	used map[string]int64
	seen map[string]struct{}
}

func (l *staticLedger) Entitlement(context.Context, string, ...string) (sdk.Entitlement, error) {
	return sdk.Entitlement{Entitled: true, Status: "active", PlanCode: l.plan}, nil
}

func (l *staticLedger) ListPlans(context.Context) (sdk.Plans, error) {
	p := sdk.Plan{Code: l.plan, Name: l.plan}
	for unit, amount := range l.limits {
		p.Allocations = append(p.Allocations, sdk.Allocation{Unit: unit, Amount: amount})
	}
	return sdk.Plans{p}, nil
}

func (l *staticLedger) Balance(_ context.Context, subject, unit string) (sdk.Balance, error) {
	limit, ok := l.limits[unit]
	if !ok {
		return sdk.Balance{Unit: unit}, sdk.ErrNotFound
	}
	l.mu.Lock()
	used := l.used[balanceKey(subject, unit)]
	l.mu.Unlock()
	return sdk.Balance{
		Unit: unit, Limit: limit, Consumed: used, Remaining: max(limit-used, 0),
		Periodic: true, Kind: sdk.UnitMetered, Unlimited: limit >= Unlimited,
	}, nil
}

func (l *staticLedger) Consume(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	limit, ok := l.limits[u.Unit]
	if !ok {
		return sdk.Decision{Reason: "unit not allocated"}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := balanceKey(u.ExternalUserID, u.Unit)
	if u.IdempotencyKey != "" {
		if _, dup := l.seen[u.IdempotencyKey]; dup {
			return sdk.Decision{Allowed: true, Balance: max(limit-l.used[key], 0)}, nil
		}
	}
	if limit < Unlimited && l.used[key]+u.Quantity > limit {
		return sdk.Decision{Reason: "plan limit", Balance: max(limit-l.used[key], 0)}, nil
	}
	l.used[key] += u.Quantity
	if u.IdempotencyKey != "" {
		l.seen[u.IdempotencyKey] = struct{}{}
	}
	return sdk.Decision{Allowed: true, Recorded: true, Balance: max(limit-l.used[key], 0)}, nil
}

func (l *staticLedger) Release(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := balanceKey(u.ExternalUserID, u.Unit)
	if u.IdempotencyKey != "" {
		if _, dup := l.seen[u.IdempotencyKey]; dup {
			return sdk.Decision{Allowed: true, Balance: l.limits[u.Unit] - l.used[key]}, nil
		}
		l.seen[u.IdempotencyKey] = struct{}{}
	}
	l.used[key] = max(l.used[key]-u.Quantity, 0)
	return sdk.Decision{Allowed: true, Recorded: true, Balance: l.limits[u.Unit] - l.used[key]}, nil
}
