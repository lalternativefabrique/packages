package aggregate

// DeliveryOutcome classifies a single HTTP attempt against a subscriber URL.
type DeliveryOutcome string

const (
	OutcomeOK        DeliveryOutcome = "ok"
	OutcomeTransient DeliveryOutcome = "transient"
	OutcomePermanent DeliveryOutcome = "permanent"
)
