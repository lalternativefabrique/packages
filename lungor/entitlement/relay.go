package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalternativefabrique/eventkit/pkg/outbox"
	outboxpgx "github.com/lalternativefabrique/eventkit/pkg/outbox/pgx"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

const statusRejected = "rejected"

// OutboxConfig tunes RunOutbox. Zero values take the eda relay defaults.
type OutboxConfig struct {
	Relay outbox.LeaseRelayConfig
	// ResyncEvery is how often movements Lungor lost are queued again.
	// Default 1h; negative disables it.
	ResyncEvery time.Duration
	// ResyncWindow is how far back a resync compares. Default 7 days.
	ResyncWindow time.Duration
}

// RunOutbox relays the movements ConsumeTx, RecordTx and ReleaseTx queued
// until ctx ends. Start it once at boot; several instances may run, the eda
// lease keeps a movement with one of them at a time.
func (g *Gate) RunOutbox(ctx context.Context, pool *pgxpool.Pool, cfg OutboxConfig) error {
	if cfg.ResyncEvery == 0 {
		cfg.ResyncEvery = time.Hour
	}
	if cfg.ResyncWindow <= 0 {
		cfg.ResyncWindow = 7 * 24 * time.Hour
	}
	store := rejectingStore{LeaseStore: outboxpgx.NewLeaseStore(pool, outboxpgx.WithTable(OutboxTable)), pool: pool}
	relay := outbox.NewLeaseRelay(store, outbox.RawPublisherFunc(func(ctx context.Context, topic string, payload []byte) error {
		return g.deliver(ctx, pool, topic, payload)
	}), cfg.Relay)

	if cfg.ResyncEvery > 0 {
		go func() {
			t := time.NewTicker(cfg.ResyncEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if n, err := g.Resync(ctx, pool, g.now().Add(-cfg.ResyncWindow)); err != nil {
						slog.Warn("entitlement: resync failed", "err", err)
					} else if n > 0 {
						slog.Warn("entitlement: movements Lungor lacked queued again", "count", n)
					}
				}
			}
		}()
	}
	return relay.Run(ctx)
}

// permanentError marks a movement Lungor will refuse however often it is sent.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func (g *Gate) deliver(ctx context.Context, pool *pgxpool.Pool, topic string, payload []byte) error {
	var m movement
	if err := json.Unmarshal(payload, &m); err != nil {
		return permanentError{fmt.Errorf("decode movement: %w", err)}
	}
	ledger := g.ledgerFor(m.Subject)
	u := sdk.Usage{
		ExternalUserID: m.Subject, Unit: m.Unit, Quantity: m.Quantity,
		IdempotencyKey: m.IdempotencyKey, OccurredAt: m.OccurredAt,
	}
	var err error
	switch topic {
	case topicConsume:
		u.Settled = true
		var d sdk.Decision
		d, err = ledger.Consume(ctx, u)
		if err == nil && (d.Overrun || !d.Allowed) {
			slog.Warn("entitlement: movement recorded past the allowance",
				"subject", m.Subject, "unit", m.Unit, "quantity", m.Quantity, "key", m.IdempotencyKey)
		}
	case topicRelease:
		u.ReleasesKey = m.ReleasesKey
		_, err = ledger.Release(ctx, u)
	default:
		return permanentError{fmt.Errorf("unknown topic %q", topic)}
	}
	if err != nil {
		if errors.Is(err, sdk.ErrBadRequest) || errors.Is(err, sdk.ErrConflict) || errors.Is(err, sdk.ErrUnprocessable) {
			return permanentError{err}
		}
		return err
	}
	g.forgetBalances(m.Subject, m.Unit)
	if err := g.recalibrate(ctx, pool, ledger, m.Subject, m.Unit, m.IdempotencyKey); err != nil {
		slog.Warn("entitlement: counter not recalibrated", "subject", m.Subject, "unit", m.Unit, "err", err)
	}
	return nil
}

// recalibrate sets the local counter to what Lungor holds plus what is still
// queued, leaving out the movement just delivered: Lungor counts it already,
// while its row is still pending until the relay marks it sent. Lungor is the truth; the counter is only how the app decides while
// Lungor cannot be reached. Locking the counter row first makes every debit
// committed before it visible to the pending sum.
func (g *Gate) recalibrate(ctx context.Context, pool *pgxpool.Pool, ledger Ledger, subject, unit, delivered string) error {
	b, err := ledger.Balance(ctx, subject, unit)
	if err != nil {
		return err
	}
	var periodEnd *time.Time
	if b.Periodic {
		if ent, err := ledger.Entitlement(ctx, subject, unit); err == nil {
			periodEnd = ent.CurrentPeriodEnd
		}
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO lungor_usage_counters (subject, unit) VALUES ($1, $2)
			ON CONFLICT (subject, unit) DO NOTHING`, subject, unit); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			SELECT 1 FROM lungor_usage_counters WHERE subject = $1 AND unit = $2 FOR UPDATE`, subject, unit); err != nil {
			return err
		}
		var pending int64
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(SUM(CASE WHEN topic = $3 THEN q ELSE -q END), 0)
			FROM (SELECT topic, (payload->>'quantity')::bigint AS q FROM `+OutboxTable+`
			      WHERE status = 'pending' AND payload->>'subject' = $1 AND payload->>'unit' = $2
			        AND payload->>'idempotency_key' <> $4) p`,
			subject, unit, topicConsume, delivered).Scan(&pending); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE lungor_usage_counters SET used = GREATEST($3, 0), period_end = $4, synced_at = $5
			WHERE subject = $1 AND unit = $2`, subject, unit, b.Consumed+pending, periodEnd, g.now().UTC())
		return err
	})
}

// Resync queues again every movement sent since `since` that Lungor does not
// hold, which covers a Lungor restore as well as a response lost after the
// write. It reports how many were queued.
func (g *Gate) Resync(ctx context.Context, pool *pgxpool.Pool, since time.Time) (int, error) {
	recorder, ok := g.cfg.Ledger.(interface {
		Recorded(ctx context.Context, since time.Time, after string, limit int) (sdk.RecordedPage, error)
	})
	if !ok {
		return 0, nil
	}
	held := map[string]struct{}{}
	after := ""
	for {
		page, err := recorder.Recorded(ctx, since, after, 0)
		if err != nil {
			return 0, err
		}
		for _, k := range page.Keys {
			held[k.IdempotencyKey] = struct{}{}
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}

	rows, err := pool.Query(ctx, `
		SELECT id, payload->>'idempotency_key', payload->>'subject' FROM `+OutboxTable+`
		WHERE status = 'sent' AND created_at >= $1`, since)
	if err != nil {
		return 0, err
	}
	var lost []int64
	for rows.Next() {
		var (
			id           int64
			key, subject string
		)
		if err := rows.Scan(&id, &key, &subject); err != nil {
			rows.Close()
			return 0, err
		}
		if _, ok := held[key]; !ok && g.ledgerFor(subject) == g.cfg.Ledger {
			lost = append(lost, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(lost) == 0 {
		return 0, nil
	}
	_, err = pool.Exec(ctx, `
		UPDATE `+OutboxTable+` SET status = 'pending', next_attempt_at = NOW(), updated_at = NOW()
		WHERE id = ANY($1)`, lost)
	return len(lost), err
}

// rejectingStore parks a movement Lungor refused for good, instead of letting
// the eda relay retry it forever; every other call is eda's.
type rejectingStore struct {
	*outboxpgx.LeaseStore
	pool *pgxpool.Pool
}

func (s rejectingStore) MarkFailed(ctx context.Context, id int64, cause error, retryAfter time.Duration) error {
	var perm permanentError
	if !errors.As(cause, &perm) {
		return s.LeaseStore.MarkFailed(ctx, id, cause, retryAfter)
	}
	slog.Error("entitlement: movement rejected by Lungor", "id", id, "err", cause)
	_, err := s.pool.Exec(ctx, `
		UPDATE `+OutboxTable+` SET status = $1, last_error = $2, updated_at = NOW() WHERE id = $3`,
		statusRejected, cause.Error(), id)
	return err
}
