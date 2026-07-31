package infrastructure

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// maxSerializationRetries bounds retries of a SERIALIZABLE transaction that the
// database aborts with 40001 (could not serialize). Under contention the engine
// asks the caller to retry — that is expected, not an error to surface.
const maxSerializationRetries = 10

// isSerializationFailure reports whether err is a Postgres 40001 (serialization
// failure) — the signal to retry a SERIALIZABLE transaction.
func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}

type LedgerRepo struct{ pool *pgxpool.Pool }

func NewLedgerRepo(p *pgxpool.Pool) *LedgerRepo { return &LedgerRepo{pool: p} }

func (r *LedgerRepo) Append(ctx context.Context, e *domain.LedgerEntry) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO usage_ledger
		  (id, tenant_id, app_id, customer_id, subscription_id, unit, delta, idempotency_key, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (app_id, idempotency_key) DO NOTHING
	`, e.ID, e.TenantID, e.AppID, e.CustomerID, e.SubscriptionID, e.Unit, e.Delta, e.IdempotencyKey, e.OccurredAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AppendIfBalance does the prepaid check + append atomically under SERIALIZABLE
// isolation, so two concurrent consumptions cannot both pass the >= 0 check (no
// overdraft). The database aborts the losing transaction with 40001; we retry it
// (bounded) — that retry is what keeps concurrent callers correct instead of
// failing. Idempotent: a replay of the same (app_id, idempotency_key) returns the
// existing balance and inserts nothing.
func (r *LedgerRepo) AppendIfBalance(ctx context.Context, e *domain.LedgerEntry) (bool, int64, error) {
	for attempt := 0; ; attempt++ {
		inserted, balance, err := r.appendIfBalanceOnce(ctx, e)
		if isSerializationFailure(err) && attempt < maxSerializationRetries {
			continue
		}
		return inserted, balance, err
	}
}

func (r *LedgerRepo) appendIfBalanceOnce(ctx context.Context, e *domain.LedgerEntry) (bool, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Idempotency short-circuit: if this key already landed, report the current
	// balance without writing again.
	var existing bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM usage_ledger WHERE app_id=$1 AND idempotency_key=$2)
	`, e.AppID, e.IdempotencyKey).Scan(&existing); err != nil {
		return false, 0, err
	}

	var balance int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(SUM(delta), 0) FROM usage_ledger
		WHERE app_id=$1 AND customer_id=$2 AND unit=$3
	`, e.AppID, e.CustomerID, e.Unit).Scan(&balance); err != nil {
		return false, 0, err
	}

	if existing {
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return false, balance, nil
	}

	newBalance := balance + e.Delta
	if newBalance < 0 {
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return false, balance, domain.ErrInsufficientBalance
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO usage_ledger
		  (id, tenant_id, app_id, customer_id, subscription_id, unit, delta, idempotency_key, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, e.ID, e.TenantID, e.AppID, e.CustomerID, e.SubscriptionID, e.Unit, e.Delta, e.IdempotencyKey, e.OccurredAt); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return true, newBalance, nil
}

// AppendIfPeriodUnder does the QUOTA check + append atomically under
// SERIALIZABLE isolation. It is AppendIfBalance's period-bounded sibling: where
// that one guards a lifetime balance >= 0, this one guards
// `consumed_in_window + qty <= limit`, so a tenant who exhausted a past period
// starts the next one with the full allowance.
//
// The atomicity guarantee is the same and is the point: two concurrent debits
// must not both observe the pre-debit consumption and both pass. The database
// aborts the losing transaction with 40001 and we retry it (bounded), which is
// what keeps concurrent callers correct rather than merely failing. Idempotent:
// a replay of the same (app_id, idempotency_key) writes nothing and returns the
// consumption already on record.
func (r *LedgerRepo) AppendIfPeriodUnder(ctx context.Context, e *domain.LedgerEntry, from, to time.Time, limit int64) (bool, int64, error) {
	for attempt := 0; ; attempt++ {
		inserted, consumed, err := r.appendIfPeriodUnderOnce(ctx, e, from, to, limit)
		if isSerializationFailure(err) && attempt < maxSerializationRetries {
			continue
		}
		return inserted, consumed, err
	}
}

func (r *LedgerRepo) appendIfPeriodUnderOnce(ctx context.Context, e *domain.LedgerEntry, from, to time.Time, limit int64) (bool, int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Idempotency short-circuit: if this key already landed, report the current
	// period consumption without writing again.
	var existing bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM usage_ledger WHERE app_id=$1 AND idempotency_key=$2)
	`, e.AppID, e.IdempotencyKey).Scan(&existing); err != nil {
		return false, 0, err
	}

	// Consumption already recorded inside the window, as a positive number. Only
	// debits count: a credit (refund, stray grant) inside the window must not buy
	// extra room beyond the plan's allowance.
	var consumed int64
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(-SUM(delta), 0) FROM usage_ledger
		WHERE app_id=$1 AND customer_id=$2 AND unit=$3
		  AND delta < 0 AND occurred_at >= $4 AND occurred_at < $5
	`, e.AppID, e.CustomerID, e.Unit, from, to).Scan(&consumed); err != nil {
		return false, 0, err
	}

	if existing {
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return false, consumed, nil
	}

	// e.Delta is negative for a consumption; the quantity charged is its absolute
	// value.
	qty := -e.Delta
	if consumed+qty > limit {
		if err := tx.Commit(ctx); err != nil {
			return false, 0, err
		}
		return false, consumed, domain.ErrQuotaExceeded
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO usage_ledger
		  (id, tenant_id, app_id, customer_id, subscription_id, unit, delta, idempotency_key, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, e.ID, e.TenantID, e.AppID, e.CustomerID, e.SubscriptionID, e.Unit, e.Delta, e.IdempotencyKey, e.OccurredAt); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return true, consumed + qty, nil
}

func (r *LedgerRepo) Balance(ctx context.Context, appID, customerID uuid.UUID, unit string) (int64, error) {
	var bal int64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(delta), 0) FROM usage_ledger
		WHERE app_id=$1 AND customer_id=$2 AND unit=$3
	`, appID, customerID, unit).Scan(&bal)
	return bal, err
}

func (r *LedgerRepo) ConsumedBetween(ctx context.Context, appID, customerID uuid.UUID, unit string, from, to time.Time) (int64, error) {
	var consumed int64
	err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(-SUM(delta), 0) FROM usage_ledger
		WHERE app_id=$1 AND customer_id=$2 AND unit=$3
		  AND delta < 0 AND occurred_at >= $4 AND occurred_at < $5
	`, appID, customerID, unit, from, to).Scan(&consumed)
	return consumed, err
}
