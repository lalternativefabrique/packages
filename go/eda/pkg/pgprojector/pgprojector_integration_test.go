//go:build integration

// Run: DATABASE_URL=... go test -tags=integration ./pkg/pgprojector/...
package pgprojector

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func poolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("postgres not reachable: %v", err)
	}
	return pool
}

// setup applies the dedup schema + a tiny read-model table, and returns a clean pool.
func setup(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, Schema)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS rm_endpoint (id TEXT PRIMARY KEY, hits INT NOT NULL DEFAULT 0)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `TRUNCATE event_idempotency, rm_endpoint`)
	require.NoError(t, err)
}

func msgWith(eventID, id string) *nats.Msg {
	return &nats.Msg{Header: nats.Header{"Event-Id": []string{eventID}, "Agg": []string{id}}}
}

func newProjector(pool *pgxpool.Pool) *Projector {
	return New(pool, Config{
		Name: "rm", Subject: "e.>", Durable: "rm-proj",
		EventID: func(m *nats.Msg) (string, error) { return m.Header.Get("Event-Id"), nil },
		Apply: func(ctx context.Context, tx Tx, m *nats.Msg) error {
			// Upsert that increments a counter — so a re-apply is observable.
			_, err := tx.Exec(ctx, `
				INSERT INTO rm_endpoint (id, hits) VALUES ($1, 1)
				ON CONFLICT (id) DO UPDATE SET hits = rm_endpoint.hits + 1
			`, m.Header.Get("Agg"))
			return err
		},
	})
}

func hits(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var n int
	// No row yet → 0 hits. Any real error fails the test.
	err := pool.QueryRow(context.Background(), `SELECT hits FROM rm_endpoint WHERE id=$1`, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0
	}
	require.NoError(t, err)
	return n
}

func TestIntegration_AppliesOnce(t *testing.T) {
	pool := poolOrSkip(t)
	defer pool.Close()
	setup(t, pool)
	p := newProjector(pool)
	ctx := context.Background()

	// First delivery applies the mutation.
	require.NoError(t, p.Handle(ctx, msgWith("ev-1", "agg-1")))
	assert.Equal(t, 1, hits(t, pool, "agg-1"))

	// Redelivery of the SAME event is an idempotent skip — hits stays 1.
	require.NoError(t, p.Handle(ctx, msgWith("ev-1", "agg-1")))
	assert.Equal(t, 1, hits(t, pool, "agg-1"), "duplicate event must not re-apply")

	// A different event on the same aggregate applies again.
	require.NoError(t, p.Handle(ctx, msgWith("ev-2", "agg-1")))
	assert.Equal(t, 2, hits(t, pool, "agg-1"))
}

func TestIntegration_ApplyErrorRollsBack(t *testing.T) {
	pool := poolOrSkip(t)
	defer pool.Close()
	setup(t, pool)
	ctx := context.Background()

	boom := errors.New("boom")
	p := New(pool, Config{
		Name: "rm", Subject: "e.>", Durable: "rm-proj",
		EventID: func(m *nats.Msg) (string, error) { return m.Header.Get("Event-Id"), nil },
		Apply:   func(context.Context, Tx, *nats.Msg) error { return boom },
	})

	err := p.Handle(ctx, msgWith("ev-1", "agg-1"))
	require.ErrorIs(t, err, boom)

	// Nothing committed: no read-model row AND no idempotency marker, so a retry
	// can re-run cleanly.
	assert.Equal(t, 0, hits(t, pool, "agg-1"))
	var marked int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM event_idempotency WHERE consumer='rm-proj' AND event_id='ev-1'`).Scan(&marked))
	assert.Equal(t, 0, marked, "failed apply must not leave a dedup marker")
}

func TestIntegration_MissingEventIDIsPermanent(t *testing.T) {
	pool := poolOrSkip(t)
	defer pool.Close()
	setup(t, pool)
	p := newProjector(pool)

	err := p.Handle(context.Background(), &nats.Msg{Header: nats.Header{}})
	require.Error(t, err)
	// consumer package treats this as permanent → the consumer will Term, not retry.
}
