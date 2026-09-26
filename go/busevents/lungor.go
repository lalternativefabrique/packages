package busevents

import (
	"fmt"
	"time"
)

const Lungor = "lungor"

// Published by Lungor under events.lungor.; the product billed is AppSlug.
const (
	SubscriptionActivated = "subscription.activated"
	SubscriptionRenewed   = "subscription.renewed"
	SubscriptionPastDue   = "subscription.past_due"
	SubscriptionCanceled  = "subscription.canceled"
)

type Interval string

const (
	Monthly Interval = "month"
	Yearly  Interval = "year"
)

// Subscription is the payload of every events.lungor.subscription.* subject.
//
// Activated is a first payment, or a return after churn. Renewed is a
// recurring payment that went through. PastDue is one that failed. Canceled
// means access stops at EffectiveAt; it is not published for a plan change
// that replaces a subscription, nor for a checkout never paid.
type Subscription struct {
	Meta
	// AppSlug is the product billed, equal to the <product> of its own
	// subjects.
	AppSlug        string `json:"app_slug"`
	PersonID       string `json:"person_id"`
	SubscriptionID string `json:"subscription_id"`
	// AmountCents is the plan price per Interval, in minor units; 0 for a
	// granted plan.
	AmountCents      int64      `json:"amount_cents"`
	Currency         string     `json:"currency"`
	Interval         Interval   `json:"interval"`
	CurrentPeriodEnd *time.Time `json:"current_period_end,omitempty"`
	// EffectiveAt and CancelAtPeriodEnd are set on canceled only.
	EffectiveAt       *time.Time `json:"effective_at,omitempty"`
	CancelAtPeriodEnd bool       `json:"cancel_at_period_end,omitempty"`
}

func (e Subscription) Validate() error {
	if err := e.validate(); err != nil {
		return err
	}
	if e.AppSlug == "" {
		return fmt.Errorf("%w: no app_slug", ErrInvalid)
	}
	if e.SubscriptionID == "" {
		return fmt.Errorf("%w: no subscription_id", ErrInvalid)
	}
	if e.Interval != Monthly && e.Interval != Yearly {
		return fmt.Errorf("%w: interval %q", ErrInvalid, e.Interval)
	}
	return requirePerson(e.PersonID)
}

// MonthlyCents is the price brought back to a month, so monthly and yearly
// payers add up.
func (e Subscription) MonthlyCents() int64 {
	if e.Interval == Yearly {
		return e.AmountCents / 12
	}
	return e.AmountCents
}

// EndsAt is when a cancellation takes effect: EffectiveAt, else OccurredAt.
func (e Subscription) EndsAt() time.Time {
	if e.EffectiveAt != nil {
		return *e.EffectiveAt
	}
	return e.OccurredAt
}
