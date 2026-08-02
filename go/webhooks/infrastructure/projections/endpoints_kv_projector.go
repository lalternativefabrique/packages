package projections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"

	"github.com/lalternative/packages/go/webhooks/domain/events"
	domainrepo "github.com/lalternative/packages/go/webhooks/domain/repository"
	"github.com/lalternative/packages/go/webhooks/infrastructure"
	infrarepo "github.com/lalternative/packages/go/webhooks/infrastructure/repository"
)

const kvDurable = "webhooks-kv-readmodel"

type kvCreatedPayload struct {
	baseEnv
	URL         string   `json:"url"`
	Secret      string   `json:"secret"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description"`
}

type kvUpdatedPayload struct {
	baseEnv
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description"`
	Disabled    bool     `json:"disabled"`
}

type kvSecretPayload struct {
	baseEnv
	Secret string `json:"secret"`
}

// EndpointsKVProjector consumes WEBHOOK_EVENTS and maintains the
// WEBHOOK_ENDPOINTS KV bucket — used by the dispatcher to fan out upstream
// events to subscribers.
//
// It is a consumer.EventHandler: the durable pull, ack/nak, staged backoff, DLQ,
// heartbeat and reconnect all come from pkg/consumer via consumer.Run. Only the
// event→KV mutation logic lives here.
type EndpointsKVProjector struct {
	kv *infrarepo.EndpointsKV
}

func NewEndpointsKVProjector(kv *infrarepo.EndpointsKV) *EndpointsKVProjector {
	return &EndpointsKVProjector{kv: kv}
}

// EventHandler contract ------------------------------------------------------

func (*EndpointsKVProjector) Name() string        { return kvDurable }
func (*EndpointsKVProjector) Subject() string     { return infrastructure.AggregateSubjectFilter }
func (*EndpointsKVProjector) DurableName() string { return kvDurable }
func (*EndpointsKVProjector) MaxDeliver() int     { return 5 }

// Handle applies one event to the KV read model. Put/Delete are idempotent, so
// a redelivery re-applies harmlessly.
func (p *EndpointsKVProjector) Handle(ctx context.Context, m *nats.Msg) error {
	eventType := m.Header.Get("Event-Type")

	switch eventType {
	case events.EndpointCreatedType:
		var ev kvCreatedPayload
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return consumer.Permanent(fmt.Errorf("decode created: %w", err))
		}
		return p.kv.PutEntry(ctx, ev.Metadata.AggregateID, ev.Metadata.TenantID, ev.URL, ev.Secret, ev.EventTypes, "active")

	case events.EndpointUpdatedType:
		var ev kvUpdatedPayload
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return consumer.Permanent(fmt.Errorf("decode updated: %w", err))
		}
		// Merge with the existing secret (updates don't carry it).
		prev, err := p.kv.Get(ctx, ev.Metadata.AggregateID)
		secret := ""
		tenant := ev.Metadata.TenantID
		if err == nil {
			secret = prev.Secret
			tenant = prev.TenantID
		} else if !errors.Is(err, domainrepo.ErrNotFound) {
			return err
		}
		status := "active"
		if ev.Disabled {
			status = "disabled"
		}
		return p.kv.PutEntry(ctx, ev.Metadata.AggregateID, tenant, ev.URL, secret, ev.EventTypes, status)

	case events.EndpointSecretRotatedType:
		var ev kvSecretPayload
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return consumer.Permanent(fmt.Errorf("decode secret: %w", err))
		}
		if err := p.kv.UpdateSecret(ctx, ev.Metadata.AggregateID, ev.Secret); err != nil {
			if errors.Is(err, domainrepo.ErrNotFound) {
				return nil // nothing to rotate yet; a later create will carry it
			}
			return err
		}
		return nil

	case events.EndpointDeletedType:
		return p.kv.Delete(ctx, m.Header.Get("Aggregate-ID"))
	}

	return nil
}

var _ consumer.EventHandler = (*EndpointsKVProjector)(nil)
