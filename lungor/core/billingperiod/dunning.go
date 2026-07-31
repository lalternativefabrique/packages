package billingperiod

import "time"

// --- Dunning: the policy for a charge that failed ---------------------------
//
// # BUG FOUND IN LUNGOR — unbounded retries (fix upstream)
//
// Status: present and UNFIXED in Lungor as of this port. Fixed here. This is the
// THIRD defect found while porting, after the month-end overflow and the
// duplicated addInterval.
//
// Lungor's rebill path (`lungor/core/finance/application/rebill/handler.go`,
// fed by a ListDue that includes `past_due` in
// `lungor/core/finance/infrastructure/postgres_subscriptions.go`) retries a
// failed charge with NO attempt counter, NO interval between attempts, NO cap
// and NO give-up. A permanently dead card — the single most common failure, an
// expired one — is therefore retried for as long as the row exists. Every
// attempt is a real PSP call, and a billable one at some PSPs.
//
// The rule below closes that: attempts are counted, capped, and once the cap is
// reached the decision is to suspend, never to retry again. It lives in this
// package rather than in the caller for the same reason Activate/Advance do —
// this package is Lungor's billing engine being proven in production use, and
// this policy is meant to be carried back into Lungor with it.
//
// # The policy (docs/epic/prd-dunning.md, decided)
//
//	J+0        charge fails; access kept, customer told
//	J+1        second and LAST attempt; access kept, customer warned
//	J+2        suspension; access cut, data kept
//	J+2..J+32  suspended, recoverable by a single payment
//	J+32       cancellation; mandate revoked, data kept
//
// One policy for every failure cause. An expired card fails the J+1 retry
// identically and only the customer can fix it, but mapping PSP decline codes to
// decide that is a source of silent bugs; the wasted attempt is cheaper.
//
// Like the rest of this package, this is pure: no database, no HTTP, no PSP, and
// no implicit clock. `now` is always a parameter — never time.Now() — which is
// what makes the schedule testable at its exact boundaries and portable.

// DunningAction is the single next thing to do for a failing subscription. It is
// a DECISION, not an effect: this package never charges, suspends, sends, or
// writes. The caller maps the action onto its own infrastructure.
type DunningAction string

const (
	// DunningNone means nothing is due yet — the next threshold is still in the
	// future. It is also what a settled or cancelled dunning returns, so a
	// trigger loop passing repeatedly over the same row does nothing extra.
	DunningNone DunningAction = "none"

	// DunningRetry means attempt the charge again. Returned at most once per
	// dunning, at the retry threshold, and never once the attempt cap is
	// reached — that cap is the give-up rule Lungor lacks.
	DunningRetry DunningAction = "retry"

	// DunningSuspend means cut access, keep data. The subscription stays
	// recoverable by a single payment for a month.
	DunningSuspend DunningAction = "suspend"

	// DunningCancel means revoke the mandate and close the subscription. Data
	// are kept; any purge is a separate rule with its own notice.
	DunningCancel DunningAction = "cancel"
)

// DunningSchedule is the set of thresholds, all measured from the FIRST failure
// (J+0), plus the attempt cap.
//
// They are named parameters and not constants buried in the decision function
// because Lungor is multi-tenant: another tenant will want another schedule, and
// it must be able to say so without forking the policy. DefaultDunningSchedule
// carries the values decided in the PRD.
type DunningSchedule struct {
	// RetryAfter is the delay from the first failure to the second and last
	// charge attempt. PRD: 1 day.
	RetryAfter time.Duration

	// SuspendAfter is the delay from the first failure to suspension. PRD: 2
	// days — deliberately short, and deliberately not spanning a payroll cycle,
	// which is why the notification carries the weight rather than one more
	// attempt.
	SuspendAfter time.Duration

	// CancelAfter is the delay from the first failure to cancellation. PRD: 32
	// days, i.e. a full month of suspension during which one payment restores
	// everything.
	CancelAfter time.Duration

	// MaxAttempts is the total number of charge attempts allowed for the failing
	// period, INCLUDING the J+0 charge that opened the dunning. PRD: 2 (the
	// original charge, plus the J+1 retry). Once Attempts reaches it, no retry
	// is ever proposed again — this is the cap Lungor has no equivalent of.
	MaxAttempts int
}

// DefaultDunningSchedule is the policy decided in docs/epic/prd-dunning.md:
// retry at J+1, suspend at J+2, cancel at J+32, two attempts in total.
func DefaultDunningSchedule() DunningSchedule {
	return DunningSchedule{
		RetryAfter:   24 * time.Hour,
		SuspendAfter: 48 * time.Hour,
		CancelAfter:  32 * 24 * time.Hour,
		MaxAttempts:  2,
	}
}

// normalized returns the schedule with any unset or incoherent field replaced by
// the default, and the thresholds forced into a non-decreasing order.
//
// It fails towards the SAFE direction, the same way Interval.AddTo does: a
// malformed schedule must never yield "retry forever". An out-of-order threshold
// is pulled up to the one before it rather than dropped, so suspension can only
// ever come at or after the retry, and cancellation at or after the suspension.
func (s DunningSchedule) normalized() DunningSchedule {
	d := DefaultDunningSchedule()
	if s.RetryAfter <= 0 {
		s.RetryAfter = d.RetryAfter
	}
	if s.SuspendAfter <= 0 {
		s.SuspendAfter = d.SuspendAfter
	}
	if s.CancelAfter <= 0 {
		s.CancelAfter = d.CancelAfter
	}
	if s.MaxAttempts < 1 {
		s.MaxAttempts = d.MaxAttempts
	}
	if s.SuspendAfter < s.RetryAfter {
		s.SuspendAfter = s.RetryAfter
	}
	if s.CancelAfter < s.SuspendAfter {
		s.CancelAfter = s.SuspendAfter
	}
	return s
}

// DunningState is where a failing subscription currently stands. It is the
// state Lungor does not persist at all (PRD FR2), and without which the schedule
// cannot be deterministic across restarts: a process that forgets how many times
// it already charged will charge again.
//
// The zero value means "not in dunning" — FirstFailureAt is zero — and every
// decision on it is DunningNone.
type DunningState struct {
	// FirstFailureAt is the instant of the J+0 failure: the anchor every
	// threshold is measured from. It is the payment's own timestamp, not the
	// moment a webhook was processed, for the same reason Activate takes paidAt.
	// Zero means no dunning is in progress.
	FirstFailureAt time.Time

	// Attempts is the number of charge attempts made for the failing period,
	// INCLUDING the J+0 charge that failed. It therefore starts at 1 when a
	// dunning opens, and is compared against DunningSchedule.MaxAttempts.
	Attempts int

	// LastAttemptAt is the instant of the most recent attempt. It exists so a
	// retry is not proposed twice inside the same threshold window: once an
	// attempt is recorded at or after the retry threshold, the retry is spent.
	LastAttemptAt time.Time

	// Suspended records that access has already been cut. It makes the decision
	// idempotent — a loop passing again over a suspended row is told None, not
	// Suspend — and it is what forbids any further charge (PRD FR3).
	Suspended bool

	// Canceled records that the subscription has been closed and the mandate
	// revoked. Terminal: every decision on a cancelled dunning is None. This is
	// the "no attempt is ever made on a cancelled subscription" rule.
	Canceled bool
}

// InDunning reports whether a dunning is in progress at all.
func (s DunningState) InDunning() bool {
	return !s.FirstFailureAt.IsZero()
}

// OpenDunning starts a dunning from the first failed charge, recording that one
// attempt has already been made. failedAt is the failing charge's own timestamp.
//
// Normalized to UTC, like Activate, so the schedule does not depend on the
// caller's locale.
func OpenDunning(failedAt time.Time) DunningState {
	return DunningState{
		FirstFailureAt: failedAt.UTC(),
		Attempts:       1,
		LastAttemptAt:  failedAt.UTC(),
	}
}

// RecordFailedAttempt registers a further failed charge at attemptedAt. It moves
// the attempt counter and the last-attempt instant, and NOTHING else: the anchor
// stays on the first failure, so a retry can never push the suspension date
// further away. That property is what stops a dunning from being extended
// indefinitely by its own retries.
//
// Called on a state not in dunning, it opens one — a failure is a failure
// however the caller reached it.
func (s DunningState) RecordFailedAttempt(attemptedAt time.Time) DunningState {
	if !s.InDunning() {
		return OpenDunning(attemptedAt)
	}
	s.Attempts++
	s.LastAttemptAt = attemptedAt.UTC()
	return s
}

// Settle ends the dunning: the customer paid. It returns the zero state, so the
// subscription is plainly out of dunning and its next failure starts a clean
// schedule rather than inheriting a spent attempt counter.
//
// This is the recovery path the emails drive to (PRD FR6), and it works from any
// point — including from suspended, which is the whole reason suspension was
// chosen over cancellation.
func (s DunningState) Settle() DunningState {
	return DunningState{}
}

// Suspend marks access as cut, at the point the caller acted on DunningSuspend.
func (s DunningState) Suspend() DunningState {
	s.Suspended = true
	return s
}

// Cancel marks the subscription closed, at the point the caller acted on
// DunningCancel. Terminal.
func (s DunningState) Cancel() DunningState {
	s.Suspended = true
	s.Canceled = true
	return s
}

// NextDunningAction decides the ONE thing to do for state s at instant now,
// under schedule sched. A zero schedule is read as the default.
//
// It is a pure function of its arguments: calling it twice at the same instant
// with the same state gives the same answer, and it never mutates anything. The
// caller performs the effect and then folds the result back in via
// RecordFailedAttempt / Suspend / Cancel / Settle.
//
// The order of the checks is the policy:
//
//  1. Not in dunning, or already cancelled → None. Cancelled is terminal, and
//     this is the clause that makes "no charge on a cancelled subscription"
//     structurally true rather than a caller's responsibility.
//  2. Past the cancel threshold → Cancel (unless already cancelled).
//  3. Past the suspend threshold → Suspend, or None if already suspended.
//     Note that this is checked BEFORE the retry: once J+2 has passed, no retry
//     is ever proposed, whatever the attempt counter says. Suspension closes the
//     charging window.
//  4. Past the retry threshold, with attempts left, and no attempt already made
//     inside this window → Retry. The attempt cap is what bounds this: at
//     MaxAttempts, the answer is None until the suspend threshold arrives.
//  5. Otherwise → None: the next threshold is still ahead.
//
// Thresholds are inclusive of their instant, matching IsDue and Window's
// half-open boundary: at exactly J+2 the subscription is suspended, not one tick
// later. Where two thresholds land on the same instant — which normalization can
// produce from an incoherent schedule — the more terminal action wins, since the
// checks run from the end of the timeline backwards.
func NextDunningAction(s DunningState, sched DunningSchedule, now time.Time) DunningAction {
	if !s.InDunning() || s.Canceled {
		return DunningNone
	}

	sched = sched.normalized()
	now = now.UTC()
	anchor := s.FirstFailureAt.UTC()

	if reached(anchor, sched.CancelAfter, now) {
		return DunningCancel
	}

	if reached(anchor, sched.SuspendAfter, now) {
		if s.Suspended {
			return DunningNone
		}
		return DunningSuspend
	}

	// Below the suspend threshold, a suspended subscription can only be one the
	// caller suspended early, out of policy. Honour the give-up rule regardless:
	// suspended means no more charging.
	if s.Suspended {
		return DunningNone
	}

	if !reached(anchor, sched.RetryAfter, now) {
		return DunningNone
	}
	if s.Attempts >= sched.MaxAttempts {
		return DunningNone
	}
	// The retry for this window is spent once an attempt has been recorded at or
	// after the threshold. Without this, the same retry would be proposed again
	// on every pass of the trigger loop between J+1 and J+2.
	if !s.LastAttemptAt.IsZero() && reached(anchor, sched.RetryAfter, s.LastAttemptAt.UTC()) {
		return DunningNone
	}
	return DunningRetry
}

// reached reports whether t is at or past anchor+d.
func reached(anchor time.Time, d time.Duration, t time.Time) bool {
	return !t.Before(anchor.Add(d))
}

// CanCharge reports whether a charge may be attempted at all for state s. It is
// the give-up rule stated positively, for a caller that wants to check before
// building a PSP request rather than after.
//
// It is deliberately narrower than "the action is Retry": it is false for a
// suspended or cancelled subscription and false once the attempt cap is reached,
// whatever the clock says.
func CanCharge(s DunningState, sched DunningSchedule) bool {
	if !s.InDunning() {
		return true
	}
	if s.Suspended || s.Canceled {
		return false
	}
	return s.Attempts < sched.normalized().MaxAttempts
}
