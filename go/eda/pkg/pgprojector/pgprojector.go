// Package pgprojector maintains a Postgres read model from an event stream,
// idempotently. It is the packaged form of a pattern otherwise reimplemented in
// every projector: for each event, open one transaction that (1) skips the
// event if it was already applied, (2) runs the app's read-model mutation, and
// (3) records the event as applied — then commits. Dedup and mutation share the
// single transaction, so a crash between the mutation and the dedup-insert can
// never leave the read model updated without its idempotency marker (or vice
// versa).
//
// The durable pull, ack/nak, staged backoff, DLQ, heartbeat and reconnect all
// come from pkg/consumer: a Projector is a consumer.EventHandler, wired with
// consumer.Run. The application supplies only EventID (how to read the event's
// unique id from a message) and Apply (the read-model mutation).
//
// Idempotency model: a simple 2-state "seen (consumer, event_id)?" gate, scoped
// to the projector's Consumer name and co-located in the mutation transaction.
// This differs from pkg/idempotency/pgx's 3-state pre-claim store, which gates
// an expensive handler BEFORE it runs across replicas; here the whole apply is
// one cheap transaction, so a post-hoc dedup in the same tx is the right shape.
package pgprojector

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

// Schema is the canonical DDL for the dedup table. It is the same table
// pkg/idempotency/pgx uses (status/claimed_at columns are simply unused here),
// so a project using both shares one table.
const Schema = `
CREATE TABLE IF NOT EXISTS event_idempotency (
    consumer     TEXT        NOT NULL,
    event_id     TEXT        NOT NULL,
    processed_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (consumer, event_id)
);
`

// Tx is the transaction handle passed to Apply. pgx.Tx satisfies it; the app
// runs its read-model upserts/updates on this handle so they commit atomically
// with the dedup marker.
type Tx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Config configures a Projector.
type Config struct {
	// Name identifies the projector in logs/metrics. Required.
	Name string
	// Subject is the NATS subject filter bound by the consumer. Required.
	Subject string
	// Durable is the durable consumer name. Required; also used as the
	// idempotency Consumer scope unless Consumer is set.
	Durable string
	// Consumer is the (consumer, event_id) dedup scope. Defaults to Durable.
	Consumer string
	// MaxDeliver bounds delivery attempts before dead-lettering. Default: 5.
	MaxDeliver int
	// EventID extracts the event's unique id from a message. Returning "" makes
	// the event non-idempotent — the projector rejects it as permanent (a
	// read-model event without an id cannot be safely de-duplicated). Required.
	EventID func(msg *nats.Msg) (string, error)
	// Apply runs the read-model mutation for one event on tx. It is called only
	// when the event has not been applied before. Return nil to commit
	// (mutation + dedup marker), or an error to roll back and retry. Required.
	Apply func(ctx context.Context, tx Tx, msg *nats.Msg) error
}

// Projector is a consumer.EventHandler that applies events to a Postgres read
// model idempotently. Build with New and wire with consumer.Run.
type Projector struct {
	pool *pgxpool.Pool
	cfg  Config
}

// New builds a Projector over pool.
func New(pool *pgxpool.Pool, cfg Config) *Projector {
	if cfg.Consumer == "" {
		cfg.Consumer = cfg.Durable
	}
	if cfg.MaxDeliver <= 0 {
		cfg.MaxDeliver = 5
	}
	return &Projector{pool: pool, cfg: cfg}
}

func (p *Projector) Name() string        { return p.cfg.Name }
func (p *Projector) Subject() string     { return p.cfg.Subject }
func (p *Projector) DurableName() string { return p.cfg.Durable }
func (p *Projector) MaxDeliver() int     { return p.cfg.MaxDeliver }

// Handle applies one message in a single transaction: dedup-check → Apply →
// dedup-insert → commit. A duplicate is committed as a no-op (idempotent skip).
func (p *Projector) Handle(ctx context.Context, msg *nats.Msg) error {
	eventID, err := p.cfg.EventID(msg)
	if err != nil {
		return consumer.Permanent(fmt.Errorf("read event id: %w", err))
	}
	if eventID == "" {
		return consumer.Permanent(errors.New("event has no id; cannot dedup a read-model event"))
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Already applied? Commit an empty tx as an idempotent skip.
	var seen string
	err = tx.QueryRow(ctx,
		`SELECT event_id FROM event_idempotency WHERE consumer = $1 AND event_id = $2`,
		p.cfg.Consumer, eventID,
	).Scan(&seen)
	if err == nil {
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("dedup check: %w", err)
	}

	if err := p.cfg.Apply(ctx, tx, msg); err != nil {
		return err // rolled back by defer; consumer naks/retries
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO event_idempotency (consumer, event_id) VALUES ($1, $2)
		 ON CONFLICT (consumer, event_id) DO NOTHING`,
		p.cfg.Consumer, eventID,
	); err != nil {
		return fmt.Errorf("mark applied: %w", err)
	}
	return tx.Commit(ctx)
}

var _ consumer.EventHandler = (*Projector)(nil)
