// Package outbox implements the transactional outbox pattern in a
// storage-agnostic way: the user persists OutboxRecord rows in the same
// transaction as their state changes; a Relay worker later publishes them
// through a Publisher (typically an event bus or NATS) and marks them sent.
//
// The Store interface is the integration seam — implement it against your
// SQL/NoSQL of choice. An in-memory implementation is shipped for tests
// and local development.
package outbox

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Status is the lifecycle of an outbox record.
type Status int

const (
	StatusPending Status = iota
	StatusDispatched
	StatusFailed
)

// OutboxRecord is a single envelope queued for publication.
type OutboxRecord[ID comparable] struct {
	// RecordID is the storage primary key. The Store assigns it.
	RecordID string
	// Envelope is the event to publish.
	Envelope ddd.EventEnvelope[ID]
	// Status reflects current state. Pending until Relay dispatches it.
	Status Status
	// Attempts increments on every dispatch attempt (success or failure).
	Attempts int
	// LastError is the last failure reason (empty on success).
	LastError string
	// CreatedAt / UpdatedAt timestamps.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store is the persistence seam. Implementations must be goroutine-safe.
// Methods are designed to be implementable on top of SQL transactions
// (Enqueue inside the user's TX, FetchPending / MarkDispatched / MarkFailed
// in their own short transactions).
type Store[ID comparable] interface {
	// Enqueue adds a record. Implementations should set RecordID,
	// CreatedAt, UpdatedAt, and Status=Pending.
	Enqueue(ctx context.Context, env ddd.EventEnvelope[ID]) (OutboxRecord[ID], error)
	// FetchPending returns up to limit oldest pending records. It MUST NOT
	// block other relays from concurrently picking different records
	// (implementations typically use SELECT ... FOR UPDATE SKIP LOCKED or
	// a claim flag).
	FetchPending(ctx context.Context, limit int) ([]OutboxRecord[ID], error)
	// MarkDispatched promotes the record to StatusDispatched.
	MarkDispatched(ctx context.Context, recordID string) error
	// MarkFailed increments Attempts and records the error. retryAfter is the
	// delay the Relay computed before this record should be polled again;
	// SQL implementations persist it as next_attempt_at = NOW() + retryAfter and
	// gate FetchPending on (next_attempt_at IS NULL OR next_attempt_at <= NOW()),
	// so a failing record backs off instead of being retried on the very next
	// poll. When terminal is true the record exhausted its retries and should be
	// set to StatusFailed (retryAfter is then irrelevant).
	MarkFailed(ctx context.Context, recordID string, err error, retryAfter time.Duration, terminal bool) error
}

// Publisher is what the Relay calls to deliver an envelope. The CQRS
// TypedEventBus or a JetStream client are the typical implementations.
type Publisher[ID comparable] interface {
	Publish(ctx context.Context, env ddd.EventEnvelope[ID]) error
}

// PublisherFunc adapts a function.
type PublisherFunc[ID comparable] func(context.Context, ddd.EventEnvelope[ID]) error

func (f PublisherFunc[ID]) Publish(ctx context.Context, env ddd.EventEnvelope[ID]) error {
	return f(ctx, env)
}

// RelayConfig configures a Relay.
type RelayConfig struct {
	// BatchSize is the maximum number of records pulled per poll.
	BatchSize int
	// PollInterval is the wait between empty polls. Successful batches
	// loop immediately.
	PollInterval time.Duration
	// MaxAttempts caps the per-record retry count. After this many failed
	// attempts the record is marked terminal (StatusFailed).
	MaxAttempts int
	// BaseBackoff is the first retry delay; each further attempt doubles it,
	// capped at MaxBackoff. The Relay passes the computed delay to
	// Store.MarkFailed so a failing record is deferred instead of hammered on
	// every poll. Default: 10s.
	BaseBackoff time.Duration
	// MaxBackoff caps the exponential backoff. Default: 10m.
	MaxBackoff time.Duration
	// OnError is invoked for every publish failure (transient or terminal).
	OnError func(record OutboxRecord[any], err error)
}

// Relay drives the outbox: fetch pending, publish, mark dispatched.
type Relay[ID comparable] struct {
	store     Store[ID]
	publisher Publisher[ID]
	cfg       RelayConfig
}

// NewRelay constructs a relay.
func NewRelay[ID comparable](store Store[ID], publisher Publisher[ID], cfg RelayConfig) *Relay[ID] {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 10
	}
	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = 10 * time.Second
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 10 * time.Minute
	}
	return &Relay[ID]{store: store, publisher: publisher, cfg: cfg}
}

// backoffFor computes the retry delay for a record that has just failed its
// nth attempt: BaseBackoff * 2^(attempts-1), capped at MaxBackoff.
func (r *Relay[ID]) backoffFor(attempts int) time.Duration {
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

// Run blocks until ctx is canceled, polling the store and publishing.
func (r *Relay[ID]) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := r.RunOnce(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			// More work likely available — loop immediately.
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.cfg.PollInterval):
		}
	}
}

// RunOnce performs a single drain pass and returns the number of records
// successfully dispatched. Useful for tests and for cron-driven relays.
func (r *Relay[ID]) RunOnce(ctx context.Context) (int, error) {
	records, err := r.store.FetchPending(ctx, r.cfg.BatchSize)
	if err != nil {
		return 0, err
	}
	dispatched := 0
	for _, rec := range records {
		if perr := r.publisher.Publish(ctx, rec.Envelope); perr != nil {
			attempts := rec.Attempts + 1
			terminal := attempts >= r.cfg.MaxAttempts
			_ = r.store.MarkFailed(ctx, rec.RecordID, perr, r.backoffFor(attempts), terminal)
			continue
		}
		if merr := r.store.MarkDispatched(ctx, rec.RecordID); merr != nil {
			return dispatched, merr
		}
		dispatched++
	}
	return dispatched, nil
}

// ----------------------------------------------------------------------------
// In-memory Store (tests, local dev)
// ----------------------------------------------------------------------------

// InMemoryStore is a goroutine-safe in-memory Store.
type InMemoryStore[ID comparable] struct {
	mu      sync.Mutex
	records map[string]*OutboxRecord[ID]
	// order preserves enqueue order so FetchPending returns oldest-first.
	order []string
	clock ddd.Clock
}

// NewInMemoryStore returns an empty store.
func NewInMemoryStore[ID comparable](clock ddd.Clock) *InMemoryStore[ID] {
	if clock == nil {
		clock = ddd.SystemClock{}
	}
	return &InMemoryStore[ID]{
		records: make(map[string]*OutboxRecord[ID]),
		clock:   clock,
	}
}

// Enqueue appends a pending record and returns its server-assigned ID.
func (s *InMemoryStore[ID]) Enqueue(_ context.Context, env ddd.EventEnvelope[ID]) (OutboxRecord[ID], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	rec := &OutboxRecord[ID]{
		RecordID:  uuid.NewString(),
		Envelope:  env,
		Status:    StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.records[rec.RecordID] = rec
	s.order = append(s.order, rec.RecordID)
	return *rec, nil
}

// FetchPending returns up to limit oldest pending records, oldest first.
func (s *InMemoryStore[ID]) FetchPending(_ context.Context, limit int) ([]OutboxRecord[ID], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRecord[ID], 0, limit)
	for _, id := range s.order {
		rec, ok := s.records[id]
		if !ok || rec.Status != StatusPending {
			continue
		}
		out = append(out, *rec)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// MarkDispatched promotes the record.
func (s *InMemoryStore[ID]) MarkDispatched(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return errors.New("outbox: record not found")
	}
	rec.Status = StatusDispatched
	rec.Attempts++
	rec.UpdatedAt = s.clock.Now()
	rec.LastError = ""
	return nil
}

// MarkFailed increments attempts; if terminal, status becomes Failed. The
// in-memory store has no wall-clock scheduling, so retryAfter is accepted for
// interface conformance but not enforced — a failed non-terminal record stays
// Pending and is re-fetched on the next poll. SQL stores persist retryAfter as
// next_attempt_at to honour the backoff.
func (s *InMemoryStore[ID]) MarkFailed(_ context.Context, id string, err error, _ time.Duration, terminal bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return errors.New("outbox: record not found")
	}
	rec.Attempts++
	rec.UpdatedAt = s.clock.Now()
	if err != nil {
		rec.LastError = err.Error()
	}
	if terminal {
		rec.Status = StatusFailed
	}
	return nil
}

// Snapshot returns a copy of every record. Intended for tests/debugging.
func (s *InMemoryStore[ID]) Snapshot() []OutboxRecord[ID] {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRecord[ID], 0, len(s.order))
	for _, id := range s.order {
		if r := s.records[id]; r != nil {
			out = append(out, *r)
		}
	}
	return out
}
