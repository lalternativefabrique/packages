package billing

import (
	"testing"
	"time"
)

// Entitled is the gate every paid feature hangs off. The case that motivates the
// table is "canceled + future period": cancelling stops the next charge, it does
// not refund the month already paid, so access must survive until it lapses.
//
// The table is carried over VERBATIM from transcript-api's infra.TestEntitled.
// That is the whole point of this test: the extraction must not change a single
// verdict, and the cheapest proof is the same cases against the moved rule.
func TestEntitled(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	cases := []struct {
		name      string
		status    string
		periodEnd time.Time
		want      bool
		why       string
	}{
		{"active, period running", "active", future, true,
			"the ordinary paying customer"},
		{"canceled, period still running", "canceled", future, true,
			"the month is paid: cancelling stops the next charge, it does not claw back this one"},
		{"canceled, period lapsed", "canceled", past, false,
			"nothing left owed — the gate closes on its own, no cron needed"},
		{"active, period lapsed", "active", past, false,
			"a renewal that never landed must not keep entitling on a stale status"},
		{"past_due, period running", "past_due", future, false,
			"the money did not arrive; this is also the state a fresh checkout starts in"},
		{"past_due, period lapsed", "past_due", past, false,
			"unpaid and expired"},
		{"unknown status", "whatever", future, false,
			"only statuses we understand may entitle"},
		{"zero value", "", time.Time{}, false,
			"an owner with no subscription entitles nothing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Entitled(Subscription{Status: tc.status, CurrentPeriodEnd: tc.periodEnd}, now)
			if got != tc.want {
				t.Fatalf("Entitled(%s, end=%v) = %v, want %v — %s",
					tc.status, tc.periodEnd, got, tc.want, tc.why)
			}
		})
	}
}

// StateFor answers "is the money healthy", where Entitled answers "may this
// request proceed". The pairing is what this package exists to make visible, so
// the table asserts both at once and pins the cases where they legitimately
// disagree.
func TestStateFor(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	cases := []struct {
		name         string
		hasSub       bool
		status       string
		periodEnd    time.Time
		wantState    State
		wantDeadline bool
		why          string
	}{
		{"no subscription at all", false, "", time.Time{}, StateHealthy, false,
			"never having subscribed is not a payment problem"},
		{"active, period running", true, "active", future, StateHealthy, false,
			"the ordinary paying customer"},
		{"canceled, period running", true, "canceled", future, StateHealthy, false,
			"a cancellation is a choice, not a failure — reported through status, not as trouble"},
		{"active, period lapsed", true, "active", past, StateSuspended, false,
			"the renewal never landed: access is gone and no deadline remains to offer"},
		{"past_due, period running", true, "past_due", future, StatePaymentFailed, true,
			"the actionable window: the card failed but the month is still theirs"},
		{"past_due, period lapsed", true, "past_due", past, StateSuspended, false,
			"unrecovered: access stopped, nothing left to promise"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := Subscription{Status: tc.status, CurrentPeriodEnd: tc.periodEnd}
			state, endsAt := StateFor(sub, tc.hasSub, now)
			if state != tc.wantState {
				t.Fatalf("StateFor(%s, end=%v) = %q, want %q — %s",
					tc.status, tc.periodEnd, state, tc.wantState, tc.why)
			}
			if got := !endsAt.IsZero(); got != tc.wantDeadline {
				t.Fatalf("StateFor(%s) deadline present = %v, want %v — a deadline is only offered while the customer can still act on it",
					tc.status, got, tc.wantDeadline)
			}
		})
	}
}

// The two predicates must never contradict each other in the direction that
// matters: reporting "healthy" while the gate is shut would tell a customer
// everything is fine as their requests are refused.
//
// The converse IS allowed and is exercised above: past_due inside a running
// period reports payment_failed while Entitled already refuses. That gap is
// deliberate (the grace window is not implemented yet), and it errs safe.
func TestHealthyNeverCoversAClosedGate(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	for _, status := range []string{StatusActive, StatusPastDue, StatusCanceled, "unknown"} {
		for _, end := range []time.Time{future, past} {
			sub := Subscription{Status: status, CurrentPeriodEnd: end}
			state, _ := StateFor(sub, true, now)
			if state == StateHealthy && !Entitled(sub, now) {
				t.Fatalf("status %q (end=%v) reports healthy but is not entitled: "+
					"the customer would be told nothing is wrong while being refused",
					status, end)
			}
		}
	}
}
