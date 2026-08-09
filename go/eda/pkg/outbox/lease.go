// lease.go implements the lease-based transactional outbox relay.
//
// It is an alternative to the Store/Relay pair in outbox.go, tuned for the
// common SQL shape where rows carry a raw (topic, payload) body rather than a
// typed EventEnvelope, and where recovery must be automatic.
//
// The lease model has NO "processing" state. Claiming a batch keeps the rows
// pending but pushes their next_attempt_at a lease into the future, so:
//
//   - concurrent relays never grab the same rows (FOR UPDATE SKIP LOCKED during
//     the claim), and
//   - a relay that dies after claiming but before marking sent does not strand
//     the row — the lease simply expires and the row becomes eligible again.
//
// This makes recovery self-healing, with no separate reaper for orphaned
// in-flight rows. The trade-off is at-least-once: a merely-slow (not dead)
// relay can have its lease expire and a row publish twice, so LeaseDuration
// must comfortably exceed the publish round-trip and consumers must be
// idempotent (the pkg/consumer IdempotencyStore covers that).
package outbox

import (
	"context"
	"time"
)

// RawRecord is one outbox row as the lease relay sees it: a raw body destined
// for a subject/topic, plus the attempt counter used for backoff.
type RawRecord struct {
	// ID is the storage primary key (opaque to the relay).
	ID int64
	// Topic is the NATS subject the payload is published on.
	Topic string
	// Payload is the raw, already-serialized event body, published verbatim.
	Payload []byte
	// Attempts is the number of delivery attempts so far (incremented by the
	// store when the row is claimed).
	Attempts int
	// Headers travel with the payload to the broker. They carry what the
	// producer knew and the relay cannot reconstruct — a trace context above
	// all, since the relay's own context belongs to its polling loop, not to
	// the request that enqueued the row. Empty unless the store reads them.
	Headers map[string]string
}

// LeaseStore is the persistence seam for the lease relay. Implementations must
// be goroutine-safe and are typically backed by a single SQL table.
type LeaseStore interface {
	// ClaimBatch atomically selects up to limit due pending rows and leases
	// them: it MUST, in one transaction, (a) select pending rows whose
	// next_attempt_at is null or due, oldest first, under a row lock that other
	// relays skip (FOR UPDATE SKIP LOCKED), and (b) push their next_attempt_at
	// out by lease and increment attempts — WITHOUT changing status away from
	// pending. Returning the claimed rows (with their pre-increment or
	// post-increment attempts; the relay only uses Attempts for backoff).
	ClaimBatch(ctx context.Context, limit int, lease time.Duration) ([]RawRecord, error)
	// MarkSent promotes a successfully published row to its terminal sent state.
	MarkSent(ctx context.Context, id int64) error
	// MarkFailed records a publish failure and schedules the next attempt by
	// setting next_attempt_at = NOW() + retryAfter, keeping the row pending.
	MarkFailed(ctx context.Context, id int64, cause error, retryAfter time.Duration) error
}

// RawPublisher publishes a raw payload on a topic. A JetStream client is the
// typical implementation; it is an interface so the relay stays transport- and
// otel-agnostic.
type RawPublisher interface {
	Publish(ctx context.Context, topic string, payload []byte) error
}

// RawPublisherFunc adapts a function to RawPublisher.
type RawPublisherFunc func(ctx context.Context, topic string, payload []byte) error

func (f RawPublisherFunc) Publish(ctx context.Context, topic string, payload []byte) error {
	return f(ctx, topic, payload)
}

// RawHeaderPublisher is the optional counterpart of RawPublisher for
// transports that carry headers. The relay prefers it when the publisher
// implements it, so adding headers never broke an existing RawPublisher.
type RawHeaderPublisher interface {
	PublishWithHeaders(ctx context.Context, topic string, payload []byte, headers map[string]string) error
}

// LeaseRelayConfig tunes a LeaseRelay. The zero value is usable via defaults.
type LeaseRelayConfig struct {
	// BatchSize is the maximum rows claimed per pass. Default: 25.
	BatchSize int
	// PollInterval is the wait between passes in Run. Default: 1s.
	PollInterval time.Duration
	// LeaseDuration is how long a claimed row is hidden from the next scan. It
	// must comfortably exceed the publish round-trip so a live relay is never
	// double-published. Default: 60s.
	LeaseDuration time.Duration
	// BaseBackoff is the first retry delay after a publish failure; each further
	// attempt doubles it, capped at MaxBackoff. Default: 10s.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff. Default: 600s.
	MaxBackoff time.Duration
	// OnError, when set, is called for each publish failure (after MarkFailed).
	OnError func(rec RawRecord, err error)
	// OnBatch, when set, is called with the count of rows published in a pass
	// that published at least one (for logging/metrics).
	OnBatch func(published int)
}

func (c *LeaseRelayConfig) withDefaults() {
	if c.BatchSize <= 0 {
		c.BatchSize = 25
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 1 * time.Second
	}
	if c.LeaseDuration <= 0 {
		c.LeaseDuration = 60 * time.Second
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 10 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 600 * time.Second
	}
}

// LeaseRelay drains a LeaseStore to a RawPublisher using the lease model.
type LeaseRelay struct {
	store LeaseStore
	pub   RawPublisher
	cfg   LeaseRelayConfig
}

// NewLeaseRelay builds a lease relay.
func NewLeaseRelay(store LeaseStore, pub RawPublisher, cfg LeaseRelayConfig) *LeaseRelay {
	cfg.withDefaults()
	return &LeaseRelay{store: store, pub: pub, cfg: cfg}
}

// Run loops until ctx is cancelled, relaying one batch per PollInterval tick.
func (r *LeaseRelay) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			n, err := r.RelayOnce(ctx)
			if err != nil {
				if r.cfg.OnError != nil {
					r.cfg.OnError(RawRecord{}, err)
				}
				continue
			}
			if n > 0 && r.cfg.OnBatch != nil {
				r.cfg.OnBatch(n)
			}
		}
	}
}

func (r *LeaseRelay) publish(ctx context.Context, row RawRecord) error {
	if hp, ok := r.pub.(RawHeaderPublisher); ok {
		return hp.PublishWithHeaders(ctx, row.Topic, row.Payload, row.Headers)
	}
	return r.pub.Publish(ctx, row.Topic, row.Payload)
}

// RelayOnce claims a batch, publishes each row, and marks sent/failed. It
// returns the number of rows successfully published.
func (r *LeaseRelay) RelayOnce(ctx context.Context) (int, error) {
	rows, err := r.store.ClaimBatch(ctx, r.cfg.BatchSize, r.cfg.LeaseDuration)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, row := range rows {
		if perr := r.publish(ctx, row); perr != nil {
			_ = r.store.MarkFailed(ctx, row.ID, perr, r.backoffFor(row.Attempts))
			if r.cfg.OnError != nil {
				r.cfg.OnError(row, perr)
			}
			continue
		}
		if merr := r.store.MarkSent(ctx, row.ID); merr != nil {
			// Published but not marked: the lease expires and the row publishes
			// again (at-least-once). Surface it; don't fail the whole pass.
			if r.cfg.OnError != nil {
				r.cfg.OnError(row, merr)
			}
			continue
		}
		published++
	}
	return published, nil
}

// backoffFor is exponential in attempts, clamped to [BaseBackoff, MaxBackoff].
// attempts is the row's current attempt count (>= 1 for a row being retried).
func (r *LeaseRelay) backoffFor(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := r.cfg.BaseBackoff
	for i := 1; i < attempts; i++ {
		d *= 2
		if d >= r.cfg.MaxBackoff {
			return r.cfg.MaxBackoff
		}
	}
	if d > r.cfg.MaxBackoff {
		d = r.cfg.MaxBackoff
	}
	return d
}
