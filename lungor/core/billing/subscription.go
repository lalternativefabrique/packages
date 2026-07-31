// Package billing holds the subscription domain: what a customer is entitled
// to, whether their money is healthy, and what a change of tier costs.
//
// # Why this exists
//
// It continues what lib/billingperiod started. That package states its own
// charter plainly: it is "Lungor's billing engine being PROVEN IN PRODUCTION
// USE on Techtuel […] meant to be carried back into Lungor, hardened". The same
// applies here — Techtuel is the proving ground, not the owner. Synthiz core
// needs these rules too, and Lungor is meant to take them back.
//
// billingperiod answers WHEN a period runs. This answers WHO is entitled inside
// it, WHETHER the money behind it is healthy, and WHAT a tier change costs.
//
// # The boundary, and how to keep it
//
// Everything here is PURE: no database, no HTTP, no payment provider. The rules
// take values and return values.
//
// That is not stylistic. These rules were previously scattered across three
// layers of transcript-api — Entitled lived in a Postgres file, the dunning
// state machine lived in an HTTP handler package, and the tier algebra lived in
// a service-local domain package. Two predicates that are documented in terms of
// each other could not see each other. Anything that needs a connection, a
// client or a request belongs in the caller's infrastructure, not here.
//
// The test is the judge: a rule in this package must be testable with no
// database, no network and no payment provider. If a test needs a Mollie mock,
// the rule was put on the wrong side of the line.
package billing

import "time"

// Subscription is the billing state of one owner: which tier they pay for,
// whether the money is current, and until when the period they already paid
// runs.
//
// It is the domain shape, deliberately NOT a database row and NOT a wire
// format: no struct tags, no provider types. The provider's own identifiers are
// carried opaquely (see ProviderCustomerID) so the same value can describe a
// subscription held at Mollie, at Lungor, or anywhere else.
type Subscription struct {
	OwnerID string
	// Status is the lifecycle state: "active", "past_due" or "canceled".
	//
	// Kept a string rather than an enum because it is persisted as one and
	// constrained by the database (the status CHECK in the subscriptions table).
	// Entitled and State are the only readers that interpret it, and both treat
	// an unknown value as not-entitled — a new status must never fail open.
	Status string
	// CurrentPeriodEnd is when the period already paid for runs out. It is the
	// single date that decides access: the month is owed whatever happens to the
	// subscription afterwards, so a cancellation does not revoke it.
	CurrentPeriodEnd time.Time
	// PeriodStart is the anchor the credit window is derived from — the start of
	// the current period. Zero when the subscription has never been paid
	// (checkout opened, no confirmation yet), which callers read as the
	// calendar-month fallback rather than as "no quota".
	PeriodStart time.Time
	// Plan is the tier this subscription pays for, by name. Empty on rows
	// written before the tier was recorded; callers resolve that to the smallest
	// purchasable tier rather than guessing upward.
	//
	// This is what grants the allowance — never PendingPlan.
	Plan string
	// PendingPlan is a SMALLER tier the customer scheduled, applied by the next
	// renewal. Empty means nothing is scheduled.
	//
	// It gates nothing: the customer keeps what they bought until the period
	// they paid for runs out. This is a promise about the NEXT period, consumed
	// by the renewal that applies it.
	PendingPlan string
	// PendingPlanEffectiveAt is when PendingPlan takes over — a copy of
	// CurrentPeriodEnd taken when the change was requested, so the customer can
	// be told the change has not happened yet.
	PendingPlanEffectiveAt time.Time
	// ProviderCustomerID and ProviderScheduleID identify the subscription at the
	// payment provider. They are opaque here on purpose: the domain must not
	// know whether the string behind them is a Mollie customer id or a Lungor
	// one.
	//
	// The fields they replace were spelled MollieCustomerID and
	// MollieSubscriptionID — a domain type naming one vendor, which is exactly
	// what made this code unusable outside Techtuel.
	ProviderCustomerID string
	ProviderScheduleID string
	// LastPaymentID is the provider's identifier for the most recent payment,
	// carried for idempotency and reconciliation. Opaque, like the ids above.
	LastPaymentID string
}

// HasSchedule reports whether the subscription still points at a live recurring
// schedule at the provider.
//
// A subscription with no schedule is not necessarily broken: it may have been
// deliberately cancelled. But it can never be moved to another tier — there is
// nothing to replace — which is why the tier-change rules check this first.
func (s Subscription) HasSchedule() bool {
	return s.ProviderCustomerID != "" && s.ProviderScheduleID != ""
}
