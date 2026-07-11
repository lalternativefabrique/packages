// Package pgx provides a Postgres-backed implementation of the consumer's
// pre-claim IdempotencyStore, using the pgx driver.
//
// The model is a three-state claim on (consumer, event_id):
//
//	none         → never seen; the first replica to TryClaim wins
//	in_progress  → a replica is handling it right now (claimed_at set)
//	done         → finished; later deliveries skip
//
// Writing the in_progress claim BEFORE running the handler closes the race
// where two replicas both read "not done" and both run the expensive handler.
// A replica that crashes mid-handler leaves an orphan in_progress row; once
// older than the TTL it is reclaimable, so processing is never wedged forever.
//
// Schema (see Schema for the canonical DDL):
//
//	CREATE TABLE event_idempotency (
//	    consumer     TEXT        NOT NULL,
//	    event_id     TEXT        NOT NULL,
//	    status       TEXT        NOT NULL DEFAULT 'done',
//	    claimed_at   TIMESTAMPTZ,
//	    processed_at TIMESTAMPTZ DEFAULT NOW(),
//	    PRIMARY KEY (consumer, event_id)
//	);
package pgx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Schema is the canonical DDL for the backing table. Apply it via your
// migration tooling; it is exposed here so consumers keep the table shape in
// sync with the queries below.
const Schema = `
CREATE TABLE IF NOT EXISTS event_idempotency (
    consumer     TEXT        NOT NULL,
    event_id     TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'done',
    claimed_at   TIMESTAMPTZ,
    processed_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (consumer, event_id)
);
CREATE INDEX IF NOT EXISTS idx_event_idempotency_lookup
    ON event_idempotency (consumer, event_id);
`

// Querier is the minimal pgx surface the store needs. Both *pgxpool.Pool and
// pgx.Tx satisfy it, so a caller may run claims inside their own transaction or
// against a pool directly.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store implements the consumer.IdempotencyStore three-state claim model on
// Postgres via pgx.
type Store struct {
	q Querier
}

// New wraps a pgx Querier (a *pgxpool.Pool or a pgx.Tx).
func New(q Querier) *Store { return &Store{q: q} }

// TryClaim atomically reserves (consumer, eventID). It returns true when this
// caller obtained the claim and must run the handler. It returns false when a
// fresh in_progress claim or a done row already exists (skip + Ack). A claim
// older than ttl is treated as orphaned and is reclaimable.
func (s *Store) TryClaim(ctx context.Context, consumer, eventID string, ttl time.Duration) (bool, error) {
	// INSERT the claim; on conflict, only steal it back if the existing claim is
	// an expired in_progress (orphan). A fresh in_progress or a done row fails
	// the WHERE, yields no row, and means "someone else owns it" → skip.
	var claimed bool
	err := s.q.QueryRow(ctx, `
		INSERT INTO event_idempotency (consumer, event_id, status, claimed_at, processed_at)
		VALUES ($1, $2, 'in_progress', NOW(), NULL)
		ON CONFLICT (consumer, event_id) DO UPDATE
			SET status = 'in_progress', claimed_at = NOW(), processed_at = NULL
			WHERE event_idempotency.status = 'in_progress'
			  AND event_idempotency.claimed_at < NOW() - $3::interval
		RETURNING true
	`, consumer, eventID, ttl.String()).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim event: %w", err)
	}
	return claimed, nil
}

// MarkDone transitions a won claim to done after the handler succeeded.
func (s *Store) MarkDone(ctx context.Context, consumer, eventID string) error {
	if _, err := s.q.Exec(ctx, `
		UPDATE event_idempotency
		SET status = 'done', processed_at = NOW()
		WHERE consumer = $1 AND event_id = $2
	`, consumer, eventID); err != nil {
		return fmt.Errorf("mark event done: %w", err)
	}
	return nil
}

// Release removes an in_progress claim after the handler failed, so a NATS
// redelivery can reclaim and retry immediately instead of waiting out the TTL.
// It only deletes in_progress rows: a row already promoted to done is kept.
func (s *Store) Release(ctx context.Context, consumer, eventID string) error {
	if _, err := s.q.Exec(ctx, `
		DELETE FROM event_idempotency
		WHERE consumer = $1 AND event_id = $2 AND status = 'in_progress'
	`, consumer, eventID); err != nil {
		return fmt.Errorf("release event claim: %w", err)
	}
	return nil
}
