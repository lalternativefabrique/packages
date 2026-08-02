package providers

import "context"

// DeliveryJob is the wire shape of a webhook delivery job pushed to the
// outbox by the dispatcher and consumed by the HTTP worker.
type DeliveryJob struct {
	DeliveryID    string // UUID, used as idempotency key on the outbox
	EndpointID    string
	TenantID      string
	URL           string
	Secret        string
	EventType     string // public type, e.g. "email.sent"
	SourceEventID string // upstream event id (NATS msg id)
	Payload       []byte // JSON body delivered to the subscriber
}

// OutboxPublisher pushes a delivery job onto the work-queue stream.
type OutboxPublisher interface {
	Publish(ctx context.Context, job DeliveryJob) error
}

// HTTPDispatcher performs a single HTTP POST against the subscriber URL.
// Implementation lives in infrastructure/. Returned status code is 0 on
// network errors. The duration covers the full request including body read.
type HTTPDispatcher interface {
	Dispatch(ctx context.Context, job DeliveryJob) HTTPResult
}

type HTTPResult struct {
	StatusCode int
	Err        error
	DurationMS int64
}
