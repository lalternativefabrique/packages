// Package ddd: generic, type-safe building blocks for event-sourced
// aggregates. This file complements interfaces.go/base.go (kept for legacy
// callers) with a modern, generics-based API.
//
// The central type is EventEnvelope: a stable on-the-wire shape with the
// metadata an event store needs (aggregate identity, version, causation,
// occurrence time, tenant, etc.) plus a typed Payload.
package ddd

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// EventPayload is implemented by domain event payload types. The Kind
// returned by EventKind is the stable string used to route, serialize and
// replay the event across systems. Keep it stable for the lifetime of
// the event type.
type EventPayload interface {
	EventKind() string
}

// EventEnvelope wraps a domain event with the metadata required to store,
// route and replay it. The aggregate ID is generic so domains can pick
// their own identifier type (string, UUID, int64, custom type).
type EventEnvelope[ID comparable] struct {
	// EventID is the unique identifier of this envelope (idempotency key).
	EventID string `json:"event_id"`
	// EventType is the stable kind string. Mirrors Payload.EventKind() and
	// is duplicated here so deserialization does not require the payload.
	EventType string `json:"event_type"`

	// AggregateID is the identifier of the aggregate this event belongs to.
	AggregateID ID `json:"aggregate_id"`
	// AggregateType labels the aggregate kind (e.g. "Message", "Brand").
	AggregateType string `json:"aggregate_type"`
	// AggregateVersion is the version produced by this event (1-based,
	// strictly increasing per aggregate).
	AggregateVersion int `json:"aggregate_version"`

	// OccurredAt is the wall-clock time the event was produced.
	OccurredAt time.Time `json:"occurred_at"`

	// CorrelationID groups all envelopes related to a single user-initiated
	// action across services. CausationID is the EventID (or CommandID)
	// that directly caused this one.
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`

	// TenantID partitions data in multi-tenant deployments. Optional.
	TenantID string `json:"tenant_id,omitempty"`

	// Metadata carries non-domain headers (user_id, source, trace_id ...).
	Metadata map[string]string `json:"metadata,omitempty"`

	// Payload is the domain-specific body. Marshaled separately by the
	// event store so it can deserialize against EventType.
	Payload EventPayload `json:"payload"`
}

// EnvelopeOption mutates an EventEnvelope at construction time.
type EnvelopeOption[ID comparable] func(*EventEnvelope[ID])

// WithCorrelation sets correlation/causation IDs.
func WithCorrelation[ID comparable](correlation, causation string) EnvelopeOption[ID] {
	return func(e *EventEnvelope[ID]) {
		e.CorrelationID = correlation
		e.CausationID = causation
	}
}

// WithTenant sets the tenant identifier.
func WithTenant[ID comparable](tenant string) EnvelopeOption[ID] {
	return func(e *EventEnvelope[ID]) { e.TenantID = tenant }
}

// WithMetadata merges keys into the envelope metadata map.
func WithMetadata[ID comparable](kv map[string]string) EnvelopeOption[ID] {
	return func(e *EventEnvelope[ID]) {
		if e.Metadata == nil {
			e.Metadata = make(map[string]string, len(kv))
		}
		for k, v := range kv {
			e.Metadata[k] = v
		}
	}
}

// NewEnvelope constructs an envelope. The aggregate version is supplied by
// the caller — typically by the aggregate's Apply path.
func NewEnvelope[ID comparable](
	clock Clock,
	aggregateType string,
	aggregateID ID,
	version int,
	payload EventPayload,
	opts ...EnvelopeOption[ID],
) EventEnvelope[ID] {
	if clock == nil {
		clock = SystemClock{}
	}
	env := EventEnvelope[ID]{
		EventID:          uuid.NewString(),
		EventType:        payload.EventKind(),
		AggregateID:      aggregateID,
		AggregateType:    aggregateType,
		AggregateVersion: version,
		OccurredAt:       clock.Now(),
		Payload:          payload,
	}
	for _, opt := range opts {
		opt(&env)
	}
	return env
}

// ----------------------------------------------------------------------------
// Aggregate
// ----------------------------------------------------------------------------

// ErrUnknownEvent is returned by Apply when an envelope's EventType is not
// recognized by the aggregate.
var ErrUnknownEvent = errors.New("ddd: unknown event type for aggregate")

// AggregateRoot is the generic interface for event-sourced aggregates.
// Implementations should embed *BaseAggregateRoot[ID] which provides the
// uncommitted-events bookkeeping.
type AggregateRoot[ID comparable] interface {
	// ID returns the aggregate identifier.
	ID() ID
	// AggregateType is the stable kind label (e.g. "Message", "Brand").
	AggregateType() string
	// Version is the version of the last applied event (0 for a fresh
	// aggregate without history).
	Version() int
	// Uncommitted returns events produced since the last MarkCommitted().
	Uncommitted() []EventEnvelope[ID]
	// MarkCommitted clears the uncommitted list. Called by the event store
	// after a successful save.
	MarkCommitted()
	// Apply mutates state from an envelope. Used during replay AND when
	// the aggregate emits a new event via Raise.
	Apply(env EventEnvelope[ID]) error
}

// BaseAggregateRoot is the embedding helper used by concrete aggregates.
// Concrete types provide their own Apply method (typically a type switch
// over Payload) and call Raise to emit new events.
type BaseAggregateRoot[ID comparable] struct {
	id            ID
	aggregateType string
	version       int
	uncommitted   []EventEnvelope[ID]
	clock         Clock
}

// Init wires the base. Must be called by the concrete aggregate's
// constructor before any Raise or LoadFromHistory.
func (b *BaseAggregateRoot[ID]) Init(id ID, aggregateType string, clock Clock) {
	if clock == nil {
		clock = SystemClock{}
	}
	b.id = id
	b.aggregateType = aggregateType
	b.clock = clock
}

func (b *BaseAggregateRoot[ID]) ID() ID                          { return b.id }
func (b *BaseAggregateRoot[ID]) AggregateType() string           { return b.aggregateType }
func (b *BaseAggregateRoot[ID]) Version() int                    { return b.version }
func (b *BaseAggregateRoot[ID]) Uncommitted() []EventEnvelope[ID] { return b.uncommitted }
func (b *BaseAggregateRoot[ID]) MarkCommitted()                  { b.uncommitted = nil }

// Clock exposes the aggregate's clock so it can be passed to NewEnvelope.
func (b *BaseAggregateRoot[ID]) Clock() Clock { return b.clock }

// Raise builds an envelope for the given payload (using the next version)
// and applies it through the supplied apply func. The envelope is appended
// to the uncommitted list. Concrete aggregates typically expose this as a
// helper inside their command methods.
//
// applyFn must mutate aggregate state from the payload. It must not append
// to uncommitted itself; Raise does that.
func Raise[ID comparable, A AggregateRoot[ID]](
	a A,
	base *BaseAggregateRoot[ID],
	payload EventPayload,
	applyFn func(EventEnvelope[ID]) error,
	opts ...EnvelopeOption[ID],
) error {
	nextVersion := base.version + 1
	env := NewEnvelope[ID](base.clock, base.aggregateType, base.id, nextVersion, payload, opts...)
	if err := applyFn(env); err != nil {
		return err
	}
	base.version = nextVersion
	base.uncommitted = append(base.uncommitted, env)
	return nil
}

// LoadFromHistory replays envelopes through the aggregate's Apply method.
// Use it inside repositories after loading from the event store.
func LoadFromHistory[ID comparable, A AggregateRoot[ID]](a A, base *BaseAggregateRoot[ID], history []EventEnvelope[ID]) error {
	for _, env := range history {
		if err := a.Apply(env); err != nil {
			return err
		}
		base.version = env.AggregateVersion
	}
	return nil
}
