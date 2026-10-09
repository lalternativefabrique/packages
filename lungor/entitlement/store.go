package entitlement

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Migration creates the snapshot table PG reads and writes.
//
//go:embed migration.sql
var Migration string

// Snapshot is the last plan read from Lungor for a subject.
type Snapshot struct {
	Plan        string
	Limits      map[string]int64
	RefreshedAt time.Time
}

// Store keeps snapshots across restarts, so steady state needs no Lungor call.
type Store interface {
	Get(ctx context.Context, subject string) (Snapshot, bool, error)
	Put(ctx context.Context, subject string, s Snapshot) error
}

// MemoryStore is a Store for tests and single-process tools.
type MemoryStore struct {
	mu   sync.Mutex
	rows map[string]Snapshot
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{rows: map[string]Snapshot{}} }

func (m *MemoryStore) Get(_ context.Context, subject string) (Snapshot, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[subject]
	s.Limits = maps.Clone(s.Limits)
	return s, ok, nil
}

func (m *MemoryStore) Put(_ context.Context, subject string, s Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.Limits = maps.Clone(s.Limits)
	m.rows[subject] = s
	return nil
}

// DB is the part of a pgx pool or connection PG needs.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PG stores snapshots in the entitlements table created by Migration.
func PG(db DB) Store { return pgStore{db: db} }

type pgStore struct{ db DB }

func (p pgStore) Get(ctx context.Context, subject string) (Snapshot, bool, error) {
	var (
		s   Snapshot
		raw []byte
	)
	err := p.db.QueryRow(ctx,
		`SELECT plan, limits, refreshed_at FROM entitlements WHERE subject = $1`, subject,
	).Scan(&s.Plan, &raw, &s.RefreshedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	if err := json.Unmarshal(raw, &s.Limits); err != nil {
		return Snapshot{}, false, fmt.Errorf("entitlement: decode limits: %w", err)
	}
	return s, true, nil
}

func (p pgStore) Put(ctx context.Context, subject string, s Snapshot) error {
	raw, err := json.Marshal(s.Limits)
	if err != nil {
		return err
	}
	_, err = p.db.Exec(ctx, `
		INSERT INTO entitlements (subject, plan, limits, refreshed_at)
		VALUES ($1, $2, $3::jsonb, $4)
		ON CONFLICT (subject) DO UPDATE
		SET plan = EXCLUDED.plan, limits = EXCLUDED.limits, refreshed_at = EXCLUDED.refreshed_at`,
		subject, s.Plan, string(raw), s.RefreshedAt)
	return err
}
