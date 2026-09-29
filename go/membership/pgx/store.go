// Package pgx is the Postgres Store of membership, on pgx v5.
package pgx

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	pgxv5 "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/membership"
)

// Schema is the members table. Products ship it in their own migrations;
// EnsureSchema applies it for tests and dev stacks.
//
//go:embed schema.sql
var Schema string

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type txKey struct{}

// Tx is the transaction a Product.Open or Product.Purge hook runs in, so the
// product's own rows commit with the member's.
func Tx(ctx context.Context) (pgxv5.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgxv5.Tx)
	return tx, ok
}

func (s *Store) within(ctx context.Context, fn func(ctx context.Context, tx pgxv5.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(context.WithValue(ctx, txKey{}, tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, Schema)
	return err
}

const columns = `identity_id, local_id, COALESCE(address, ''), state, resources, attempts, last_error, next_attempt_at, opened_at, updated_at, ready_at, erased_at`

func scan(row pgxv5.Row) (membership.Member, error) {
	var m membership.Member
	var state string
	err := row.Scan(&m.IdentityID, &m.LocalID, &m.Address, &state, &m.Resources, &m.Attempts, &m.LastError, &m.NextAttemptAt, &m.OpenedAt, &m.UpdatedAt, &m.ReadyAt, &m.ErasedAt)
	if err != nil {
		return membership.Member{}, err
	}
	m.State = membership.State(state)
	return m, nil
}

func (s *Store) Get(ctx context.Context, identityID string) (membership.Member, error) {
	m, err := scan(s.pool.QueryRow(ctx, `SELECT `+columns+` FROM members WHERE identity_id = $1`, identityID))
	if errors.Is(err, pgxv5.ErrNoRows) {
		return membership.Member{}, membership.ErrNotMember
	}
	return m, err
}

func (s *Store) AddressTaken(ctx context.Context, address string) (bool, error) {
	var taken bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM members WHERE address = $1)`, address).Scan(&taken)
	return taken, err
}

func (s *Store) Insert(ctx context.Context, m membership.Member, within membership.Within) (membership.Member, error) {
	if existing, err := s.Get(ctx, m.IdentityID); err == nil {
		return existing, nil
	} else if !errors.Is(err, membership.ErrNotMember) {
		return membership.Member{}, err
	}
	now := time.Now().UTC()
	if m.OpenedAt.IsZero() {
		m.OpenedAt = now
	}
	if m.NextAttemptAt.IsZero() {
		m.NextAttemptAt = now
	}
	err := s.within(ctx, func(ctx context.Context, tx pgxv5.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('member:' || $1))`, m.IdentityID); err != nil {
			return err
		}
		if within != nil {
			if err := within(ctx, &m); err != nil {
				return err
			}
		}
		resources := m.Resources
		if len(resources) == 0 {
			resources = []byte("{}")
		}
		var address *string
		if m.Address != "" {
			address = &m.Address
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO members (identity_id, local_id, address, state, resources, last_error, next_attempt_at, opened_at, updated_at, erased_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)
			ON CONFLICT (identity_id) DO NOTHING`,
			m.IdentityID, m.LocalID, address, string(m.State), resources, m.LastError, m.NextAttemptAt, m.OpenedAt, m.ErasedAt)
		return err
	})
	if err != nil {
		return membership.Member{}, err
	}
	return s.Get(ctx, m.IdentityID)
}

func (s *Store) Transition(ctx context.Context, identityID string, from, to membership.State, patch membership.Patch, within membership.Within) (bool, error) {
	var nextAttempt *time.Time
	if !patch.NextAttemptAt.IsZero() {
		nextAttempt = &patch.NextAttemptAt
	}
	moved := false
	err := s.within(ctx, func(ctx context.Context, tx pgxv5.Tx) error {
		row, err := scan(tx.QueryRow(ctx, `
		UPDATE members SET
			state = $3,
			resources = COALESCE($4, resources),
			last_error = $5,
			attempts = 0,
			next_attempt_at = COALESCE($6, next_attempt_at),
			ready_at = CASE WHEN $3 = 'ready' THEN now() ELSE ready_at END,
			erased_at = CASE WHEN $3 = 'erased' THEN now() ELSE erased_at END,
			updated_at = now()
		WHERE identity_id = $1 AND state = $2
		RETURNING `+columns+``,
			identityID, string(from), string(to), []byte(patch.Resources), patch.LastError, nextAttempt))
		if errors.Is(err, pgxv5.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		moved = true
		if within != nil {
			return within(ctx, &row)
		}
		return nil
	})
	return moved, err
}

func (s *Store) ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]membership.Member, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
			SELECT identity_id FROM members
			WHERE state IN ('provisioning', 'erasing') AND next_attempt_at <= $1
			ORDER BY next_attempt_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE members m SET next_attempt_at = $1 + $2::interval
		FROM due WHERE m.identity_id = due.identity_id
		RETURNING `+qualified("m")+``,
		now, fmt.Sprintf("%d milliseconds", lease.Milliseconds()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []membership.Member
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Fail(ctx context.Context, identityID string, cause string, retryAt time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE members SET attempts = attempts + 1, last_error = $2, next_attempt_at = $3, updated_at = now()
		WHERE identity_id = $1`, identityID, cause, retryAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return membership.ErrNotMember
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]membership.Member, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+columns+` FROM members ORDER BY identity_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []membership.Member
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func qualified(alias string) string {
	return alias + ".identity_id, " + alias + ".local_id, COALESCE(" + alias + ".address, ''), " + alias + ".state, " + alias + ".resources, " +
		alias + ".attempts, " + alias + ".last_error, " + alias + ".next_attempt_at, " + alias + ".opened_at, " + alias + ".updated_at, " + alias + ".ready_at, " + alias + ".erased_at"
}
