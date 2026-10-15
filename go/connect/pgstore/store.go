// Package pgstore is the Postgres Store of connect, on pgx v5. Access tokens
// are not stored: connect keeps them in memory.
package pgstore

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/connect"
)

// Schema is the grant tables. Hosts ship it in their own migrations;
// EnsureSchema applies it for tests and dev stacks.
//
//go:embed schema.sql
var Schema string

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, Schema)
	return err
}

type Store struct {
	pool   *pgxpool.Pool
	sealer connect.Sealer
}

// New seals each refresh token with the owner as additional data.
func New(pool *pgxpool.Pool, sealer connect.Sealer) *Store {
	return &Store{pool: pool, sealer: sealer}
}

func (s *Store) AddPending(ctx context.Context, p connect.Pending) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM connect_grant_pending WHERE expires_at < NOW()`); err != nil {
		return fmt.Errorf("prune pending: %w", err)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO connect_grant_pending (state, verifier, owner, product, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, p.State, p.Verifier, p.Owner, p.Product, p.ExpiresAt)
	if err != nil {
		return fmt.Errorf("add pending: %w", err)
	}
	return nil
}

func (s *Store) TakePending(ctx context.Context, state string) (connect.Pending, error) {
	p := connect.Pending{State: state}
	err := s.pool.QueryRow(ctx, `
		DELETE FROM connect_grant_pending WHERE state = $1
		RETURNING verifier, owner, product, expires_at`, state).
		Scan(&p.Verifier, &p.Owner, &p.Product, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, connect.ErrUnknownState
	}
	if err != nil {
		return p, fmt.Errorf("take pending: %w", err)
	}
	return p, nil
}

type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Store) Save(ctx context.Context, c connect.Connection) error {
	return s.save(ctx, s.pool, c)
}

func (s *Store) save(ctx context.Context, db dbtx, c connect.Connection) error {
	refresh, err := s.sealer.Seal(c.RefreshToken, c.Owner)
	if err != nil {
		return err
	}
	scopes, resources := c.Scopes, c.Resources
	if scopes == nil {
		scopes = []string{}
	}
	_, err = db.Exec(ctx, `
		INSERT INTO connect_grant (owner, product, status, refresh_token, scopes, resources, connected_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, COALESCE($6, '{}'::text[]), $7, NOW())
		ON CONFLICT (owner, product) DO UPDATE SET
			status = EXCLUDED.status, refresh_token = EXCLUDED.refresh_token, scopes = EXCLUDED.scopes,
			connected_at = EXCLUDED.connected_at, updated_at = NOW()`,
		c.Owner, c.Product, string(c.Status), refresh, scopes, resources, c.ConnectedAt)
	if err != nil {
		return fmt.Errorf("save grant: %w", err)
	}
	return nil
}

const selectGrant = `SELECT owner, product, status, refresh_token, scopes, resources, connected_at FROM connect_grant`

func (s *Store) Connection(ctx context.Context, owner, product string) (connect.Connection, error) {
	return s.one(ctx, s.pool, selectGrant+` WHERE owner = $1 AND product = $2`, owner, product)
}

func (s *Store) one(ctx context.Context, db dbtx, sql string, args ...any) (connect.Connection, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return connect.Connection{}, fmt.Errorf("grant: %w", err)
	}
	c, err := pgx.CollectExactlyOneRow(rows, s.scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, connect.ErrNotConnected
	}
	if err != nil {
		return c, fmt.Errorf("grant: %w", err)
	}
	return c, nil
}

func (s *Store) Connections(ctx context.Context, owner string) ([]connect.Connection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT owner, product, status, NULL::bytea, scopes, resources, connected_at
		FROM connect_grant WHERE owner = $1 ORDER BY product`, owner)
	if err != nil {
		return nil, fmt.Errorf("grants: %w", err)
	}
	return pgx.CollectRows(rows, s.scan)
}

func (s *Store) scan(row pgx.CollectableRow) (connect.Connection, error) {
	var c connect.Connection
	var status string
	var refresh []byte
	if err := row.Scan(&c.Owner, &c.Product, &status, &refresh, &c.Scopes, &c.Resources, &c.ConnectedAt); err != nil {
		return c, err
	}
	c.Status = connect.Status(status)
	if len(refresh) == 0 {
		return c, nil
	}
	var err error
	c.RefreshToken, err = s.sealer.Open(refresh, c.Owner)
	return c, err
}

func (s *Store) Rotate(ctx context.Context, owner, product string, fn func(connect.Connection) (connect.Connection, error)) (connect.Connection, error) {
	var out connect.Connection
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		locked, err := s.one(ctx, tx, selectGrant+` WHERE owner = $1 AND product = $2 FOR UPDATE`, owner, product)
		if err != nil {
			return err
		}
		out, err = fn(locked)
		if err != nil {
			return err
		}
		return s.save(ctx, tx, out)
	})
	return out, err
}

func (s *Store) MarkRevoked(ctx context.Context, owner, product string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE connect_grant SET status = 'revoked', refresh_token = NULL, updated_at = NOW()
		WHERE owner = $1 AND product = $2`, owner, product)
	if err != nil {
		return fmt.Errorf("mark revoked: %w", err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, owner, product string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM connect_grant WHERE owner = $1 AND product = $2`, owner, product); err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}
	return nil
}

func (s *Store) Reached(ctx context.Context, c connect.Connection, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	_, err := s.pool.Exec(ctx, `UPDATE connect_grant SET resources = $3 WHERE owner = $1 AND product = $2`, c.Owner, c.Product, ids)
	if err != nil {
		return fmt.Errorf("record reached: %w", err)
	}
	return nil
}

func (s *Store) Purge(ctx context.Context, owner string) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM connect_grant WHERE owner = $1`, owner); err != nil {
			return fmt.Errorf("purge grants: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM connect_grant_pending WHERE owner = $1`, owner); err != nil {
			return fmt.Errorf("purge pending: %w", err)
		}
		return nil
	})
}
