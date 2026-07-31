package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UsageUnitRepository persists app-declared usage units.
type UsageUnitRepository interface {
	Create(ctx context.Context, u *UsageUnit) error
	GetByCode(ctx context.Context, appID uuid.UUID, code string) (*UsageUnit, error)
	ListByApp(ctx context.Context, appID uuid.UUID) ([]*UsageUnit, error)
}

// LedgerRepository is the append-only ledger plus its read aggregations.
type LedgerRepository interface {
	// Append writes an immutable entry with no cap check. It is idempotent on
	// (app_id, idempotency_key): a replay returns inserted=false and writes
	// nothing. No consumption path uses it — every debit goes through
	// AppendIfPeriodUnder so it cannot escape the ceiling. Kept as the ledger's
	// unconditional write for out-of-band corrections and for test fixtures that
	// need to seed history directly.
	Append(ctx context.Context, e *LedgerEntry) (inserted bool, err error)

	// AppendIfBalance atomically checks that the post-movement balance stays
	// >= 0 and appends in a single transaction. Returns ErrInsufficientBalance
	// without writing if the entry would overdraw.
	//
	// This is the PREPAID cap, and nothing calls it any more: the quota model
	// replaced it with AppendIfPeriodUnder (FR5). It caps on the LIFETIME balance,
	// which is exactly the defect that was removed — a customer who spent an
	// allowance stayed at zero and was refused forever, and unspent units piled up
	// across months. Do not wire a new caller onto it.
	AppendIfBalance(ctx context.Context, e *LedgerEntry) (inserted bool, balance int64, err error)

	// AppendIfPeriodUnder is THE consumption cap: it atomically checks that the
	// consumption already recorded inside [from, to) plus this entry stays within
	// `limit`, and appends in the same transaction. Returns ErrQuotaExceeded
	// without writing when the debit would overrun the period's allowance.
	// Consumption from outside the window never counts, so an exhausted past
	// period cannot block a fresh one.
	//
	// It must be serialized against concurrent debits (no two callers may both
	// pass the check), and it is idempotent on (app_id, idempotency_key): a replay
	// writes nothing and reports the consumption already on record.
	AppendIfPeriodUnder(ctx context.Context, e *LedgerEntry, from, to time.Time, limit int64) (inserted bool, consumed int64, err error)

	// Balance returns SUM(delta) for a customer+unit across all time. It is an
	// accounting read over the whole journal, NOT a quota: the quota reading is
	// ConsumedBetween over the current period.
	Balance(ctx context.Context, appID, customerID uuid.UUID, unit string) (int64, error)

	// ConsumedBetween returns the absolute consumption (sum of negative deltas,
	// as a positive number) for a customer+unit within [from, to).
	ConsumedBetween(ctx context.Context, appID, customerID uuid.UUID, unit string, from, to time.Time) (int64, error)
}

// Resolved is the (tenant, customer) pair an app's external user maps to.
type Resolved struct {
	TenantID   uuid.UUID
	CustomerID uuid.UUID
}

// CustomerResolver maps an app's external user id to a tenant + customer. In the
// shared-lib deployment this is a StaticResolver (fixed tenant, deterministic
// customer uuid) — no customers table required.
type CustomerResolver interface {
	Resolve(ctx context.Context, appID uuid.UUID, externalUserID string) (Resolved, error)
}
