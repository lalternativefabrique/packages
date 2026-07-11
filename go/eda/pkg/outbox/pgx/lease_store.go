// Package pgx provides a Postgres-backed LeaseStore for the lease-model outbox
// relay, using the pgx driver.
//
// The lease model keeps every row pending and reserves a claimed batch by
// pushing next_attempt_at into the future, so a crashed relay self-heals when
// the lease expires (no "processing" state, no reaper). See pkg/outbox/lease.go.
//
// Schema (see Schema for the canonical DDL): the store expects a table with at
// least id, topic, payload, status, attempts, last_error, last_attempt_at,
// next_attempt_at, published_at, updated_at columns. The table name is
// configurable (default "outbox_events").
package pgx

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/eda/pkg/outbox"
)

// Schema is the canonical DDL for the default outbox_events table. Apply it via
// your migration tooling; it is exposed here to keep the table shape in sync
// with the queries below.
const Schema = `
CREATE TABLE IF NOT EXISTS outbox_events (
    id              BIGSERIAL PRIMARY KEY,
    topic           TEXT        NOT NULL,
    ordering_key    TEXT,
    payload         JSONB       NOT NULL,
    trace_headers   JSONB,
    status          TEXT        NOT NULL DEFAULT 'pending',
    attempts        INTEGER     NOT NULL DEFAULT 0,
    last_error      TEXT,
    last_attempt_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_outbox_events_status ON outbox_events (status);
CREATE INDEX IF NOT EXISTS idx_outbox_events_next_attempt_at ON outbox_events (next_attempt_at);
`

const (
	statusPending = "pending"
	statusSent    = "sent"
)

// LeaseStore implements outbox.LeaseStore on Postgres via pgx.
type LeaseStore struct {
	pool  *pgxpool.Pool
	table string
}

// Option configures a LeaseStore.
type Option func(*LeaseStore)

// WithTable overrides the table name (default "outbox_events").
func WithTable(name string) Option {
	return func(s *LeaseStore) {
		if name != "" {
			s.table = name
		}
	}
}

// NewLeaseStore wraps a pgx pool.
func NewLeaseStore(pool *pgxpool.Pool, opts ...Option) *LeaseStore {
	s := &LeaseStore{pool: pool, table: "outbox_events"}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ClaimBatch selects up to limit due pending rows under FOR UPDATE SKIP LOCKED
// and leases them by pushing next_attempt_at out, keeping status pending. The
// SELECT and UPDATE run in one transaction so the row locks are held until the
// lease is written, preventing a concurrent relay from grabbing the same rows.
func (s *LeaseStore) ClaimBatch(ctx context.Context, limit int, lease time.Duration) ([]outbox.RawRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, fmt.Sprintf(`
		SELECT id, topic, payload, attempts
		FROM %s
		WHERE status = $1
		  AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
		ORDER BY id
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`, s.table), statusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("select pending: %w", err)
	}

	var out []outbox.RawRecord
	var ids []int64
	for rows.Next() {
		var r outbox.RawRecord
		if err := rows.Scan(&r.ID, &r.Topic, &r.Payload, &r.Attempts); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		// Attempts as returned reflects the count AFTER this claim, matching the
		// increment below, so backoff is computed against the current attempt.
		r.Attempts++
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending: %w", err)
	}
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}

	// Lease: keep rows pending but push next_attempt_at out so the next scan
	// skips them while this relay publishes. No 'processing' state → a crash
	// before MarkSent/MarkFailed self-heals when the lease expires.
	leaseSecs := int(lease / time.Second)
	if leaseSecs < 1 {
		leaseSecs = 1
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		UPDATE %s
		SET attempts = attempts + 1, last_attempt_at = NOW(),
		    next_attempt_at = NOW() + ($1 * INTERVAL '1 second'), updated_at = NOW()
		WHERE id = ANY($2)
	`, s.table), leaseSecs, ids); err != nil {
		return nil, fmt.Errorf("lease rows: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim tx: %w", err)
	}
	return out, nil
}

// MarkSent promotes a published row to sent.
func (s *LeaseStore) MarkSent(ctx context.Context, id int64) error {
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE %s
		SET status = $1, published_at = NOW(), updated_at = NOW()
		WHERE id = $2
	`, s.table), statusSent, id); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	return nil
}

// MarkFailed records the error and schedules the next attempt via
// next_attempt_at, keeping the row pending.
func (s *LeaseStore) MarkFailed(ctx context.Context, id int64, cause error, retryAfter time.Duration) error {
	secs := int(retryAfter / time.Second)
	if secs < 1 {
		secs = 1
	}
	var errMsg string
	if cause != nil {
		errMsg = cause.Error()
	}
	if _, err := s.pool.Exec(ctx, fmt.Sprintf(`
		UPDATE %s
		SET status = $1, last_error = $2,
		    next_attempt_at = NOW() + ($3 * INTERVAL '1 second'), updated_at = NOW()
		WHERE id = $4
	`, s.table), statusPending, errMsg, secs, id); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return nil
}

var _ outbox.LeaseStore = (*LeaseStore)(nil)
