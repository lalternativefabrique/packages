package billing

import "time"

// DefaultProrationFloorCents is the amount below which an upgrade charges
// nothing at all.
//
// 2 € — a product decision, recorded here because it is the kind of number that
// otherwise gets re-invented per call site. The reasoning: an upgrade made in
// the last days of a period prorates to a few dozen cents, of which the PSP's
// fixed fee (~0,25 €) takes most. Collecting it would mean introducing a
// payment that can FAIL — and therefore block the upgrade — to net a fraction
// of a euro. "No charge today" is also the better commercial message.
const DefaultProrationFloorCents int64 = 200

// Prorate returns what to charge NOW for moving from a tier costing
// fromCents/period to one costing toCents/period, part-way through the period
// w.
//
// The amount is the DIFFERENCE, never a catalogue price. The period already
// paid was settled at the old price; only the top-up to the new tier is owed,
// and only for the time left:
//
//	(toCents - fromCents) × remaining / total
//
// Charging the new price in full would bill the current period twice; charging
// the old one is meaningless — it is the price of the tier being left.
//
// Rounding is UP to the cent, in the merchant's favour, because the alternative
// is losing a cent per upgrade forever to a rounding rule nobody chose.
//
// Below floorCents the result is 0: the caller charges nothing and grants the
// tier anyway. Pass DefaultProrationFloorCents unless the product says
// otherwise; a floor of 0 charges every non-zero amount.
//
// Returns 0 — never a negative — when the move is not an increase, when the
// window has already ended, or when the window is degenerate. A downgrade is
// scheduled for the next renewal and refunds nothing, so there is no negative
// proration in this model, and a stray negative here would become a CREDIT at
// the provider.
//
// Pure by construction: a window, two amounts, a clock reading. No owner, no
// provider, no database. This is the piece Lungor is meant to take back as-is.
func Prorate(w Window, fromCents, toCents int64, now time.Time, floorCents int64) int64 {
	delta := toCents - fromCents
	if delta <= 0 {
		return 0
	}
	total := w.End.Sub(w.Start)
	if total <= 0 {
		// A degenerate window carries no information about how much of the period
		// is left. Charging a full delta on it would bill a whole period's
		// difference for an unknown remainder.
		return 0
	}
	remaining := w.End.Sub(now)
	if remaining <= 0 {
		// The period is over: the next renewal already charges the new price in
		// full, so there is nothing to top up.
		return 0
	}
	if remaining > total {
		// now precedes the window (clock skew, or a window opened in the future).
		// Clamp rather than extrapolate: the most that can be owed is one period's
		// difference.
		remaining = total
	}

	// Integer arithmetic throughout, and the multiplication comes FIRST: money is
	// never computed in floating point, and dividing first would discard the
	// remainder that the ceiling below exists to catch.
	num := delta * int64(remaining)
	den := int64(total)
	amount := num / den
	if num%den != 0 {
		amount++ // round up to the cent
	}

	if amount < floorCents {
		return 0
	}
	return amount
}

// Window is the half-open period [Start, End) a proration is computed against.
//
// It mirrors lib/billingperiod.Window deliberately rather than importing it:
// this package must stay free of dependencies so it can be vendored into
// another codebase whole, and the shape is two timestamps. Callers that already
// hold a billingperiod.Window convert it in one line.
type Window struct {
	Start time.Time
	End   time.Time
}
