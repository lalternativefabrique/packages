package billing

import "time"

// Entitled reports whether the owner may be served at time t.
//
// The rule is one line of policy: the period already paid for is owed, whatever
// happens to the subscription afterwards. So "canceled" still entitles until
// CurrentPeriodEnd — a customer who cancels on the 3rd keeps the month they
// bought — and "past_due" entitles never, because the money did not arrive.
//
// Unknown statuses are refused. A status the domain does not recognise must
// fail CLOSED: a new provider state that silently granted access would be
// invisible until someone noticed the revenue gap.
//
// Ported from transcript-api's infra.Entitled, where it sat in the Postgres
// file (`infra/pgsubscriptions.go`) purely because that is where the row was
// read. It touches no database, and it is the single most important predicate
// in billing: every quota gate resolves through it.
func Entitled(s Subscription, t time.Time) bool {
	switch s.Status {
	case StatusActive, StatusCanceled:
		return s.CurrentPeriodEnd.After(t)
	default:
		return false
	}
}

// Subscription lifecycle statuses, as persisted.
//
// Declared as constants so the two predicates that interpret them (Entitled and
// State) cannot drift apart on a typo — they previously lived in different
// packages and compared bare string literals.
const (
	// StatusActive — the subscription is paid and current.
	StatusActive = "active"
	// StatusPastDue — a payment did not go through. Access depends on whether
	// the period already paid has run out; see State.
	StatusPastDue = "past_due"
	// StatusCanceled — the customer stopped future charges. Still entitling
	// until the paid period lapses: cancelling is not a refund.
	StatusCanceled = "canceled"
)

// CanChangeTier reports whether the subscription is in a state that may move to
// another tier at time t.
//
// Two conditions, and both are needed:
//
//   - the status is active — an unpaid subscription owes money on the tier it
//     already has, and a cancelled one asked to leave. Neither should be
//     re-priced;
//   - it still entitles — a subscription whose paid period has lapsed has no
//     renewal left for a change to ride on.
//
// This is policy, not a convenience: it decides who may be re-charged. It was
// previously spelled inline as `sub.Status != "active" || !Entitled(...)` at
// each of the adapter's tier-change paths, which is two chances to disagree and
// no place to test it.
func CanChangeTier(s Subscription, t time.Time) bool {
	return s.Status == StatusActive && Entitled(s, t)
}

// State is what the customer is told about the health of their subscription,
// as opposed to whether a given request may proceed.
type State string

const (
	// StateHealthy — nothing to say. Also the answer for someone with no
	// subscription at all: never having subscribed is not a payment problem.
	StateHealthy State = "healthy"
	// StatePaymentFailed — the money did not arrive AND access is still granted.
	// This is the window in which the customer can fix it without losing
	// anything, which is why the deadline is reported alongside it.
	StatePaymentFailed State = "payment_failed"
	// StateSuspended — access has stopped because the payment was never
	// recovered. No deadline accompanies it: there is no future date left to
	// promise.
	StateSuspended State = "suspended"
)

// StateFor derives the customer-facing state from a subscription at time t.
// hasSubscription is false when the owner has no row at all.
//
// It answers a DIFFERENT question from Entitled, and the two must not be
// conflated. Entitled answers "may this request proceed". This answers "is
// there a payment problem, and can the customer still act on it". They agree on
// the cases that matter — a healthy state never covers for a closed gate — but
// "healthy" here means nothing is wrong with the money, not that access is
// granted.
//
// endsAt is the deadline the customer is still acting inside, and zero once
// access has already stopped.
//
// Ported from transcript-api's api.billingStateFor, which lived in the HTTP
// handler package. The two predicates were documented in terms of each other
// while sitting in packages that could not import each other; they are now
// neighbours, which is the point of this package.
func StateFor(s Subscription, hasSubscription bool, t time.Time) (state State, endsAt time.Time) {
	if !hasSubscription {
		// A free tenant is not in trouble; they simply never subscribed.
		return StateHealthy, time.Time{}
	}
	switch s.Status {
	case StatusActive, StatusCanceled:
		// Entitled's rule, restated: the paid period is owed whatever happens
		// next. A cancellation is a deliberate choice, not a failure — it is
		// reported through the status, and is not a payment problem.
		if s.CurrentPeriodEnd.After(t) {
			return StateHealthy, time.Time{}
		}
		// The period lapsed with no renewal payment: access is gone.
		return StateSuspended, time.Time{}
	default:
		// past_due — the money did not arrive. Two very different situations
		// share this status, and the period end separates them: still inside it,
		// the customer keeps access and has a window to fix the card; past it,
		// access has already stopped.
		//
		// This is also the status a checkout starts in (the pending row carries a
		// period end in the past), so someone who opened a payment page and
		// walked away lands in the suspended branch. That is the honest answer —
		// they have no access.
		if s.CurrentPeriodEnd.After(t) {
			return StatePaymentFailed, s.CurrentPeriodEnd
		}
		return StateSuspended, time.Time{}
	}
}
