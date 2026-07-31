package billingperiod

import (
	"testing"
	"time"
)

// j0 is the anchor every dunning test measures from: the instant of the first
// failed charge. A mid-day hour on purpose — a schedule expressed in days must
// not quietly assume midnight anchors.
var j0 = date(2026, time.March, 10, 14)

// at returns the instant `d` after the first failure, so tests read in the
// vocabulary of the PRD (J+1, J+2, J+32) rather than in absolute dates.
func at(d time.Duration) time.Time { return j0.Add(d) }

const (
	day  = 24 * time.Hour
	tick = time.Nanosecond
)

// --- the nominal sequence ---------------------------------------------------

// The whole policy in one pass, walking a single subscription from the failed
// charge to cancellation and folding each decision back into the state exactly
// as a caller would.
func TestDunningNominalSequence(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0)

	if s.Attempts != 1 {
		t.Fatalf("opening a dunning counts the failed charge: Attempts = %d, want 1", s.Attempts)
	}

	// J+0: the charge just failed. Nothing more is due — access is kept while
	// the customer is told.
	if got := NextDunningAction(s, sched, at(0)); got != DunningNone {
		t.Errorf("at J+0: got %q, want %q", got, DunningNone)
	}

	// Anywhere inside the first day: still nothing.
	if got := NextDunningAction(s, sched, at(12*time.Hour)); got != DunningNone {
		t.Errorf("at J+0.5: got %q, want %q", got, DunningNone)
	}

	// J+1: the second and last attempt.
	if got := NextDunningAction(s, sched, at(day)); got != DunningRetry {
		t.Fatalf("at J+1: got %q, want %q", got, DunningRetry)
	}

	// The caller charges, it fails again, and folds that back in.
	s = s.RecordFailedAttempt(at(day))
	if s.Attempts != 2 {
		t.Fatalf("after the retry: Attempts = %d, want 2", s.Attempts)
	}

	// The anchor must NOT move: a retry cannot push suspension further away.
	if !s.FirstFailureAt.Equal(j0) {
		t.Errorf("a retry moved the anchor to %v, want %v", s.FirstFailureAt, j0)
	}

	// Between J+1 and J+2 the retry is spent and the cap is reached: nothing.
	if got := NextDunningAction(s, sched, at(day+6*time.Hour)); got != DunningNone {
		t.Errorf("at J+1.25: got %q, want %q", got, DunningNone)
	}

	// J+2: suspension.
	if got := NextDunningAction(s, sched, at(2*day)); got != DunningSuspend {
		t.Fatalf("at J+2: got %q, want %q", got, DunningSuspend)
	}
	s = s.Suspend()

	// Through the suspension month: nothing further, the customer may pay.
	for _, d := range []time.Duration{2 * day, 10 * day, 31 * day} {
		if got := NextDunningAction(s, sched, at(d)); got != DunningNone {
			t.Errorf("suspended at J+%v: got %q, want %q", d/day, got, DunningNone)
		}
	}

	// J+32: cancellation.
	if got := NextDunningAction(s, sched, at(32*day)); got != DunningCancel {
		t.Fatalf("at J+32: got %q, want %q", got, DunningCancel)
	}
	s = s.Cancel()

	// Terminal.
	if got := NextDunningAction(s, sched, at(40*day)); got != DunningNone {
		t.Errorf("cancelled at J+40: got %q, want %q", got, DunningNone)
	}
}

// --- the Lungor defect ------------------------------------------------------

// THE defect this file exists to close. In Lungor a failing subscription is
// retried for as long as its row exists, because nothing counts the attempts.
// Here, once the cap is reached, no clock value can produce a Retry.
func TestNoRetryOnceAttemptCapIsReached(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0).RecordFailedAttempt(at(day))

	// Sweep the whole dunning at a fine grain and past its end: not one Retry.
	for d := time.Duration(0); d <= 60*day; d += 3 * time.Hour {
		if got := NextDunningAction(s, sched, at(d)); got == DunningRetry {
			t.Fatalf("retry proposed at J+%v after the cap was reached", d)
		}
	}

	if CanCharge(s, sched) {
		t.Error("CanCharge is true after the attempt cap was reached")
	}
}

// A suspended subscription is never charged again — the give-up rule stated in
// the feature file as "a suspended subscription is never charged again".
func TestSuspendedSubscriptionIsNeverCharged(t *testing.T) {
	sched := DefaultDunningSchedule()
	// Suspended while an attempt was still nominally available, to prove it is
	// the suspension that forbids the charge and not the counter.
	s := OpenDunning(j0).Suspend()

	if s.Attempts >= sched.MaxAttempts {
		t.Fatalf("test setup: want attempts left, got %d of %d", s.Attempts, sched.MaxAttempts)
	}

	for d := time.Duration(0); d <= 31*day; d += 2 * time.Hour {
		if got := NextDunningAction(s, sched, at(d)); got == DunningRetry {
			t.Fatalf("retry proposed at J+%v on a suspended subscription", d)
		}
	}

	if CanCharge(s, sched) {
		t.Error("CanCharge is true on a suspended subscription")
	}
}

// Cancellation is terminal: no charge, no suspension, no further cancellation,
// however far the clock runs.
func TestCanceledSubscriptionIsNeverActedOnAgain(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0).Cancel()

	for d := time.Duration(0); d <= 400*day; d += 6 * time.Hour {
		if got := NextDunningAction(s, sched, at(d)); got != DunningNone {
			t.Fatalf("at J+%v on a cancelled subscription: got %q, want %q", d, got, DunningNone)
		}
	}

	if CanCharge(s, sched) {
		t.Error("CanCharge is true on a cancelled subscription")
	}
}

// A cancelled state that somehow still carries attempts left must stay
// terminal — cancellation wins over the counter.
func TestCanceledWinsOverRemainingAttempts(t *testing.T) {
	s := DunningState{FirstFailureAt: j0, Attempts: 1, Canceled: true}
	if got := NextDunningAction(s, DefaultDunningSchedule(), at(day)); got != DunningNone {
		t.Errorf("got %q, want %q", got, DunningNone)
	}
}

// --- exact boundaries -------------------------------------------------------

// Every threshold is inclusive of its instant, matching IsDue and Window's
// half-open boundary. One nanosecond either side of each.
func TestDunningThresholdBoundaries(t *testing.T) {
	sched := DefaultDunningSchedule()
	retried := OpenDunning(j0).RecordFailedAttempt(at(day))
	suspended := retried.Suspend()

	cases := []struct {
		name  string
		state DunningState
		now   time.Time
		want  DunningAction
	}{
		{"one tick before J+1", OpenDunning(j0), at(day - tick), DunningNone},
		{"exactly J+1", OpenDunning(j0), at(day), DunningRetry},
		{"one tick after J+1", OpenDunning(j0), at(day + tick), DunningRetry},

		{"one tick before J+2", retried, at(2*day - tick), DunningNone},
		{"exactly J+2", retried, at(2 * day), DunningSuspend},
		{"one tick after J+2", retried, at(2*day + tick), DunningSuspend},

		{"one tick before J+32", suspended, at(32*day - tick), DunningNone},
		{"exactly J+32", suspended, at(32 * day), DunningCancel},
		{"one tick after J+32", suspended, at(32*day + tick), DunningCancel},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextDunningAction(tc.state, sched, tc.now); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Past J+2, the retry is never proposed even by a state that still has attempts
// left: suspension closes the charging window whatever the counter says. This is
// the ordering of the checks, asserted directly.
func TestSuspendThresholdClosesTheRetryWindow(t *testing.T) {
	// One attempt made, cap not reached, but J+2 has passed.
	s := OpenDunning(j0)
	if got := NextDunningAction(s, DefaultDunningSchedule(), at(2*day)); got != DunningSuspend {
		t.Errorf("got %q, want %q — a pending attempt must not outrank suspension", got, DunningSuspend)
	}
}

// A trigger loop that has been down for a month comes back to a subscription
// well past every threshold. It must get the terminal action, not replay the
// schedule from the start.
func TestLateWakeupGoesStraightToTheCurrentAction(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0)

	if got := NextDunningAction(s, sched, at(50*day)); got != DunningCancel {
		t.Errorf("at J+50 with a never-processed dunning: got %q, want %q", got, DunningCancel)
	}
}

// --- idempotence ------------------------------------------------------------

// The decision is a pure function: asked repeatedly at the same instant with the
// same state, it answers the same thing and mutates nothing. The trigger loop
// passes over the same row on every tick, and must not accumulate effects.
func TestDecisionIsStableAndDoesNotMutate(t *testing.T) {
	sched := DefaultDunningSchedule()

	states := map[string]DunningState{
		"fresh":     OpenDunning(j0),
		"retried":   OpenDunning(j0).RecordFailedAttempt(at(day)),
		"suspended": OpenDunning(j0).RecordFailedAttempt(at(day)).Suspend(),
		"cancelled": OpenDunning(j0).RecordFailedAttempt(at(day)).Suspend().Cancel(),
	}

	for name, s := range states {
		for _, d := range []time.Duration{0, day, 2 * day, 10 * day, 32 * day} {
			now := at(d)
			before := s
			first := NextDunningAction(s, sched, now)
			for i := 0; i < 5; i++ {
				if got := NextDunningAction(s, sched, now); got != first {
					t.Fatalf("%s at J+%v: call %d gave %q, first gave %q", name, d/day, i+2, got, first)
				}
			}
			if s != before {
				t.Fatalf("%s at J+%v: the decision mutated the state", name, d/day)
			}
		}
	}
}

// Acting on a decision makes it stop being proposed: after suspending, the
// answer is None, not Suspend again. Without this a loop would re-suspend — and,
// once wired, re-send the suspension email — on every tick for thirty days.
func TestActingOnADecisionRetiresIt(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0).RecordFailedAttempt(at(day))

	if got := NextDunningAction(s, sched, at(2*day)); got != DunningSuspend {
		t.Fatalf("setup: got %q, want %q", got, DunningSuspend)
	}
	s = s.Suspend()
	if got := NextDunningAction(s, sched, at(2*day)); got != DunningNone {
		t.Errorf("after suspending: got %q, want %q", got, DunningNone)
	}
}

// The J+1 retry is proposed exactly once, however many times the loop passes
// between J+1 and J+2 — as long as the caller records the attempt it made.
func TestRetryIsProposedOnlyOncePerWindow(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0)

	proposals := 0
	for d := time.Duration(0); d < 2*day; d += 30 * time.Minute {
		if NextDunningAction(s, sched, at(d)) == DunningRetry {
			proposals++
			s = s.RecordFailedAttempt(at(d))
		}
	}

	if proposals != 1 {
		t.Errorf("retry proposed %d times between J+0 and J+2, want exactly 1", proposals)
	}
}

// A retry recorded strictly BEFORE the threshold — an out-of-band attempt, say a
// customer-triggered one — does not consume the scheduled retry window. Only the
// attempt cap does.
func TestAnEarlyAttemptDoesNotConsumeTheRetryWindow(t *testing.T) {
	sched := DunningSchedule{
		RetryAfter:   day,
		SuspendAfter: 2 * day,
		CancelAfter:  32 * day,
		MaxAttempts:  3, // room for the early attempt plus the scheduled one
	}
	s := OpenDunning(j0).RecordFailedAttempt(at(2 * time.Hour))

	if got := NextDunningAction(s, sched, at(day)); got != DunningRetry {
		t.Errorf("got %q, want %q", got, DunningRetry)
	}
}

// --- recovery ---------------------------------------------------------------

// A payment mid-dunning ends it cleanly, and the next failure starts from a
// clean counter rather than inheriting a spent one.
func TestSettleEndsTheDunning(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0).RecordFailedAttempt(at(day)).Settle()

	if s.InDunning() {
		t.Error("still in dunning after settling")
	}
	if s.Attempts != 0 {
		t.Errorf("Attempts = %d after settling, want 0", s.Attempts)
	}
	if !CanCharge(s, sched) {
		t.Error("CanCharge is false after settling")
	}
	// Well past every threshold: a settled subscription is not suspended or
	// cancelled by the clock catching up.
	if got := NextDunningAction(s, sched, at(40*day)); got != DunningNone {
		t.Errorf("at J+40 after settling: got %q, want %q", got, DunningNone)
	}

	// A later failure gets a full, fresh schedule.
	later := date(2026, time.June, 1, 9)
	s = s.RecordFailedAttempt(later)
	if s.Attempts != 1 {
		t.Errorf("Attempts = %d on the new dunning, want 1", s.Attempts)
	}
	if !s.FirstFailureAt.Equal(later) {
		t.Errorf("anchor = %v, want %v", s.FirstFailureAt, later)
	}
	if got := NextDunningAction(s, sched, later.Add(day)); got != DunningRetry {
		t.Errorf("J+1 of the new dunning: got %q, want %q", got, DunningRetry)
	}
}

// Suspension is recoverable — the reason it was chosen over cancellation. A
// payment at J+20 restores a chargeable, unsuspended subscription.
func TestSettleFromSuspendedRestoresTheSubscription(t *testing.T) {
	sched := DefaultDunningSchedule()
	s := OpenDunning(j0).RecordFailedAttempt(at(day)).Suspend().Settle()

	if s.Suspended {
		t.Error("still suspended after settling")
	}
	if !CanCharge(s, sched) {
		t.Error("CanCharge is false after settling from suspended")
	}
	if got := NextDunningAction(s, sched, at(32*day)); got != DunningNone {
		t.Errorf("at J+32 after settling: got %q, want %q — a recovered customer must not be cancelled", got, DunningNone)
	}
}

// --- custom schedules -------------------------------------------------------

// Lungor is multi-tenant: another tenant wants another schedule and must get it
// without forking the policy.
func TestCustomScheduleIsHonoured(t *testing.T) {
	// A far more aggressive tenant: retry after an hour, suspend after four,
	// cancel after a week, three attempts allowed.
	sched := DunningSchedule{
		RetryAfter:   time.Hour,
		SuspendAfter: 4 * time.Hour,
		CancelAfter:  7 * day,
		MaxAttempts:  3,
	}

	s := OpenDunning(j0)

	if got := NextDunningAction(s, sched, at(59*time.Minute)); got != DunningNone {
		t.Errorf("before the retry threshold: got %q, want %q", got, DunningNone)
	}
	if got := NextDunningAction(s, sched, at(time.Hour)); got != DunningRetry {
		t.Fatalf("at the retry threshold: got %q, want %q", got, DunningRetry)
	}

	s = s.RecordFailedAttempt(at(time.Hour))
	// A third attempt is allowed by this tenant's cap, and the retry window has
	// re-opened for it only once the threshold is crossed again — here it is
	// already crossed, so the next pass proposes it.
	if got := NextDunningAction(s, sched, at(2*time.Hour)); got != DunningNone {
		t.Errorf("with the window's retry already spent: got %q, want %q", got, DunningNone)
	}

	if got := NextDunningAction(s, sched, at(4*time.Hour)); got != DunningSuspend {
		t.Errorf("at the suspend threshold: got %q, want %q", got, DunningSuspend)
	}

	s = s.Suspend()
	if got := NextDunningAction(s, sched, at(7*day)); got != DunningCancel {
		t.Errorf("at the cancel threshold: got %q, want %q", got, DunningCancel)
	}

	// The default thresholds are NOT in force for this tenant: at J+2 under the
	// default it would suspend; here it is long past that and already cancelled.
	if got := NextDunningAction(s, DefaultDunningSchedule(), at(2*day)); got != DunningNone {
		t.Errorf("sanity: got %q, want %q", got, DunningNone)
	}
}

// A single-attempt tenant never retries at all: the J+0 charge is the only one.
func TestSingleAttemptScheduleNeverRetries(t *testing.T) {
	sched := DefaultDunningSchedule()
	sched.MaxAttempts = 1

	s := OpenDunning(j0)
	if got := NextDunningAction(s, sched, at(day)); got != DunningNone {
		t.Errorf("at J+1 with MaxAttempts=1: got %q, want %q", got, DunningNone)
	}
	if got := NextDunningAction(s, sched, at(2*day)); got != DunningSuspend {
		t.Errorf("at J+2 with MaxAttempts=1: got %q, want %q", got, DunningSuspend)
	}
}

// The zero schedule reads as the PRD default rather than as "every threshold is
// now", so a caller that forgets to pass one does not suspend instantly.
func TestZeroScheduleFallsBackToTheDefault(t *testing.T) {
	var zero DunningSchedule
	s := OpenDunning(j0)

	if got := NextDunningAction(s, zero, at(0)); got != DunningNone {
		t.Errorf("at J+0 with a zero schedule: got %q, want %q", got, DunningNone)
	}
	if got := NextDunningAction(s, zero, at(day)); got != DunningRetry {
		t.Errorf("at J+1 with a zero schedule: got %q, want %q", got, DunningRetry)
	}
	if got := NextDunningAction(s.RecordFailedAttempt(at(day)), zero, at(2*day)); got != DunningSuspend {
		t.Errorf("at J+2 with a zero schedule: got %q, want %q", got, DunningSuspend)
	}
}

// An incoherent schedule must fail towards the safe direction — never towards
// "retry forever". Out-of-order thresholds are pulled into order, so suspension
// still comes at or after the retry and cancellation at or after suspension.
func TestIncoherentScheduleIsOrderedSafely(t *testing.T) {
	sched := DunningSchedule{
		RetryAfter:   10 * day,
		SuspendAfter: day, // before the retry — incoherent
		CancelAfter:  2 * day,
		MaxAttempts:  2,
	}
	s := OpenDunning(j0)

	// Suspension is pulled up to the retry threshold and cancellation up to
	// suspension, so all three collapse onto J+10. No Retry is ever proposed:
	// the charging window cannot outlive suspension.
	for d := time.Duration(0); d <= 40*day; d += time.Hour {
		if got := NextDunningAction(s, sched, at(d)); got == DunningRetry {
			t.Fatalf("retry proposed at J+%v under an incoherent schedule", d)
		}
	}
	// Nothing happens before the collapsed threshold — an incoherent schedule
	// must not act early, which would suspend a customer ahead of their warning.
	if got := NextDunningAction(s, sched, at(10*day-tick)); got != DunningNone {
		t.Errorf("before the collapsed threshold: got %q, want %q", got, DunningNone)
	}
	// At it, the most terminal action wins, since cancellation was ordered no
	// earlier than suspension and both now sit on the same instant.
	if got := NextDunningAction(s, sched, at(10*day)); got != DunningCancel {
		t.Errorf("at the collapsed threshold: got %q, want %q", got, DunningCancel)
	}
}

// A schedule whose cancel threshold is coherent but whose suspend threshold is
// not still orders cleanly: suspension at the retry instant, cancellation later.
func TestIncoherentSuspendThresholdStillOrdersBeforeCancel(t *testing.T) {
	sched := DunningSchedule{
		RetryAfter:   5 * day,
		SuspendAfter: day, // before the retry — pulled up to J+5
		CancelAfter:  20 * day,
		MaxAttempts:  2,
	}
	s := OpenDunning(j0)

	if got := NextDunningAction(s, sched, at(5*day)); got != DunningSuspend {
		t.Errorf("at the ordered suspend threshold: got %q, want %q", got, DunningSuspend)
	}
	if got := NextDunningAction(s.Suspend(), sched, at(20*day)); got != DunningCancel {
		t.Errorf("at the cancel threshold: got %q, want %q", got, DunningCancel)
	}
}

// A negative MaxAttempts must not be read as "no cap".
func TestNonPositiveMaxAttemptsFallsBackToTheDefault(t *testing.T) {
	sched := DefaultDunningSchedule()
	sched.MaxAttempts = -5

	s := OpenDunning(j0).RecordFailedAttempt(at(day))
	if CanCharge(s, sched) {
		t.Error("CanCharge is true with a negative MaxAttempts — the cap was lost")
	}
	if got := NextDunningAction(s, sched, at(day+time.Hour)); got == DunningRetry {
		t.Error("retry proposed with a negative MaxAttempts")
	}
}

// --- state hygiene ----------------------------------------------------------

// The zero state is "not in dunning": every decision on it is None, and it is
// chargeable. A subscription paying normally must never be acted on.
func TestZeroStateIsNotInDunning(t *testing.T) {
	var s DunningState
	sched := DefaultDunningSchedule()

	if s.InDunning() {
		t.Error("the zero state reports as in dunning")
	}
	if !CanCharge(s, sched) {
		t.Error("the zero state is not chargeable")
	}
	for _, d := range []time.Duration{0, day, 2 * day, 32 * day, 400 * day} {
		if got := NextDunningAction(s, sched, at(d)); got != DunningNone {
			t.Errorf("zero state at J+%v: got %q, want %q", d/day, got, DunningNone)
		}
	}
}

// RecordFailedAttempt on a subscription not yet in dunning opens one: a failure
// is a failure however the caller reached it.
func TestRecordFailedAttemptOpensADunning(t *testing.T) {
	var s DunningState
	s = s.RecordFailedAttempt(j0)

	if !s.InDunning() {
		t.Fatal("not in dunning after a failed attempt")
	}
	if s.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", s.Attempts)
	}
	if !s.FirstFailureAt.Equal(j0) {
		t.Errorf("anchor = %v, want %v", s.FirstFailureAt, j0)
	}
}

// Anchors and attempt instants are normalized to UTC, like Activate's window, so
// a schedule does not depend on the caller's locale. A dunning opened from a
// zoned clock must hit its thresholds at the same absolute instants.
func TestDunningIsZoneIndependent(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*3600)
	zoned := j0.In(zone)

	s := OpenDunning(zoned)
	if s.FirstFailureAt.Location() != time.UTC {
		t.Errorf("anchor location = %v, want UTC", s.FirstFailureAt.Location())
	}
	if !s.FirstFailureAt.Equal(j0) {
		t.Errorf("anchor = %v, want the same instant as %v", s.FirstFailureAt, j0)
	}

	sched := DefaultDunningSchedule()
	if got := NextDunningAction(s, sched, at(day).In(zone)); got != DunningRetry {
		t.Errorf("at J+1 read from a zoned clock: got %q, want %q", got, DunningRetry)
	}
	if got := NextDunningAction(s, sched, at(day-tick).In(zone)); got != DunningNone {
		t.Errorf("one tick before J+1 read from a zoned clock: got %q, want %q", got, DunningNone)
	}
}
