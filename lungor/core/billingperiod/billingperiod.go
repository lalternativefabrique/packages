// Package billingperiod computes subscription billing cycles: when a period
// starts, when it ends, and when it is due for renewal.
//
// # Why this exists
//
// It is ported from Lungor's finance module (`lungor/apps/core/finance`), where
// the same rule lives in `application/activate` and `application/rebill`
// (duplicated between the two as a local `addInterval`).
//
// This is Lungor's billing engine being PROVEN IN PRODUCTION USE on Techtuel,
// not a local utility that happens to resemble it. What survives here is meant
// to be carried back into Lungor, hardened. Two rules follow from that, and
// they should survive any future edit to this file:
//
//   - Fidelity over invention. Names, semantics and boundaries track
//     `lungor/core/finance` wherever that is defensible, so a port back reads as
//     a transplant rather than a reinterpretation. `Interval`/`AddTo` mirror
//     `BillingInterval`/`addInterval`; `Activate` and `Advance` mirror the
//     activate and rebill handlers; `Window` names what Lungor spells as the
//     `CurrentPeriodStart`/`CurrentPeriodEnd` pair on its Subscription.
//   - Defects found while porting are reported UPSTREAM, not just worked around
//     here. See the month-end bug below.
//
// The code is embedded, deliberately. Techtuel does not call Lungor's API and
// no migration onto it is planned; there is no `GET /entitlements` round-trip
// in this design and none should be added on its account.
//
// # The model: the system owns the cycle, the PSP only moves money
//
// The billing period is computed LOCALLY, anchored on the date a payment was
// confirmed. It is deliberately NOT read from the payment provider: Mollie's
// subscription object and its `nextPaymentDate` are not used, and Mollie's
// recurring schedule is not the source of truth for entitlement. The PSP
// charges the card; this package decides what the customer is entitled to and
// until when.
//
// The consequence that matters is Advance: a renewal starts where the previous
// period ENDED, never at `time.Now()`. Anchoring a renewal on the clock makes
// every late webhook shift the cycle, and the slip compounds month after month.
// A subscriber billed on the 18th resets on the 18th, whatever day their
// webhook was actually processed.
//
// # BUG FOUND IN LUNGOR — month-end overflow (fix upstream)
//
// Status: present and UNFIXED in Lungor as of this port. Fixed here. The patch
// below is written to be portable back verbatim.
//
// Lungor's `addInterval` (duplicated in `finance/application/activate/handler.go`
// and `finance/application/rebill/handler.go`) is a bare
// `start.AddDate(0, count, 0)`. Go's AddDate normalizes overflow FORWARD, so a
// cycle anchored on a day the target month does not have walks past the end of
// that month:
//
//	2026-01-31 AddDate(0,1,0) → 2026-03-03   want 2026-02-28
//	2026-01-30 AddDate(0,1,0) → 2026-03-02   want 2026-02-28
//	2028-02-29 AddDate(1,0,0) → 2029-03-01   want 2029-02-28
//
// Impact on Lungor: a subscriber anchored on the 29th–31st is billed on the
// 1st–3rd of the following month from their SECOND period on, and the anchor
// keeps sliding forward every short month. It also silently lengthens those
// periods — the customer gets days they did not pay for, and the "monthly"
// cycle is not monthly. Roughly a tenth of all anchor dates are affected.
//
// Fix: clamp the day to the last valid day of the target month (see
// addMonthsClamped), which is the standard subscription-billing behaviour and
// what Stripe and Mollie both do. Lungor should adopt this; do NOT resolve the
// divergence by reverting to AddDate here.
//
// Clamping is not reversible, by design: an anchor on the 31st clamped to
// 28 February advances to 28 March, not back to the 31st. Preserving the
// original day-of-month would require carrying the anchor separately from the
// window, which buys a cosmetic gain for real complexity. Lungor has no
// day-of-month field either, so this keeps the two models aligned.
//
// This package is pure: no database, no HTTP, no PSP. Every function is a
// deterministic transformation of its arguments and takes any clock explicitly.
package billingperiod

import "time"

// Interval is the unit a billing cycle advances by.
type Interval string

const (
	IntervalMonth Interval = "month"
	IntervalYear  Interval = "year"
)

// AddTo advances t by count intervals, clamping to the last day of the target
// month so a cycle anchored on a day that the target month does not have stays
// inside that month (31 Jan + 1 month = 28 Feb, not 3 Mar).
//
// A count below 1 is treated as 1, and an unrecognised interval falls back to
// one month — both ported from Lungor's addInterval, so a malformed plan yields
// the shortest sane period rather than an unbounded one. Failing towards a
// SHORTER period is the safe direction: it under-grants entitlement, which is
// recoverable, instead of granting a free year.
func (iv Interval) AddTo(t time.Time, count int) time.Time {
	if count < 1 {
		count = 1
	}
	switch iv {
	case IntervalYear:
		return addMonthsClamped(t, 12*count)
	case IntervalMonth:
		return addMonthsClamped(t, count)
	default:
		return addMonthsClamped(t, count)
	}
}

// addMonthsClamped adds months to t, preserving the day of month when the
// target month has it and clamping to that month's last day when it does not.
//
// It sidesteps AddDate's forward normalization by building the target date from
// its parts: the year/month arithmetic is done first, the day is clamped
// against the resulting month's length, and only then is the date constructed.
func addMonthsClamped(t time.Time, months int) time.Time {
	year, month, day := t.Date()

	// Shift to a 0-based absolute month count so the arithmetic carries across
	// year boundaries without special cases, then convert back.
	total := int(month) - 1 + months
	targetYear := year + floorDiv(total, 12)
	targetMonth := time.Month(floorMod(total, 12) + 1)

	if max := daysIn(targetYear, targetMonth); day > max {
		day = max
	}

	hour, min, sec := t.Clock()
	return time.Date(targetYear, targetMonth, day, hour, min, sec, t.Nanosecond(), t.Location())
}

// daysIn is the number of days in a month, leap years included: day 0 of the
// following month is the last day of this one.
func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// floorDiv / floorMod divide towards negative infinity, so subtracting months
// from a January date rolls into the previous year correctly. Go's / and %
// truncate towards zero, which would give the wrong month for negative totals.
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func floorMod(a, b int) int {
	m := a % b
	if m != 0 && (m < 0) != (b < 0) {
		m += b
	}
	return m
}

// Window is the half-open interval [Start, End) a subscription period covers.
// Half-open so consecutive periods tile without overlap: the instant that ends
// one period is the instant that begins the next, and a debit at that instant
// belongs to exactly one of them.
type Window struct {
	Start time.Time
	End   time.Time
}

// Contains reports whether t falls inside the window: Start is included, End is
// not. A zero-width or inverted window contains nothing.
func (w Window) Contains(t time.Time) bool {
	return !t.Before(w.Start) && t.Before(w.End)
}

// Activate opens the FIRST billing period of a subscription, anchored on the
// date the payment was confirmed. paidAt is the payment's own timestamp, not
// the moment the webhook happened to be processed — a webhook delivered three
// days late must not shift the cycle by three days.
//
// Normalized to UTC so a window is comparable however the caller's clock is
// zoned, and so the stored anchor never depends on the server's locale.
func Activate(paidAt time.Time, iv Interval, count int) Window {
	start := paidAt.UTC()
	return Window{Start: start, End: iv.AddTo(start, count)}
}

// Advance opens the NEXT period after w: it starts exactly where w ended and
// runs one interval further.
//
// It takes the previous window rather than a clock on purpose. Computing a
// renewal as `now + 1 month` is what lets a late webhook shift the cycle, and
// the shift compounds every month — which is the drift this package exists to
// remove. The renewal date is a property of the subscription, not of when we
// got around to processing it.
func Advance(w Window, iv Interval, count int) Window {
	start := w.End.UTC()
	return Window{Start: start, End: iv.AddTo(start, count)}
}

// IsDue reports whether w has run out by now — i.e. the subscription is due to
// be renewed (or to lapse). True exactly when now is at or past End, matching
// Contains' half-open boundary.
func IsDue(w Window, now time.Time) bool {
	return !now.UTC().Before(w.End.UTC())
}

// WindowContaining walks forward from an anchor window to the period containing
// now, and reports it. It is how a stored anchor is turned into "the period the
// tenant is in right now" without a cron job writing a new row every month: the
// cycle is recomputed from the anchor on read.
//
// Each step is an Advance, so the tiling stays exact and no drift is introduced
// however many periods have elapsed. A window that already contains now (or lies
// in the future) is returned unchanged.
//
// The iteration is bounded: a corrupt anchor far in the past must not spin. On
// hitting the bound the last window computed is returned — a stale but bounded
// window, never an open-ended one that would read as unlimited quota.
func WindowContaining(anchor Window, iv Interval, count int, now time.Time) Window {
	const maxSteps = 1200 // a century of monthly periods
	w := anchor
	for i := 0; i < maxSteps && IsDue(w, now); i++ {
		w = Advance(w, iv, count)
	}
	return w
}
