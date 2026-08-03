package events

import "time"

const (
	EndpointCreatedType       = "endpoint-created"
	EndpointUpdatedType       = "endpoint-updated"
	EndpointDeletedType       = "endpoint-deleted"
	EndpointSecretRotatedType = "endpoint-secret-rotated"
	DeliveryAttemptedType     = "delivery-attempted"
	DeliverySucceededType     = "delivery-succeeded"
	DeliveryFailedType        = "delivery-failed"
)

// Catalog is the set of public event types a subscriber may register for.
//
// It is DATA, not constants: the internal event-sourcing types above belong to
// this package because they are its own, but what a product publishes is the
// product's to declare — email.sent for a mail service, subscription.renewed
// for a billing one. Hardcoding one product's list here is what would make the
// package that product's.
//
// Validation stays mandatory. Accepting an unknown type would let a subscriber
// register for an event that is never emitted and wait forever, with nothing
// anywhere reporting the mistake — a silent failure is worse than a rejected
// request.
type Catalog []string

// Allows reports whether t is in the catalog.
func (c Catalog) Allows(t string) bool {
	for _, x := range c {
		if x == t {
			return true
		}
	}
	return false
}

type EventMetadata struct {
	AggregateID      string    `json:"aggregateId"`
	AggregateVersion int       `json:"aggregateVersion"`
	EventID          string    `json:"eventId"`
	EventType        string    `json:"eventType"`
	EventVersion     int       `json:"eventVersion"`
	Timestamp        time.Time `json:"timestamp"`
	TenantID         string    `json:"tenantId,omitempty"`
}

type EventWithMetadata interface {
	GetMetadata() EventMetadata
	SetMetadata(EventMetadata)
	GetAggregateID() string
	GetEventType() string
	GetEventVersion() int
	// EventKind is the stable event-kind string. It also makes every event
	// satisfy the shared eda ddd.EventPayload interface, so events can be stored
	// directly by the eda JetStream event store.
	EventKind() string
}

type BaseEvent struct {
	Metadata EventMetadata `json:"metadata"`
}

func (e *BaseEvent) GetMetadata() EventMetadata  { return e.Metadata }
func (e *BaseEvent) SetMetadata(m EventMetadata) { e.Metadata = m }
func (e *BaseEvent) GetAggregateID() string      { return e.Metadata.AggregateID }
func (e *BaseEvent) GetEventType() string        { return e.Metadata.EventType }
func (e *BaseEvent) GetEventVersion() int        { return e.Metadata.EventVersion }

// EndpointCreatedEvent — a tenant registered a new webhook endpoint.
type EndpointCreatedEvent struct {
	BaseEvent
	URL         string   `json:"url"`
	Secret      string   `json:"secret"` // raw secret; consumers project a hash
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description,omitempty"`
}

// EndpointUpdatedEvent — URL / event-type filter / description changed.
type EndpointUpdatedEvent struct {
	BaseEvent
	URL         string   `json:"url"`
	EventTypes  []string `json:"eventTypes"`
	Description string   `json:"description,omitempty"`
	Disabled    bool     `json:"disabled"`
}

// EndpointDeletedEvent — tenant removed the endpoint.
type EndpointDeletedEvent struct {
	BaseEvent
}

// EndpointSecretRotatedEvent — new HMAC secret issued for the endpoint.
type EndpointSecretRotatedEvent struct {
	BaseEvent
	Secret string `json:"secret"`
}

// DeliveryAttemptedEvent — one HTTP POST attempt against the subscriber URL.
type DeliveryAttemptedEvent struct {
	BaseEvent
	DeliveryID    string `json:"deliveryId"`
	EventType     string `json:"eventType"`     // public type, e.g. "email.sent"
	SourceEventID string `json:"sourceEventId"` // upstream event that triggered the delivery
	Attempt       int    `json:"attempt"`
	StatusCode    int    `json:"statusCode,omitempty"`
	Outcome       string `json:"outcome"` // "ok" | "transient" | "permanent"
	Error         string `json:"error,omitempty"`
	DurationMS    int64  `json:"durationMs"`
}

// DeliverySucceededEvent — delivery reached terminal success state.
type DeliverySucceededEvent struct {
	BaseEvent
	DeliveryID    string `json:"deliveryId"`
	EventType     string `json:"eventType"`
	SourceEventID string `json:"sourceEventId"`
	Attempts      int    `json:"attempts"`
}

// DeliveryFailedEvent — delivery exhausted retries or hit a permanent error.
type DeliveryFailedEvent struct {
	BaseEvent
	DeliveryID    string `json:"deliveryId"`
	EventType     string `json:"eventType"`
	SourceEventID string `json:"sourceEventId"`
	Attempts      int    `json:"attempts"`
	Reason        string `json:"reason"`
}
