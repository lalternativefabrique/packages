package billing

import "time"

// PlanTierChange decides what a requested move from the owner's current tier to
// `to` should DO, without doing any of it. It is the rule half of what was
// previously welded to the Mollie adapter's HTTP calls: validation,
// classification and timing were interleaved with cancel/create round-trips,
// so the policy could not be read, reused or tested on its own.
//
// The adapter keeps the money and the network. This decides:
//
//   - whether the move is allowed at all;
//   - whether the new allowance lands now or at the renewal;
//   - when the new price starts being charged.
//
// It returns an error for every refusal a caller must be able to explain, so
// the mapping from state to refusal exists once rather than at each provider.
func PlanTierChange(sub Subscription, current, to Tier, now time.Time) (TierChange, error) {
	// Purchasability first: it is the gate in front of every money path, and the
	// free tier fails it. Reaching free is a CANCELLATION — it has its own call
	// and its own meaning, and routing it here would leave a subscriber holding a
	// schedule pointed at a tier that cannot be charged.
	if !to.Purchasable() {
		return TierChange{}, ErrNotPurchasable
	}
	if !sub.HasSchedule() {
		// Nothing to replace. The caller should open a checkout instead, so this
		// must not read as "you may not have this tier".
		return TierChange{}, ErrNoSubscription
	}
	if !CanChangeTier(sub, now) {
		return TierChange{}, ErrChangeNotAllowed
	}

	switch Classify(current, to) {
	case ChangeNone:
		// Already on this tier. Replacing the schedule with an identical one would
		// churn a mandate for nothing, and erroring would be defensible and
		// useless. But a PENDING downgrade is still withdrawn: "I am on Max" and
		// "I am on Max and scheduled to leave it" are different states, and the
		// second is the one the caller is asking to leave.
		return TierChange{
			Kind:                ChangeNone,
			WithdrawPendingPlan: sub.PendingPlan != "",
		}, nil

	case ChangeUp:
		// Granting more never breaks anything, so the allowance lands now. The
		// price waits: the period already paid is not re-charged, only topped up
		// by the proration the caller collects separately.
		//
		// Any pending downgrade is withdrawn. Left in place it would fire at the
		// next renewal and silently undo the tier the customer just moved up to —
		// they would pay for the larger tier, hold it a month, and find themselves
		// back down without ever having asked twice.
		return TierChange{
			Kind:                ChangeUp,
			ApplyTierNow:        true,
			ReplaceSchedule:     true,
			ScheduleStartsAt:    sub.CurrentPeriodEnd,
			WithdrawPendingPlan: sub.PendingPlan != "",
		}, nil

	default:
		// A smaller tier. Nothing changes today, and that asymmetry with ChangeUp
		// is the point: taking away can leave a customer ABOVE the new ceiling —
		// someone who bought the large tier and already spent past the small one's
		// allowance would be refused on every request, for a month they paid for.
		// So the tier on the row is NOT touched and the renewal applies it.
		//
		// The schedule IS replaced now even though the tier is not. That is what
		// makes "the smaller price is charged from the renewal" true: leaving the
		// live schedule alone would apply the smaller allowance while still
		// charging the larger price — the worst of both, and exactly what the
		// customer asked to stop.
		if sub.PendingPlan == to.Name {
			// Already promised. Replacing the schedule again would churn the mandate
			// — and every churn is a window in which the customer has none — for a
			// state that is already correct.
			return TierChange{Kind: ChangeDown, EffectiveAt: sub.PendingPlanEffectiveAt}, nil
		}
		return TierChange{
			Kind:              ChangeDown,
			ReplaceSchedule:   true,
			ScheduleStartsAt:  sub.CurrentPeriodEnd,
			RecordPendingPlan: true,
			EffectiveAt:       sub.CurrentPeriodEnd,
		}, nil
	}
}

// PlanUpgrade is PlanTierChange restricted to moves UP. A smaller tier is
// refused with ErrNotAnUpgrade.
//
// The restriction belongs here, not at each entry point. Applying a smaller
// tier immediately is exactly what the deferred-downgrade design prevents: a
// subscriber who has already spent past the smaller allowance lands over their
// new ceiling and is refused on every request, for a period they paid for.
// Left to callers, that comparison gets written once per handler — and the one
// place it is missing is the one that charges.
func PlanUpgrade(sub Subscription, current, to Tier, now time.Time) (TierChange, error) {
	change, err := PlanTierChange(sub, current, to, now)
	if err != nil {
		return TierChange{}, err
	}
	if change.Kind == ChangeDown {
		return TierChange{}, ErrNotAnUpgrade
	}
	return change, nil
}

// PlanDowngrade is PlanTierChange restricted to moves DOWN. A larger tier is
// refused with ErrNotADowngrade.
//
// A larger tier is the immediate, CHARGED path, so silently redirecting to it
// would take money on a call the customer made in order to spend less. It has
// to be a deliberate second act.
//
// Asking for the tier already held is not refused by either variant: the
// customer's stated intent matches reality, and that no-op is also how a
// scheduled downgrade gets withdrawn.
func PlanDowngrade(sub Subscription, current, to Tier, now time.Time) (TierChange, error) {
	change, err := PlanTierChange(sub, current, to, now)
	if err != nil {
		return TierChange{}, err
	}
	if change.Kind == ChangeUp {
		return TierChange{}, ErrNotADowngrade
	}
	return change, nil
}

// TierChange is the plan of record for a tier move: what the adapter must do,
// in what order, and what to tell the customer.
//
// It is data, not behaviour, so the decision can be asserted in a table test
// with no provider in sight — which is how the timing asymmetry between an
// upgrade and a downgrade stops being folk knowledge held in one adapter.
type TierChange struct {
	// Kind is what the move turned out to be.
	Kind Change
	// ApplyTierNow grants the new allowance immediately. True only for an
	// upgrade: it is what makes "more, right away" real, and what must never
	// happen on a downgrade.
	ApplyTierNow bool
	// ReplaceSchedule means the recurring schedule must be cancelled and
	// re-created at the new amount.
	//
	// ORDER, and it is not negotiable — cancel, THEN create. The two calls are
	// separate round-trips with no shared transaction, so one of them can be the
	// last thing that succeeds, and the order decides which failure the customer
	// gets:
	//
	//   - create-then-cancel leaves TWO live schedules if the cancel fails: the
	//     customer is billed by two mandates every month, only the newest of
	//     which is reachable. Their money, and a refund to arrange.
	//   - cancel-then-create leaves NO schedule if the create fails: the customer
	//     keeps the access they paid for and simply stops renewing. Repairable,
	//     and it costs them nothing.
	//
	// The second is the lesser harm.
	ReplaceSchedule bool
	// ScheduleStartsAt is when the new schedule takes its first charge: the end
	// of the period already paid. Never today — that would bill a period twice.
	ScheduleStartsAt time.Time
	// RecordPendingPlan stores the deferred tier, written LAST, once the schedule
	// that will honour it exists. A promise recorded against a schedule that
	// failed to be created is a change nothing can apply.
	RecordPendingPlan bool
	// WithdrawPendingPlan clears a scheduled downgrade the move supersedes.
	WithdrawPendingPlan bool
	// EffectiveAt is when the customer sees the change, for a deferred one. Zero
	// for an immediate change: it already happened.
	//
	// Reported because someone who is not told WHEN assumes it already happened,
	// and is surprised twice — once now when their allowance is untouched, once
	// at the renewal when it is not.
	EffectiveAt time.Time
}
