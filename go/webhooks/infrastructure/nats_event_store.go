package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

const (
	// StreamName backs the endpoint event log. Subjects follow the
	// aggregate.id.type layout used across the platform (same shape as synthiz):
	// webhook.<endpoint-id>.<event-type>. The read-model projectors bind to
	// webhook.>.
	StreamName = "WEBHOOK_EVENTS"
	subjectPat = "webhook.>"
)

// AggregateSubjectFilter is the subject the read-model projectors consume.
const AggregateSubjectFilter = "webhook.>"

// NATSEventStore persists Endpoint aggregates as an append-only JetStream event
// log, on the v2 jetstream API. It follows the reference pattern (synthiz): one
// subject per event (webhook.<id>.<type>), a n: bare publish with Nats-Msg-Id
// for dedup, and replay-on-load. No optimistic concurrency — an aggregate is
// rarely written concurrently and the consumers are idempotent.
type NATSEventStore struct {
	js     jetstream.JetStream
	stream jetstream.Stream
}

func NewNATSEventStore(ctx context.Context, nc *nats.Conn) (*NATSEventStore, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	stream, err := js.Stream(ctx, StreamName)
	if err != nil {
		stream, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name:        StreamName,
			Description: "event log for webhook endpoints",
			Subjects:    []string{subjectPat},
			Storage:     jetstream.FileStorage,
			Retention:   jetstream.LimitsPolicy,
			Duplicates:  2 * time.Minute, // dedupe window for Nats-Msg-Id
		})
		if err != nil {
			return nil, fmt.Errorf("create stream %s: %w", StreamName, err)
		}
	}
	return &NATSEventStore{js: js, stream: stream}, nil
}

func subjectFor(aggregateID, eventType string) string {
	return fmt.Sprintf("webhook.%s.%s", aggregateID, eventType)
}

// Save appends the aggregate's uncommitted events, one bare publish each, then
// marks committed. Nats-Msg-Id (the event id) dedupes a retried publish within
// the stream's duplicate window.
func (s *NATSEventStore) Save(ctx context.Context, e *aggregate.Endpoint) error {
	uncommitted := e.UncommittedEvents()
	if len(uncommitted) == 0 {
		return nil
	}

	for _, ev := range uncommitted {
		meta := ev.GetMetadata()
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		msg := &nats.Msg{
			Subject: subjectFor(e.ID, meta.EventType),
			Data:    data,
			Header:  nats.Header{},
		}
		msg.Header.Set("Event-Type", meta.EventType)
		msg.Header.Set("Aggregate-ID", e.ID)
		msg.Header.Set("Aggregate-Version", strconv.Itoa(meta.AggregateVersion))
		msg.Header.Set("Tenant-ID", meta.TenantID)

		if _, err := s.js.PublishMsg(ctx, msg, jetstream.WithMsgID(meta.EventID)); err != nil {
			return fmt.Errorf("publish event: %w", err)
		}
	}

	e.MarkCommitted()
	return nil
}

// Load reconstructs an Endpoint by replaying its event history. A missing
// aggregate yields an empty endpoint (Version 0), matching the previous contract
// where callers check Version == 0.
func (s *NATSEventStore) Load(ctx context.Context, id string) (*aggregate.Endpoint, error) {
	filter := fmt.Sprintf("webhook.%s.>", id)
	cons, err := s.js.OrderedConsumer(ctx, StreamName, jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{filter},
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("ordered consumer: %w", err)
	}

	e := aggregate.NewEndpoint(id)
	var history []events.EventWithMetadata

	for {
		batch, err := cons.Fetch(100, jetstream.FetchMaxWait(200*time.Millisecond))
		if err != nil {
			return nil, fmt.Errorf("fetch: %w", err)
		}
		gotAny := false
		for msg := range batch.Messages() {
			gotAny = true
			ev, err := decode(msg.Headers().Get("Event-Type"), msg.Data())
			if err != nil {
				return nil, err
			}
			history = append(history, ev)
		}
		if batch.Error() != nil {
			return nil, batch.Error()
		}
		if !gotAny {
			break
		}
	}

	if len(history) == 0 {
		return e, nil // empty aggregate; caller checks Version == 0
	}
	e.Replay(history)
	return e, nil
}

var _ repository.EndpointRepository = (*NATSEventStore)(nil)

func decode(eventType string, data []byte) (events.EventWithMetadata, error) {
	var ev events.EventWithMetadata
	switch eventType {
	case events.EndpointCreatedType:
		ev = &events.EndpointCreatedEvent{}
	case events.EndpointUpdatedType:
		ev = &events.EndpointUpdatedEvent{}
	case events.EndpointDeletedType:
		ev = &events.EndpointDeletedEvent{}
	case events.EndpointSecretRotatedType:
		ev = &events.EndpointSecretRotatedEvent{}
	case events.DeliveryAttemptedType:
		ev = &events.DeliveryAttemptedEvent{}
	case events.DeliverySucceededType:
		ev = &events.DeliverySucceededEvent{}
	case events.DeliveryFailedType:
		ev = &events.DeliveryFailedEvent{}
	default:
		return nil, fmt.Errorf("unknown event type: %s", eventType)
	}
	if err := json.Unmarshal(data, ev); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", eventType, err)
	}
	return ev, nil
}
