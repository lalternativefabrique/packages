package events

// EventKind makes each concrete event satisfy the shared eda ddd.EventPayload
// interface (EventKind() string), so they can be stored/loaded directly by the
// eda JetStream event store without a wrapper. The kind mirrors the *Type
// constants and the subject/eventType used across the pipeline.

func (*EndpointCreatedEvent) EventKind() string       { return EndpointCreatedType }
func (*EndpointUpdatedEvent) EventKind() string       { return EndpointUpdatedType }
func (*EndpointDeletedEvent) EventKind() string       { return EndpointDeletedType }
func (*EndpointSecretRotatedEvent) EventKind() string { return EndpointSecretRotatedType }
func (*DeliveryAttemptedEvent) EventKind() string     { return DeliveryAttemptedType }
func (*DeliverySucceededEvent) EventKind() string     { return DeliverySucceededType }
func (*DeliveryFailedEvent) EventKind() string        { return DeliveryFailedType }
