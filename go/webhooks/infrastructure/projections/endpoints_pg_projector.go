package projections

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
	"github.com/lalternative/packages/go/eda/pkg/pgprojector"

	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/infrastructure"
)

const (
	pgDurable     = "webhooks-pg-readmodel"
	pgConsumerKey = "webhooks-pg-readmodel"
)

type metadata struct {
	AggregateID string `json:"aggregateId"`
	EventID     string `json:"eventId"`
	Timestamp   any    `json:"timestamp"`
	TenantID    string `json:"tenantId"`
}

type baseEnv struct {
	Metadata metadata `json:"metadata"`
}

type createdPayload struct {
	baseEnv
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description"`
}

type updatedPayload struct {
	baseEnv
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description"`
	Disabled    bool     `json:"disabled"`
}

// NewEndpointsPGProjector builds the webhooks Postgres read-model projector on
// top of the shared eda pgprojector: the durable pull, ack/backoff, DLQ,
// reconnect AND the transactional idempotency (dedup-check + apply + dedup-mark
// in one tx) all come from the lib. Only the event→SQL mutation lives here.
func NewEndpointsPGProjector(pool *pgxpool.Pool) *pgprojector.Projector {
	return pgprojector.New(pool, pgprojector.Config{
		Name:     pgDurable,
		Subject:  infrastructure.AggregateSubjectFilter,
		Durable:  pgDurable,
		Consumer: pgConsumerKey,
		EventID:  eventIDFromEnvelope,
		Apply:    applyEndpointEvent,
	})
}

// eventIDFromEnvelope reads the event's unique id from the message body.
func eventIDFromEnvelope(m *nats.Msg) (string, error) {
	var env baseEnv
	if err := json.Unmarshal(m.Data, &env); err != nil {
		return "", fmt.Errorf("decode envelope: %w", err)
	}
	return env.Metadata.EventID, nil
}

// applyEndpointEvent runs the read-model mutation for one event on tx. Dedup and
// commit are handled by pgprojector; this only issues the upsert/update.
func applyEndpointEvent(ctx context.Context, tx pgprojector.Tx, m *nats.Msg) error {
	eventType := m.Header.Get("Event-Type")

	switch eventType {
	case events.EndpointCreatedType:
		var ev createdPayload
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return consumer.Permanent(err)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO webhook_endpoints (id, tenant_id, url, event_types, description, status, created_at, updated_at, last_event_id)
			VALUES ($1, $2, $3, $4, $5, 'active', NOW(), NOW(), $6)
			ON CONFLICT (id) DO NOTHING
		`, ev.Metadata.AggregateID, ev.Metadata.TenantID, ev.URL, ev.EventTypes, ev.Description, ev.Metadata.EventID)
		return err

	case events.EndpointUpdatedType:
		var ev updatedPayload
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			return consumer.Permanent(err)
		}
		status := "active"
		if ev.Disabled {
			status = "disabled"
		}
		_, err := tx.Exec(ctx, `
			UPDATE webhook_endpoints
			   SET url = $2, event_types = $3, description = $4, status = $5,
			       updated_at = NOW(), last_event_id = $6
			 WHERE id = $1
		`, ev.Metadata.AggregateID, ev.URL, ev.EventTypes, ev.Description, status, ev.Metadata.EventID)
		return err

	case events.EndpointDeletedType:
		var env baseEnv
		if err := json.Unmarshal(m.Data, &env); err != nil {
			return consumer.Permanent(err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE webhook_endpoints
			   SET status = 'deleted', updated_at = NOW(), last_event_id = $2
			 WHERE id = $1
		`, env.Metadata.AggregateID, env.Metadata.EventID)
		return err

	default:
		// Secret rotations and delivery events don't update this read model.
		return nil
	}
}
