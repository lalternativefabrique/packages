package entitlement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

// Inbox entry statuses.
const (
	InboxPending = "pending"
	InboxDone    = "done"
	InboxFailed  = "failed"
)

// InboxEntry is a verified Lungor delivery waiting to be applied.
type InboxEntry struct {
	SourceEventID string
	DeliveryID    string
	Type          string
	Subject       string
	Payload       []byte
	Status        string
	Attempts      int
	LastError     string
	NextAttemptAt time.Time
	ReceivedAt    time.Time
	ProcessedAt   time.Time
}

func (e InboxEntry) delivery() sdk.Delivery {
	return sdk.Delivery{Type: e.Type, ID: e.DeliveryID, SourceEventID: e.SourceEventID, Timestamp: e.ReceivedAt, Payload: e.Payload}
}

// Inbox keeps verified deliveries until RunInbox applied them, so a delivery
// acknowledged to Lungor is never lost to a failing hook or ledger.
type Inbox interface {
	// Insert stores e as pending; a source event already stored is a no-op
	// reporting false.
	Insert(ctx context.Context, e InboxEntry) (bool, error)
	// Claim returns up to limit pending entries due at now and pushes their
	// next attempt to now+lease, so concurrent workers skip them.
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]InboxEntry, error)
	Done(ctx context.Context, sourceEventID string, at time.Time) error
	Retry(ctx context.Context, sourceEventID string, attempts int, lastErr string, next time.Time) error
	Fail(ctx context.Context, sourceEventID string, attempts int, lastErr string, at time.Time) error
	// Replay resets failed entries received at or after since to pending.
	Replay(ctx context.Context, since, now time.Time) (int, error)
}

// InboxConfig tunes RunInbox. Zero values take the defaults.
type InboxConfig struct {
	// Poll is how often due entries are looked for. Default 5s.
	Poll time.Duration
	// MaxAttempts before an entry is marked failed. Default 10.
	MaxAttempts int
	// Backoff is the delay after the first failure, doubled per attempt up to
	// MaxBackoff. Defaults 5s and 10m.
	Backoff    time.Duration
	MaxBackoff time.Duration
	// Batch is how many entries one poll claims. Default 50.
	Batch int
}

func (c InboxConfig) withDefaults() InboxConfig {
	if c.Poll <= 0 {
		c.Poll = 5 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 10
	}
	if c.Backoff <= 0 {
		c.Backoff = 5 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 10 * time.Minute
	}
	if c.Batch <= 0 {
		c.Batch = 50
	}
	return c
}

func (c InboxConfig) backoff(attempts int) time.Duration {
	d := c.Backoff
	for i := 1; i < attempts && d < c.MaxBackoff; i++ {
		d *= 2
	}
	return min(d, c.MaxBackoff)
}

// RunInbox applies the deliveries Webhook stored in Config.Inbox until ctx
// ends. Start it once at boot when an Inbox is configured.
func (g *Gate) RunInbox(ctx context.Context, cfg InboxConfig) error {
	if g.cfg.Inbox == nil {
		return errors.New("entitlement: RunInbox needs Config.Inbox")
	}
	cfg = cfg.withDefaults()
	t := time.NewTicker(cfg.Poll)
	defer t.Stop()
	for {
		g.drainInbox(ctx, cfg)
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		case <-g.inboxWake:
		}
	}
}

func (g *Gate) drainInbox(ctx context.Context, cfg InboxConfig) {
	for ctx.Err() == nil {
		entries, err := g.cfg.Inbox.Claim(ctx, g.now(), ledgerTimeout*2, cfg.Batch)
		if err != nil {
			slog.Error("entitlement: inbox claim failed", "err", err)
			return
		}
		for _, e := range entries {
			g.processEntry(ctx, cfg, e)
		}
		if len(entries) < cfg.Batch {
			return
		}
	}
}

func (g *Gate) processEntry(ctx context.Context, cfg InboxConfig, e InboxEntry) {
	runCtx, cancel := context.WithTimeout(ctx, ledgerTimeout)
	err := g.applyDelivery(runCtx, g.deliveryHook(), e.delivery())
	cancel()
	if err == nil {
		if err := g.cfg.Inbox.Done(ctx, e.SourceEventID, g.now()); err != nil {
			slog.Error("entitlement: inbox entry not marked done", "event_id", e.SourceEventID, "err", err)
		}
		return
	}
	attempts := e.Attempts + 1
	if attempts >= cfg.MaxAttempts {
		slog.Error("entitlement: webhook delivery abandoned",
			"subject", e.Subject, "type", e.Type, "event_id", e.SourceEventID, "attempts", attempts, "err", err)
		if ferr := g.cfg.Inbox.Fail(ctx, e.SourceEventID, attempts, err.Error(), g.now()); ferr != nil {
			slog.Error("entitlement: inbox entry not marked failed", "event_id", e.SourceEventID, "err", ferr)
		}
		return
	}
	slog.Warn("entitlement: webhook delivery failed, retrying",
		"subject", e.Subject, "type", e.Type, "event_id", e.SourceEventID, "attempts", attempts, "err", err)
	if rerr := g.cfg.Inbox.Retry(ctx, e.SourceEventID, attempts, err.Error(), g.now().Add(cfg.backoff(attempts))); rerr != nil {
		slog.Error("entitlement: inbox retry not scheduled", "event_id", e.SourceEventID, "err", rerr)
	}
}

// ReplayInbox queues again the deliveries marked failed since then.
func (g *Gate) ReplayInbox(ctx context.Context, since time.Time) (int, error) {
	if g.cfg.Inbox == nil {
		return 0, errors.New("entitlement: ReplayInbox needs Config.Inbox")
	}
	n, err := g.cfg.Inbox.Replay(ctx, since, g.now())
	if err != nil {
		return 0, fmt.Errorf("entitlement: replay inbox: %w", err)
	}
	if n > 0 {
		g.wakeInbox()
	}
	return n, nil
}

func (g *Gate) wakeInbox() {
	select {
	case g.inboxWake <- struct{}{}:
	default:
	}
}

func (g *Gate) setDeliveryHook(fn func(ctx context.Context, d sdk.Delivery) error) {
	g.mu.Lock()
	g.onDelivery = fn
	g.mu.Unlock()
}

func (g *Gate) deliveryHook() func(ctx context.Context, d sdk.Delivery) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.onDelivery
}

// MemoryInbox is an Inbox for tests and single-process tools.
type MemoryInbox struct {
	mu   sync.Mutex
	rows map[string]InboxEntry
}

func NewMemoryInbox() *MemoryInbox { return &MemoryInbox{rows: map[string]InboxEntry{}} }

func (m *MemoryInbox) Insert(_ context.Context, e InboxEntry) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[e.SourceEventID]; ok {
		return false, nil
	}
	e.Status, e.Payload = InboxPending, slices.Clone(e.Payload)
	m.rows[e.SourceEventID] = e
	return true, nil
}

func (m *MemoryInbox) Claim(_ context.Context, now time.Time, lease time.Duration, limit int) ([]InboxEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []InboxEntry
	for id, e := range m.rows {
		if len(out) == limit {
			break
		}
		if e.Status != InboxPending || e.NextAttemptAt.After(now) {
			continue
		}
		e.NextAttemptAt = now.Add(lease)
		m.rows[id] = e
		out = append(out, e)
	}
	return out, nil
}

func (m *MemoryInbox) Done(_ context.Context, id string, at time.Time) error {
	return m.update(id, func(e *InboxEntry) { e.Status, e.ProcessedAt, e.LastError = InboxDone, at, "" })
}

func (m *MemoryInbox) Retry(_ context.Context, id string, attempts int, lastErr string, next time.Time) error {
	return m.update(id, func(e *InboxEntry) { e.Attempts, e.LastError, e.NextAttemptAt = attempts, lastErr, next })
}

func (m *MemoryInbox) Fail(_ context.Context, id string, attempts int, lastErr string, at time.Time) error {
	return m.update(id, func(e *InboxEntry) {
		e.Status, e.Attempts, e.LastError, e.ProcessedAt = InboxFailed, attempts, lastErr, at
	})
}

func (m *MemoryInbox) Replay(_ context.Context, since, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, e := range m.rows {
		if e.Status != InboxFailed || e.ReceivedAt.Before(since) {
			continue
		}
		e.Status, e.Attempts, e.NextAttemptAt = InboxPending, 0, now
		m.rows[id] = e
		n++
	}
	return n, nil
}

// Entry returns the stored entry for a source event.
func (m *MemoryInbox) Entry(id string) (InboxEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	return e, ok
}

func (m *MemoryInbox) update(id string, fn func(*InboxEntry)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.rows[id]
	if !ok {
		return fmt.Errorf("entitlement: inbox entry %s not found", id)
	}
	fn(&e)
	m.rows[id] = e
	return nil
}

// InboxDB is the part of a pgx pool PGInbox needs.
type InboxDB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PGInbox stores deliveries in the entitlement_inbox table created by
// Migration.
func PGInbox(db InboxDB) Inbox { return pgInbox{db: db} }

type pgInbox struct{ db InboxDB }

func (p pgInbox) Insert(ctx context.Context, e InboxEntry) (bool, error) {
	tag, err := p.db.Exec(ctx, `
		INSERT INTO entitlement_inbox (source_event_id, delivery_id, type, subject, payload, status, next_attempt_at, received_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'pending', $6, $6)
		ON CONFLICT (source_event_id) DO NOTHING`,
		e.SourceEventID, e.DeliveryID, e.Type, e.Subject, string(e.Payload), e.ReceivedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (p pgInbox) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]InboxEntry, error) {
	rows, err := p.db.Query(ctx, `
		UPDATE entitlement_inbox SET next_attempt_at = $2
		WHERE source_event_id IN (
			SELECT source_event_id FROM entitlement_inbox
			WHERE status = 'pending' AND next_attempt_at <= $1
			ORDER BY next_attempt_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED)
		RETURNING source_event_id, delivery_id, type, subject, payload, attempts, received_at`,
		now, now.Add(lease), limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (InboxEntry, error) {
		e := InboxEntry{Status: InboxPending}
		err := row.Scan(&e.SourceEventID, &e.DeliveryID, &e.Type, &e.Subject, &e.Payload, &e.Attempts, &e.ReceivedAt)
		return e, err
	})
}

func (p pgInbox) Done(ctx context.Context, id string, at time.Time) error {
	_, err := p.db.Exec(ctx,
		`UPDATE entitlement_inbox SET status = 'done', processed_at = $2, last_error = NULL WHERE source_event_id = $1`, id, at)
	return err
}

func (p pgInbox) Retry(ctx context.Context, id string, attempts int, lastErr string, next time.Time) error {
	_, err := p.db.Exec(ctx,
		`UPDATE entitlement_inbox SET attempts = $2, last_error = $3, next_attempt_at = $4 WHERE source_event_id = $1`,
		id, attempts, lastErr, next)
	return err
}

func (p pgInbox) Fail(ctx context.Context, id string, attempts int, lastErr string, at time.Time) error {
	_, err := p.db.Exec(ctx,
		`UPDATE entitlement_inbox SET status = 'failed', attempts = $2, last_error = $3, processed_at = $4 WHERE source_event_id = $1`,
		id, attempts, lastErr, at)
	return err
}

func (p pgInbox) Replay(ctx context.Context, since, now time.Time) (int, error) {
	tag, err := p.db.Exec(ctx, `
		UPDATE entitlement_inbox SET status = 'pending', attempts = 0, next_attempt_at = $2, processed_at = NULL
		WHERE status = 'failed' AND received_at >= $1`, since, now)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
