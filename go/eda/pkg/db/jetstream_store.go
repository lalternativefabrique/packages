// Package db: generic event store backed by NATS JetStream (v2 API).
//
// This file provides JetStreamStore[ID], the modern, type-safe event store
// built on github.com/nats-io/nats.go/jetstream. It coexists with the
// legacy NATSEventStore (event_store.go) which uses the deprecated v1
// JetStreamContext.
//
// Design choices:
//
//   - Subject layout: <prefix>.<aggregateType>.<aggregateID>.<eventType>
//     Storing the event type in the subject makes per-type subscriptions
//     trivial and avoids parsing headers during routing.
//   - Optimistic concurrency: ExpectLastSequencePerSubject is set so two
//     writers cannot interleave on the same aggregate. The store reads the
//     last sequence per <prefix>.<aggregateType>.<aggregateID>.> on save.
//   - Idempotency: every publish carries Nats-Msg-Id = EventID, so a
//     replayed Save will be deduped by the server within the dedupe window.
//   - Replay: load uses an ephemeral ordered consumer that filters by
//     subject and stops cleanly via context cancellation / iterator close.
//   - Snapshots: companion SnapshotStore interface with an in-memory and
//     a KV-backed implementation (kv.go), so aggregates with long histories
//     can short-circuit replay.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Errors returned by the typed event store.
var (
	ErrConcurrencyConflict = errors.New("eventstore: concurrency conflict")
	ErrAggregateNotFound   = errors.New("eventstore: aggregate not found")
	ErrUnknownEventType    = errors.New("eventstore: unknown event type")
)

// PayloadFactory builds a fresh payload value to JSON-unmarshal into.
// One factory must be registered per EventKind via PayloadRegistry.
type PayloadFactory func() ddd.EventPayload

// PayloadRegistry maps EventKind strings to constructors. It is used by
// the store to deserialize payloads when loading.
type PayloadRegistry struct {
	byKind map[string]PayloadFactory
}

// NewPayloadRegistry returns an empty registry.
func NewPayloadRegistry() *PayloadRegistry {
	return &PayloadRegistry{byKind: make(map[string]PayloadFactory)}
}

// Register binds an EventKind to its constructor. Panics on duplicate kind
// so misconfiguration is caught at boot.
func (r *PayloadRegistry) Register(kind string, factory PayloadFactory) {
	if _, dup := r.byKind[kind]; dup {
		panic(fmt.Sprintf("eventstore: duplicate event kind %q", kind))
	}
	r.byKind[kind] = factory
}

func (r *PayloadRegistry) build(kind string, data []byte) (ddd.EventPayload, error) {
	f, ok := r.byKind[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownEventType, kind)
	}
	p := f()
	if err := json.Unmarshal(data, p); err != nil {
		return nil, fmt.Errorf("eventstore: unmarshal %s: %w", kind, err)
	}
	return p, nil
}

// IDCodec converts aggregate IDs to/from their string representation as
// they appear in NATS subjects. For string IDs the identity codec is used.
type IDCodec[ID comparable] interface {
	Encode(ID) string
	Decode(string) (ID, error)
}

// StringIDCodec is the identity codec for string aggregate IDs.
type StringIDCodec struct{}

func (StringIDCodec) Encode(s string) string             { return s }
func (StringIDCodec) Decode(s string) (string, error)    { return s, nil }

// JetStreamStoreConfig configures a JetStreamStore.
type JetStreamStoreConfig[ID comparable] struct {
	// StreamName is the JetStream stream backing the event log.
	StreamName string
	// SubjectPrefix is prepended to every event subject. Example: "events".
	// Final subjects look like: events.<aggregateType>.<aggregateID>.<eventType>
	SubjectPrefix string
	// AggregateType this store serves. A store is bound to one aggregate
	// type to keep subject filters efficient and codec deterministic.
	AggregateType string
	// Payloads must contain factories for every event kind this aggregate
	// can produce.
	Payloads *PayloadRegistry
	// IDs encodes/decodes aggregate IDs. Defaults to StringIDCodec when ID
	// is string.
	IDs IDCodec[ID]
	// MaxAge bounds event retention. Zero means keep forever.
	MaxAge time.Duration
	// CreateStreamIfMissing controls whether NewJetStreamStore creates the
	// stream when absent. Set to false in production if stream lifecycle is
	// managed externally.
	CreateStreamIfMissing bool
}

// JetStreamStore is the generic, JetStream-backed event store.
type JetStreamStore[ID comparable] struct {
	js     jetstream.JetStream
	stream jetstream.Stream
	cfg    JetStreamStoreConfig[ID]
}

// NewJetStreamStore wires the store, optionally creating its stream.
func NewJetStreamStore[ID comparable](
	ctx context.Context,
	nc *nats.Conn,
	cfg JetStreamStoreConfig[ID],
) (*JetStreamStore[ID], error) {
	if cfg.StreamName == "" {
		return nil, errors.New("eventstore: StreamName required")
	}
	if cfg.SubjectPrefix == "" {
		return nil, errors.New("eventstore: SubjectPrefix required")
	}
	if cfg.AggregateType == "" {
		return nil, errors.New("eventstore: AggregateType required")
	}
	if cfg.Payloads == nil {
		return nil, errors.New("eventstore: PayloadRegistry required")
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("eventstore: jetstream new: %w", err)
	}

	subjectFilter := fmt.Sprintf("%s.%s.>", cfg.SubjectPrefix, cfg.AggregateType)

	stream, err := js.Stream(ctx, cfg.StreamName)
	if err != nil {
		if !cfg.CreateStreamIfMissing {
			return nil, fmt.Errorf("eventstore: open stream %s: %w", cfg.StreamName, err)
		}
		stream, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:        cfg.StreamName,
			Description: fmt.Sprintf("event log for %s", cfg.AggregateType),
			Subjects:    []string{subjectFilter},
			Storage:     jetstream.FileStorage,
			Retention:   jetstream.LimitsPolicy,
			MaxAge:      cfg.MaxAge,
			Duplicates:  2 * time.Minute, // dedupe window for Nats-Msg-Id
		})
		if err != nil {
			return nil, fmt.Errorf("eventstore: create stream %s: %w", cfg.StreamName, err)
		}
	}

	return &JetStreamStore[ID]{js: js, stream: stream, cfg: cfg}, nil
}

func (s *JetStreamStore[ID]) subjectFor(aggregateID ID, eventType string) string {
	return fmt.Sprintf("%s.%s.%s.%s",
		s.cfg.SubjectPrefix, s.cfg.AggregateType, s.cfg.IDs.Encode(aggregateID), eventType)
}

func (s *JetStreamStore[ID]) aggregateSubjectFilter(aggregateID ID) string {
	return fmt.Sprintf("%s.%s.%s.>",
		s.cfg.SubjectPrefix, s.cfg.AggregateType, s.cfg.IDs.Encode(aggregateID))
}

// Save persists envelopes for one aggregate. expectedVersion is the
// version the caller believes is currently stored; pass 0 for a fresh
// aggregate. A mismatch yields ErrConcurrencyConflict.
//
// Envelopes are expected to have AggregateVersion = expectedVersion+1,
// expectedVersion+2, ... contiguous.
func (s *JetStreamStore[ID]) Save(
	ctx context.Context,
	aggregateID ID,
	expectedVersion int,
	envelopes []ddd.EventEnvelope[ID],
) error {
	if len(envelopes) == 0 {
		return nil
	}

	// Read the last sequence on the aggregate subject filter to drive OCC.
	lastSeq, lastVersion, err := s.lastAggregateState(ctx, aggregateID)
	if err != nil {
		return err
	}
	if expectedVersion != lastVersion {
		return fmt.Errorf("%w: aggregate %v expected v%d, got v%d",
			ErrConcurrencyConflict, aggregateID, expectedVersion, lastVersion)
	}

	expectedSeq := lastSeq
	for _, env := range envelopes {
		payload, err := json.Marshal(env.Payload)
		if err != nil {
			return fmt.Errorf("eventstore: marshal payload: %w", err)
		}
		msg := &nats.Msg{
			Subject: s.subjectFor(aggregateID, env.EventType),
			Data:    payload,
			Header:  nats.Header{},
		}
		msg.Header.Set("Event-Id", env.EventID)
		msg.Header.Set("Event-Type", env.EventType)
		msg.Header.Set("Aggregate-Id", s.cfg.IDs.Encode(aggregateID))
		msg.Header.Set("Aggregate-Type", s.cfg.AggregateType)
		msg.Header.Set("Aggregate-Version", strconv.Itoa(env.AggregateVersion))
		msg.Header.Set("Occurred-At", env.OccurredAt.UTC().Format(time.RFC3339Nano))
		if env.TenantID != "" {
			msg.Header.Set("Tenant-Id", env.TenantID)
		}
		if env.CorrelationID != "" {
			msg.Header.Set("Correlation-Id", env.CorrelationID)
		}
		if env.CausationID != "" {
			msg.Header.Set("Causation-Id", env.CausationID)
		}
		for k, v := range env.Metadata {
			msg.Header.Set("Meta-"+k, v)
		}

		opts := []jetstream.PublishOpt{
			jetstream.WithMsgID(env.EventID), // idempotent within the dedupe window
		}
		// Last-seq-per-subject is computed across the aggregate filter,
		// not a single subject, so we use ExpectLastSequence (stream-wide
		// would be too strict). Per-subject is sufficient because all
		// aggregate events live under <prefix>.<type>.<id>.>.
		if expectedSeq > 0 {
			opts = append(opts, jetstream.WithExpectLastSequencePerSubject(expectedSeq))
		}

		ack, err := s.js.PublishMsg(ctx, msg, opts...)
		if err != nil {
			// Translate server-side OCC failure.
			if isWrongLastSeqErr(err) {
				return fmt.Errorf("%w: %s", ErrConcurrencyConflict, err.Error())
			}
			return fmt.Errorf("eventstore: publish %s: %w", env.EventType, err)
		}
		expectedSeq = ack.Sequence
	}

	return nil
}

// Load returns the full history of an aggregate, oldest first.
func (s *JetStreamStore[ID]) Load(ctx context.Context, aggregateID ID) ([]ddd.EventEnvelope[ID], error) {
	return s.LoadFromVersion(ctx, aggregateID, 0)
}

// LoadFromVersion returns envelopes with AggregateVersion > fromVersion.
func (s *JetStreamStore[ID]) LoadFromVersion(ctx context.Context, aggregateID ID, fromVersion int) ([]ddd.EventEnvelope[ID], error) {
	cons, err := s.js.OrderedConsumer(ctx, s.cfg.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{s.aggregateSubjectFilter(aggregateID)},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("eventstore: ordered consumer: %w", err)
	}

	// Determine total expected count by checking the last sequence so we
	// know when to stop fetching. If lastSeq is 0 the aggregate is unknown.
	_, lastVersion, err := s.lastAggregateState(ctx, aggregateID)
	if err != nil {
		return nil, err
	}
	if lastVersion == 0 {
		return nil, fmt.Errorf("%w: %v", ErrAggregateNotFound, aggregateID)
	}
	remaining := lastVersion - fromVersion
	if remaining <= 0 {
		return nil, nil
	}

	out := make([]ddd.EventEnvelope[ID], 0, remaining)

	for len(out) < remaining {
		batchSize := remaining - len(out)
		if batchSize > 500 {
			batchSize = 500
		}
		batch, err := cons.Fetch(batchSize, jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			return nil, fmt.Errorf("eventstore: fetch: %w", err)
		}
		gotAny := false
		for msg := range batch.Messages() {
			gotAny = true
			env, err := s.decode(msg.Headers(), msg.Data(), aggregateID)
			if err != nil {
				return nil, err
			}
			if env.AggregateVersion <= fromVersion {
				continue
			}
			out = append(out, env)
		}
		if batch.Error() != nil {
			return nil, batch.Error()
		}
		if !gotAny {
			break
		}
	}

	return out, nil
}

// Subscribe streams envelopes published after the call, invoking handler
// for each. Returns when ctx is canceled. The subscription uses an
// ephemeral ordered consumer.
func (s *JetStreamStore[ID]) Subscribe(
	ctx context.Context,
	handler func(context.Context, ddd.EventEnvelope[ID]) error,
) error {
	subject := fmt.Sprintf("%s.%s.>", s.cfg.SubjectPrefix, s.cfg.AggregateType)
	cons, err := s.js.OrderedConsumer(ctx, s.cfg.StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{subject},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
	})
	if err != nil {
		return fmt.Errorf("eventstore: subscribe consumer: %w", err)
	}

	iter, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("eventstore: subscribe messages: %w", err)
	}
	defer iter.Stop()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg, err := iter.Next()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("eventstore: subscribe next: %w", err)
		}
		// We don't know aggregateID without parsing headers.
		aggIDStr := msg.Headers().Get("Aggregate-Id")
		aggID, decErr := s.cfg.IDs.Decode(aggIDStr)
		if decErr != nil {
			return fmt.Errorf("eventstore: decode aggregate id %q: %w", aggIDStr, decErr)
		}
		env, err := s.decode(msg.Headers(), msg.Data(), aggID)
		if err != nil {
			return err
		}
		if err := handler(ctx, env); err != nil {
			return err
		}
	}
}

// decode rebuilds an envelope from the message headers and body.
func (s *JetStreamStore[ID]) decode(h nats.Header, data []byte, aggregateID ID) (ddd.EventEnvelope[ID], error) {
	eventType := h.Get("Event-Type")
	payload, err := s.cfg.Payloads.build(eventType, data)
	if err != nil {
		return ddd.EventEnvelope[ID]{}, err
	}
	versionStr := h.Get("Aggregate-Version")
	version, _ := strconv.Atoi(versionStr)
	occ, _ := time.Parse(time.RFC3339Nano, h.Get("Occurred-At"))

	env := ddd.EventEnvelope[ID]{
		EventID:          h.Get("Event-Id"),
		EventType:        eventType,
		AggregateID:      aggregateID,
		AggregateType:    h.Get("Aggregate-Type"),
		AggregateVersion: version,
		OccurredAt:       occ,
		CorrelationID:    h.Get("Correlation-Id"),
		CausationID:      h.Get("Causation-Id"),
		TenantID:         h.Get("Tenant-Id"),
		Payload:          payload,
	}
	// Replay user metadata (Meta-* headers).
	for k, vs := range h {
		if len(k) > 5 && k[:5] == "Meta-" && len(vs) > 0 {
			if env.Metadata == nil {
				env.Metadata = make(map[string]string)
			}
			env.Metadata[k[5:]] = vs[0]
		}
	}
	return env, nil
}

// lastAggregateState returns (last subject sequence, last version). Both 0
// if the aggregate has no events yet.
func (s *JetStreamStore[ID]) lastAggregateState(ctx context.Context, aggregateID ID) (uint64, int, error) {
	// We need the last message across the aggregate subject filter. The
	// JetStream API exposes GetLastMsgFor(subject) but only for an exact
	// subject. To find the last across all event types of one aggregate
	// we read stream info filtered by subjects-and-sequences.
	info, err := s.stream.Info(ctx, jetstream.WithSubjectFilter(s.aggregateSubjectFilter(aggregateID)))
	if err != nil {
		return 0, 0, fmt.Errorf("eventstore: stream info: %w", err)
	}
	if len(info.State.Subjects) == 0 {
		return 0, 0, nil
	}
	// Walk the per-subject counts to estimate the highest sequence: we
	// must inspect the last message of each subject and keep the max
	// sequence (and the version it carries).
	var maxSeq uint64
	var maxVersion int
	for subject := range info.State.Subjects {
		msg, err := s.stream.GetLastMsgForSubject(ctx, subject)
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				continue
			}
			return 0, 0, fmt.Errorf("eventstore: last msg %s: %w", subject, err)
		}
		if msg.Sequence > maxSeq {
			maxSeq = msg.Sequence
			v, _ := strconv.Atoi(msg.Header.Get("Aggregate-Version"))
			maxVersion = v
		}
	}
	return maxSeq, maxVersion, nil
}

// isWrongLastSeqErr matches the server-side "wrong last sequence" error
// returned when ExpectLastSequence(PerSubject) fails.
func isWrongLastSeqErr(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *jetstream.APIError
	if errors.As(err, &apiErr) {
		// 10071 = wrong last sequence; see NATS error codes.
		return apiErr.ErrorCode == 10071
	}
	return false
}
