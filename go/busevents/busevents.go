// Package busevents is the contract of the suite's shared bus: the subjects
// each product publishes under events.<product>.> and the payload of each.
// Producers and consumers import the same types, so a renamed field breaks a
// build instead of a dashboard.
//
// Every payload is flat JSON carrying event_id and occurred_at at the top
// level: go/eda's consumer reads event_id from the body for idempotency, and
// an envelope would hide it. Payloads carry no e-mail and no name, since every
// consumer of the bus can read them.
package busevents

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Meta is what every event carries. EventID is also the Nats-Msg-Id header of
// the publish, so a redelivery or a republish of one fact is deduplicated.
type Meta struct {
	EventID    string    `json:"event_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

var ErrInvalid = errors.New("invalid bus event")

func (m Meta) validate() error {
	if m.EventID == "" {
		return fmt.Errorf("%w: no event_id", ErrInvalid)
	}
	if m.OccurredAt.IsZero() {
		return fmt.Errorf("%w: no occurred_at", ErrInvalid)
	}
	return nil
}

// Event is any payload of this package.
type Event interface {
	Validate() error
	ID() string
}

func (m Meta) ID() string { return m.EventID }

// Subject is events.<product>.<name>. The product is the NATS account the
// message is published from, which is what makes the prefix trustworthy.
func Subject(product, name string) string {
	return "events." + product + "." + name
}

// ProductOf is the <product> of a bus subject, "" when it is not one.
func ProductOf(subject string) string {
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 3 || parts[0] != "events" {
		return ""
	}
	return parts[1]
}

// NameOf is what follows events.<product>. in a subject.
func NameOf(subject string) string {
	parts := strings.SplitN(subject, ".", 3)
	if len(parts) < 3 || parts[0] != "events" {
		return ""
	}
	return parts[2]
}
