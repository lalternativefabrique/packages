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
	// Envelope pulls the routing facts out of an upstream event. Optional:
	// nil reads the {metadata:{eventId,timestamp,tenantId}} shape from the body.
	//
	// It exists because event envelopes are a product's own convention, and a
	// publisher whose events are flat — or whose scope key is an application
	// rather than a tenant — would otherwise resolve to an empty scope and have
	// every one of its events dropped without a word.
	//
	// The headers are passed alongside the body because a publisher may carry
	// its routing facts there instead: an event store that keeps the body to
	// the payload alone leaves nothing in it to route on. Use HeaderEnvelope
	// for that layout.
	Envelope func(raw []byte, header nats.Header) (Envelope, error)
}

// Envelope is what the dispatcher needs to route one upstream event: who it
// belongs to, what identifies it, and when it happened.
type Envelope struct {
	// Scope is the key endpoints are looked up by — whatever the product stores
	// as an endpoint's TenantID. An empty Scope drops the event: it cannot be
	// delivered to anyone.
	Scope string
	// EventID identifies the upstream event. It is what makes delivery ids
	// deterministic, so a replay reuses one instead of fanning out a duplicate.
	// Empty is allowed but gives up that idempotency.
	EventID   string
	Timestamp time.Time
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

// MetadataEnvelope reads the {metadata:{eventId,timestamp,tenantId}} shape. It
// is the default when a Source declares no Envelope of its own.
func MetadataEnvelope(raw []byte, _ nats.Header) (Envelope, error) {
	var env struct {
		Metadata struct {
			EventID   string    `json:"eventId"`
			Timestamp time.Time `json:"timestamp"`
			TenantID  string    `json:"tenantId"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Scope:     env.Metadata.TenantID,
		EventID:   env.Metadata.EventID,
		Timestamp: env.Metadata.Timestamp,
	}, nil
}

// HeaderEnvelope reads the routing facts from the NATS headers rather than the
// body: Tenant-Id, Event-Id and Occurred-At. It suits publishers whose event
// body carries the payload alone, which is the layout go-eda's event store
// writes.
func HeaderEnvelope(_ []byte, header nats.Header) (Envelope, error) {
	env := Envelope{
		Scope:   header.Get("Tenant-Id"),
		EventID: header.Get("Event-Id"),
	}
	if occurred := header.Get("Occurred-At"); occurred != "" {
		ts, err := time.Parse(time.RFC3339Nano, occurred)
		if err != nil {
			return Envelope{}, fmt.Errorf("parse Occurred-At: %w", err)
		}
		env.Timestamp = ts
	}
	return env, nil
}

func (d *Dispatcher) handle(ctx context.Context, m *nats.Msg) error {
	publicType := d.source.PublicType(upstreamType(m))
	if publicType == "" {
		return nil // not interested
	}

	readEnvelope := d.source.Envelope
	if readEnvelope == nil {
		readEnvelope = MetadataEnvelope
	}
	env, err := readEnvelope(m.Data, m.Header)
	if err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if env.Scope == "" {
		return nil
	}

	endpoints, err := d.lookup.ActiveByTenant(ctx, env.Scope)
	if err != nil {
		return fmt.Errorf("lookup endpoints: %w", err)
	}
	if len(endpoints) == 0 {
		return nil
	}

	payload, err := buildPayload(publicType, env.EventID, env.Timestamp, m.Data)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}

	for _, ep := range endpoints {
		if !subscribed(ep.EventTypes, publicType) {
			continue
		}
		// Deterministic delivery id keeps publish idempotent across replays.
		deliveryID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(ep.ID+":"+env.EventID)).String()
		job := providers.DeliveryJob{
			DeliveryID:    deliveryID,
			EndpointID:    ep.ID,
			TenantID:      ep.TenantID,
			URL:           ep.URL,
			Secret:        ep.Secret,
			EventType:     publicType,
			SourceEventID: env.EventID,
			Payload:       payload,
		}
		if err := d.outbox.Publish(ctx, job); err != nil {
			return fmt.Errorf("publish job: %w", err)
		}
	}
	return nil
}

// upstreamType names the event PublicType is asked about: the Event-Type header
// when the publisher sets one, the NATS subject otherwise.
//
// The fallback is not a convenience. A publisher that emits without headers —
// several do, since the subject already carries the type — would otherwise hand
// PublicType an empty string, and every one of its events would be dropped in
// silence. Falling back on the subject makes the mapping work against what is
// always present.
func upstreamType(m *nats.Msg) string {
	if t := m.Header.Get("Event-Type"); t != "" {
		return t
	}
	return m.Subject
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
