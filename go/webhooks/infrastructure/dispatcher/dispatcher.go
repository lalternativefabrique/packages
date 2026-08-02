package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
	"github.com/nats-io/nats.go"
)

const durableName = "webhooks-dispatcher"

// Source names the upstream event stream a product fans out to its
// subscribers, and translates its internal event types into the public ones.
//
// It is the ONLY product-specific piece of the dispatch pipeline: endpoints,
// signing, retries and delivery are identical everywhere, but what counts as a
// publishable event is a product decision. Spore publishes message-sent as
// email.sent; a billing service publishes subscription lifecycle changes.
type Source struct {
	// StreamName is the JetStream stream carrying the upstream events, and
	// SubjectFilter the subjects to pull from it (e.g. "message.>").
	StreamName    string
	SubjectFilter string
	// PublicType maps an upstream event type onto the public type subscribers
	// register for. Returning "" drops the event: most internal events are not
	// meant to leave the system, so silence is the safe default.
	PublicType func(upstream string) string
}

// Dispatcher consumes a Source's upstream events, maps them to public webhook
// event types and publishes one DeliveryJob per (endpoint, event) onto the
// webhook outbox. Each job is keyed by (sourceEventId, endpointId) so
// projector replays don't fan out duplicate deliveries.
type Dispatcher struct {
	js     nats.JetStreamContext
	lookup repository.EndpointActiveLookup
	outbox providers.OutboxPublisher
	source Source
}

func NewDispatcher(js nats.JetStreamContext, lookup repository.EndpointActiveLookup, outbox providers.OutboxPublisher, source Source) *Dispatcher {
	return &Dispatcher{js: js, lookup: lookup, outbox: outbox, source: source}
}

func (d *Dispatcher) Run(ctx context.Context) error {
	if d.source.StreamName == "" || d.source.SubjectFilter == "" || d.source.PublicType == nil {
		return fmt.Errorf("dispatcher: incomplete source (stream, subject filter and PublicType are required)")
	}
	sub, err := d.js.PullSubscribe(d.source.SubjectFilter, durableName,
		nats.BindStream(d.source.StreamName),
		nats.DeliverAll(),
		nats.AckExplicit(),
		nats.AckWait(30*time.Second),
	)
	if err != nil {
		return fmt.Errorf("subscribe %s: %w", d.source.StreamName, err)
	}
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()

	for {
		if ctx.Err() != nil {
			return nil
		}
		msgs, err := sub.Fetch(50, nats.MaxWait(2*time.Second))
		if err != nil {
			if errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.Canceled) {
				continue
			}
			log.Printf("webhooks dispatcher: fetch: %v", err)
			time.Sleep(time.Second)
			continue
		}
		for _, m := range msgs {
			if err := d.handle(ctx, m); err != nil {
				log.Printf("webhooks dispatcher: %s: %v", m.Header.Get("Event-Type"), err)
				_ = m.Nak()
				continue
			}
			_ = m.Ack()
		}
	}
}

// upstreamMeta is the minimal envelope shared by all upstream events.
type upstreamMeta struct {
	Metadata struct {
		EventID   string    `json:"eventId"`
		Timestamp time.Time `json:"timestamp"`
		TenantID  string    `json:"tenantId"`
	} `json:"metadata"`
}

func (d *Dispatcher) handle(ctx context.Context, m *nats.Msg) error {
	upstreamType := m.Header.Get("Event-Type")
	publicType := d.source.PublicType(upstreamType)
	if publicType == "" {
		return nil // not interested
	}

	var env upstreamMeta
	if err := json.Unmarshal(m.Data, &env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if env.Metadata.TenantID == "" {
		return nil
	}

	endpoints, err := d.lookup.ActiveByTenant(ctx, env.Metadata.TenantID)
	if err != nil {
		return fmt.Errorf("lookup endpoints: %w", err)
	}
	if len(endpoints) == 0 {
		return nil
	}

	payload, err := buildPayload(publicType, env.Metadata.EventID, env.Metadata.Timestamp, m.Data)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	for _, ep := range endpoints {
		if !subscribed(ep.EventTypes, publicType) {
			continue
		}
		// Deterministic delivery id keeps publish idempotent across replays.
		deliveryID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(ep.ID+":"+env.Metadata.EventID)).String()
		job := providers.DeliveryJob{
			DeliveryID:    deliveryID,
			EndpointID:    ep.ID,
			TenantID:      ep.TenantID,
			URL:           ep.URL,
			Secret:        ep.Secret,
			EventType:     publicType,
			SourceEventID: env.Metadata.EventID,
			Payload:       payload,
		}
		if err := d.outbox.Publish(ctx, job); err != nil {
			return fmt.Errorf("publish job: %w", err)
		}
	}
	return nil
}

func subscribed(types []string, t string) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

// buildPayload composes the JSON body delivered to the subscriber.
//
// The shape is intentionally simple: { type, id, createdAt, data }. `data`
// embeds the raw upstream event JSON so subscribers can read every field
// without us hand-mapping each new attribute. Versioning is carried by the
// upstream event metadata (`eventVersion`) when subscribers care.
func buildPayload(publicType, sourceEventID string, ts time.Time, raw []byte) ([]byte, error) {
	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"type":      publicType,
		"id":        sourceEventID,
		"createdAt": ts.UTC().Format(time.RFC3339Nano),
		"data":      data,
	})
}
