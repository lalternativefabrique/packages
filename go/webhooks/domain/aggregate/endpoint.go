package aggregate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lalternative/packages/go/webhooks/domain/events"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
	StatusDeleted  Status = "deleted"
)

const SecretPrefix = "whsec_"

// Endpoint is a tenant-owned HTTP subscription. Aggregate id is a UUID.
type Endpoint struct {
	ID          string
	TenantID    string
	URL         string
	Secret      string
	EventTypes  []string
	Description string
	Status      Status
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time

	uncommitted []events.EventWithMetadata
}

func NewEndpoint(id string) *Endpoint {
	return &Endpoint{ID: id}
}

// Create initializes a new endpoint. Generates a fresh HMAC secret.
func (e *Endpoint) Create(tenantID, rawURL string, eventTypes []string, description string, catalog events.Catalog) error {
	if e.Version != 0 {
		return errors.New("endpoint already exists")
	}
	if tenantID == "" {
		return errors.New("tenant_id is required")
	}
	if err := validateURL(rawURL); err != nil {
		return err
	}
	types, err := normalizeEventTypes(eventTypes, catalog)
	if err != nil {
		return err
	}
	secret, err := generateSecret()
	if err != nil {
		return fmt.Errorf("generate secret: %w", err)
	}

	now := time.Now().UTC()
	e.apply(&events.EndpointCreatedEvent{
		URL:         rawURL,
		Secret:      secret,
		EventTypes:  types,
		Description: description,
	}, tenantID, now)
	return nil
}

// Update changes URL / filter / description / disabled. Idempotent.
func (e *Endpoint) Update(rawURL string, eventTypes []string, description string, disabled bool, catalog events.Catalog) error {
	if e.Version == 0 {
		return errors.New("endpoint does not exist")
	}
	if e.Status == StatusDeleted {
		return errors.New("endpoint is deleted")
	}
	if err := validateURL(rawURL); err != nil {
		return err
	}
	types, err := normalizeEventTypes(eventTypes, catalog)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	e.apply(&events.EndpointUpdatedEvent{
		URL:         rawURL,
		EventTypes:  types,
		Description: description,
		Disabled:    disabled,
	}, e.TenantID, now)
	return nil
}

// Delete marks the endpoint as deleted. Idempotent.
func (e *Endpoint) Delete() error {
	if e.Version == 0 {
		return errors.New("endpoint does not exist")
	}
	if e.Status == StatusDeleted {
		return nil
	}
	now := time.Now().UTC()
	e.apply(&events.EndpointDeletedEvent{}, e.TenantID, now)
	return nil
}

// RecordDeliveryAttempt appends a delivery-attempted event for observability.
// Outcome / status / error reflect a single HTTP attempt against the URL.
func (e *Endpoint) RecordDeliveryAttempt(deliveryID, eventType, sourceEventID string, attempt int, statusCode int, outcome DeliveryOutcome, errMsg string, durationMS int64) {
	now := time.Now().UTC()
	e.apply(&events.DeliveryAttemptedEvent{
		DeliveryID:    deliveryID,
		EventType:     eventType,
		SourceEventID: sourceEventID,
		Attempt:       attempt,
		StatusCode:    statusCode,
		Outcome:       string(outcome),
		Error:         errMsg,
		DurationMS:    durationMS,
	}, e.TenantID, now)
}

// MarkDeliverySucceeded appends a delivery-succeeded terminal event.
func (e *Endpoint) MarkDeliverySucceeded(deliveryID, eventType, sourceEventID string, attempts int) {
	now := time.Now().UTC()
	e.apply(&events.DeliverySucceededEvent{
		DeliveryID:    deliveryID,
		EventType:     eventType,
		SourceEventID: sourceEventID,
		Attempts:      attempts,
	}, e.TenantID, now)
}

// MarkDeliveryFailed appends a delivery-failed terminal event.
func (e *Endpoint) MarkDeliveryFailed(deliveryID, eventType, sourceEventID string, attempts int, reason string) {
	now := time.Now().UTC()
	e.apply(&events.DeliveryFailedEvent{
		DeliveryID:    deliveryID,
		EventType:     eventType,
		SourceEventID: sourceEventID,
		Attempts:      attempts,
		Reason:        reason,
	}, e.TenantID, now)
}

// RotateSecret issues a fresh HMAC secret. Returns the new raw secret.
func (e *Endpoint) RotateSecret() (string, error) {
	if e.Version == 0 {
		return "", errors.New("endpoint does not exist")
	}
	if e.Status == StatusDeleted {
		return "", errors.New("endpoint is deleted")
	}
	secret, err := generateSecret()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	e.apply(&events.EndpointSecretRotatedEvent{Secret: secret}, e.TenantID, now)
	return secret, nil
}

func (e *Endpoint) UncommittedEvents() []events.EventWithMetadata { return e.uncommitted }
func (e *Endpoint) MarkCommitted()                                { e.uncommitted = nil }

func (e *Endpoint) Replay(history []events.EventWithMetadata) {
	for _, ev := range history {
		e.applyState(ev)
		e.Version = ev.GetMetadata().AggregateVersion
	}
}

func (e *Endpoint) apply(ev events.EventWithMetadata, tenantID string, ts time.Time) {
	e.Version++
	ev.SetMetadata(events.EventMetadata{
		AggregateID:      e.ID,
		AggregateVersion: e.Version,
		EventID:          uuid.New().String(),
		EventType:        eventType(ev),
		EventVersion:     1,
		Timestamp:        ts,
		TenantID:         tenantID,
	})
	e.applyState(ev)
	e.uncommitted = append(e.uncommitted, ev)
}

func (e *Endpoint) applyState(ev events.EventWithMetadata) {
	switch x := ev.(type) {
	case *events.EndpointCreatedEvent:
		e.TenantID = ev.GetMetadata().TenantID
		e.URL = x.URL
		e.Secret = x.Secret
		e.EventTypes = append([]string(nil), x.EventTypes...)
		e.Description = x.Description
		e.Status = StatusActive
		if e.CreatedAt.IsZero() {
			e.CreatedAt = ev.GetMetadata().Timestamp
		}
		e.UpdatedAt = ev.GetMetadata().Timestamp
	case *events.EndpointUpdatedEvent:
		e.URL = x.URL
		e.EventTypes = append([]string(nil), x.EventTypes...)
		e.Description = x.Description
		if x.Disabled {
			e.Status = StatusDisabled
		} else {
			e.Status = StatusActive
		}
		e.UpdatedAt = ev.GetMetadata().Timestamp
	case *events.EndpointDeletedEvent:
		e.Status = StatusDeleted
		e.UpdatedAt = ev.GetMetadata().Timestamp
	case *events.EndpointSecretRotatedEvent:
		e.Secret = x.Secret
		e.UpdatedAt = ev.GetMetadata().Timestamp
	}
}

func eventType(ev events.EventWithMetadata) string {
	switch ev.(type) {
	case *events.EndpointCreatedEvent:
		return events.EndpointCreatedType
	case *events.EndpointUpdatedEvent:
		return events.EndpointUpdatedType
	case *events.EndpointDeletedEvent:
		return events.EndpointDeletedType
	case *events.EndpointSecretRotatedEvent:
		return events.EndpointSecretRotatedType
	}
	panic(fmt.Sprintf("unknown event type %T", ev))
}

func validateURL(raw string) error {
	if raw == "" {
		return errors.New("url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("url must be http or https")
	}
	if u.Host == "" {
		return errors.New("url must have a host")
	}
	return nil
}

// normalizeEventTypes deduplicates, lowercases, sorts and validates event
// types against the allowed list.
func normalizeEventTypes(types []string, catalog events.Catalog) ([]string, error) {
	if len(types) == 0 {
		return nil, errors.New("at least one event_type is required")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(types))
	for _, t := range types {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if !catalog.Allows(t) {
			return nil, fmt.Errorf("unknown event type %q", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one event_type is required")
	}
	sort.Strings(out)
	return out, nil
}

func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return SecretPrefix + hex.EncodeToString(buf), nil
}
