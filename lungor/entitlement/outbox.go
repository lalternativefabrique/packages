package entitlement

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	outboxpgx "github.com/lalternativefabrique/eventkit/pkg/outbox/pgx"
)

// OutboxTable holds the movements waiting to reach Lungor. It is the generic
// eda outbox table under its own name, so it never mixes with an app's events.
const OutboxTable = "lungor_outbox"

const (
	topicConsume = "lungor.consume"
	topicRelease = "lungor.release"
)

//go:embed outbox_migration.sql
var outboxExtras string

// OutboxMigration creates the outbox and the local counters ConsumeTx,
// RecordTx and ReleaseTx write to. Apply it through the app's own migrations.
var OutboxMigration = strings.ReplaceAll(outboxpgx.Schema, "outbox_events", OutboxTable) + outboxExtras

type movement struct {
	Subject        string    `json:"subject"`
	Unit           string    `json:"unit"`
	Quantity       int64     `json:"quantity"`
	IdempotencyKey string    `json:"idempotency_key"`
	OccurredAt     time.Time `json:"occurred_at"`
	ReleasesKey    string    `json:"releases_key,omitempty"`
}

// ConsumeTx debits n of unit inside the caller's transaction: it decides on
// the local counter and the cached limit, and queues the debit for Lungor.
// The action and its debit commit together or not at all. A refusal is a
// *LimitError and writes nothing; roll the transaction back.
//
// Postgres serialises concurrent debits of the same subject and unit on the
// counter row, so two of them cannot both pass under the limit.
func (g *Gate) ConsumeTx(ctx context.Context, tx pgx.Tx, subject, unit string, n int64, idemKey string) error {
	return g.debitTx(ctx, tx, subject, unit, n, idemKey, true)
}

// RecordTx is ConsumeTx for an act that already happened: it never refuses,
// and an overrun is Lungor's to report.
func (g *Gate) RecordTx(ctx context.Context, tx pgx.Tx, subject, unit string, n int64, idemKey string) error {
	return g.debitTx(ctx, tx, subject, unit, n, idemKey, false)
}

// ReleaseTx gives back n of unit inside the caller's transaction: a held slot
// of a capacity unit, or, with releasesKey set, the surplus of the metered
// consumption that key names.
func (g *Gate) ReleaseTx(ctx context.Context, tx pgx.Tx, subject, unit string, n int64, idemKey, releasesKey string) error {
	if err := validMovement(subject, unit, n, idemKey); err != nil {
		return err
	}
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	inserted, err := enqueue(ctx, tx, topicRelease, movement{
		Subject: subject, Unit: unit, Quantity: n, IdempotencyKey: idemKey,
		OccurredAt: g.now().UTC(), ReleasesKey: releasesKey,
	})
	if err != nil || !inserted {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE lungor_usage_counters SET used = GREATEST(used - $3, 0)
		WHERE subject = $1 AND unit = $2`, subject, unit, n)
	return err
}

func (g *Gate) debitTx(ctx context.Context, tx pgx.Tx, subject, unit string, n int64, idemKey string, capped bool) error {
	if err := validMovement(subject, unit, n, idemKey); err != nil {
		return err
	}
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	s, err := g.snapshot(ctx, subject)
	if err != nil {
		return err
	}
	limit := g.limitOf(s, unit)
	now := g.now().UTC()

	if _, err := tx.Exec(ctx, `
		INSERT INTO lungor_usage_counters (subject, unit) VALUES ($1, $2)
		ON CONFLICT (subject, unit) DO NOTHING`, subject, unit); err != nil {
		return fmt.Errorf("entitlement: counter: %w", err)
	}
	var used int64
	if err := tx.QueryRow(ctx, `
		SELECT CASE WHEN period_end IS NOT NULL AND period_end <= $3 THEN 0 ELSE used END
		FROM lungor_usage_counters WHERE subject = $1 AND unit = $2
		FOR UPDATE`, subject, unit, now).Scan(&used); err != nil {
		return fmt.Errorf("entitlement: counter: %w", err)
	}
	var replay bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM `+OutboxTable+` WHERE payload->>'idempotency_key' = $1)`, idemKey).Scan(&replay); err != nil {
		return fmt.Errorf("entitlement: replay check: %w", err)
	}
	if replay {
		return nil
	}
	if capped && limit < Unlimited && n > limit-used {
		return &LimitError{Unit: unit, Limit: limit, Used: used, Plan: s.Plan}
	}

	inserted, err := enqueue(ctx, tx, topicConsume, movement{
		Subject: subject, Unit: unit, Quantity: n, IdempotencyKey: idemKey, OccurredAt: now,
	})
	if err != nil || !inserted {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE lungor_usage_counters
		SET used = CASE WHEN period_end IS NOT NULL AND period_end <= $4 THEN $3 ELSE used + $3 END,
		    period_end = CASE WHEN period_end IS NOT NULL AND period_end <= $4 THEN NULL ELSE period_end END
		WHERE subject = $1 AND unit = $2`, subject, unit, n, now)
	return err
}

// enqueue reports false when the key is already queued: a replay inside the
// app counts nothing twice.
func enqueue(ctx context.Context, tx pgx.Tx, topic string, m movement) (bool, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO `+OutboxTable+` (topic, ordering_key, payload)
		VALUES ($1, $2, $3::jsonb)
		ON CONFLICT ((payload->>'idempotency_key')) DO NOTHING`,
		topic, m.Subject+"/"+m.Unit, string(payload))
	if err != nil {
		return false, fmt.Errorf("entitlement: enqueue: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

var errInvalidMovement = errors.New("entitlement: subject, unit, a positive quantity and an idempotency key are required")

func validMovement(subject, unit string, n int64, idemKey string) error {
	if subject == "" || unit == "" || n <= 0 || idemKey == "" {
		return errInvalidMovement
	}
	return nil
}
