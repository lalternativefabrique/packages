package billing

import (
	"errors"
	"testing"
	"time"
)

func changeNow() time.Time { return time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC) }

// activeSub is a healthy subscriber on `tier` with a live schedule and a month
// left to run.
func activeSub(tier Tier) Subscription {
	return Subscription{
		OwnerID:            "own_1",
		Status:             StatusActive,
		CurrentPeriodEnd:   changeNow().Add(15 * 24 * time.Hour),
		Plan:               tier.Name,
		ProviderCustomerID: "cst_1",
		ProviderScheduleID: "sub_1",
	}
}

// An upgrade grants NOW and charges LATER. Both halves matter: granting late
// would sell something the customer cannot use, and charging now would bill a
// period that is already paid.
func TestPlanTierChangeUpgrade(t *testing.T) {
	sub := activeSub(pro)
	got, err := PlanTierChange(sub, pro, max_, changeNow())
	if err != nil {
		t.Fatalf("upgrade refused: %v", err)
	}
	if got.Kind != ChangeUp {
		t.Fatalf("Kind = %v, want ChangeUp", got.Kind)
	}
	if !got.ApplyTierNow {
		t.Fatal("an upgrade must grant the allowance immediately — that is what the customer just paid a proration for")
	}
	if !got.ReplaceSchedule {
		t.Fatal("the recurring amount must move to the new tier")
	}
	if !got.ScheduleStartsAt.Equal(sub.CurrentPeriodEnd) {
		t.Fatalf("ScheduleStartsAt = %v, want the period end %v — charging earlier bills a paid period twice",
			got.ScheduleStartsAt, sub.CurrentPeriodEnd)
	}
	if got.RecordPendingPlan {
		t.Fatal("an upgrade is immediate: there is nothing pending about it")
	}
}

// A downgrade changes NOTHING today. This is the asymmetry with an upgrade, and
// the reason is the customer who already spent past the smaller ceiling.
func TestPlanTierChangeDowngrade(t *testing.T) {
	sub := activeSub(max_)
	got, err := PlanTierChange(sub, max_, pro, changeNow())
	if err != nil {
		t.Fatalf("downgrade refused: %v", err)
	}
	if got.Kind != ChangeDown {
		t.Fatalf("Kind = %v, want ChangeDown", got.Kind)
	}
	if got.ApplyTierNow {
		t.Fatal("a downgrade must NOT apply today: a customer who already spent past the smaller " +
			"allowance would be refused on every request, for a month they paid for")
	}
	if !got.ReplaceSchedule {
		t.Fatal("the schedule must move to the smaller amount now, or the customer keeps paying " +
			"the larger price for the smaller tier they scheduled")
	}
	if !got.RecordPendingPlan {
		t.Fatal("the deferred tier must be recorded, or the renewal has nothing to apply")
	}
	if !got.EffectiveAt.Equal(sub.CurrentPeriodEnd) {
		t.Fatalf("EffectiveAt = %v, want the period end %v — a customer not told WHEN assumes it already happened",
			got.EffectiveAt, sub.CurrentPeriodEnd)
	}
}

// Re-requesting a downgrade already scheduled is a no-op, not a second churn.
// Every schedule replacement is a window in which the customer has none.
func TestPlanTierChangeDowngradeIsIdempotent(t *testing.T) {
	sub := activeSub(max_)
	sub.PendingPlan = pro.Name
	sub.PendingPlanEffectiveAt = sub.CurrentPeriodEnd

	got, err := PlanTierChange(sub, max_, pro, changeNow())
	if err != nil {
		t.Fatalf("repeat downgrade refused: %v", err)
	}
	if got.ReplaceSchedule || got.RecordPendingPlan {
		t.Fatal("the promise is already recorded: replacing the mandate again churns it for a state that is already correct")
	}
	if !got.EffectiveAt.Equal(sub.PendingPlanEffectiveAt) {
		t.Fatal("the original effective date must be reported, not recomputed")
	}
}

// Asking for the tier already held is a no-op — EXCEPT that it withdraws a
// pending downgrade. "I am on Max" and "I am on Max but leaving it" are
// different states, and the second is what the caller is asking to leave.
func TestPlanTierChangeSameTier(t *testing.T) {
	t.Run("nothing pending", func(t *testing.T) {
		got, err := PlanTierChange(activeSub(max_), max_, max_, changeNow())
		if err != nil {
			t.Fatalf("same-tier request refused: %v", err)
		}
		if got.Kind != ChangeNone || got.ReplaceSchedule || got.WithdrawPendingPlan {
			t.Fatal("a request matching reality must do nothing at all")
		}
	})

	t.Run("withdraws a pending downgrade", func(t *testing.T) {
		sub := activeSub(max_)
		sub.PendingPlan = pro.Name
		got, err := PlanTierChange(sub, max_, max_, changeNow())
		if err != nil {
			t.Fatalf("re-upgrade to the held tier refused: %v", err)
		}
		if !got.WithdrawPendingPlan {
			t.Fatal("the customer is asking to stay on the tier they are scheduled to leave: the schedule must be withdrawn")
		}
	})
}

// An upgrade supersedes a pending downgrade. Left in place it would fire at the
// renewal and undo the tier the customer just moved up to.
func TestUpgradeWithdrawsPendingDowngrade(t *testing.T) {
	sub := activeSub(pro)
	sub.PendingPlan = free.Name

	got, err := PlanTierChange(sub, pro, max_, changeNow())
	if err != nil {
		t.Fatalf("upgrade refused: %v", err)
	}
	if !got.WithdrawPendingPlan {
		t.Fatal("a pending downgrade left in place would silently undo the upgrade at the next renewal — " +
			"the customer would pay for the larger tier, hold it a month, and drop back without asking twice")
	}
}

// Every refusal, and what each one protects.
func TestPlanTierChangeRefusals(t *testing.T) {
	now := changeNow()

	cases := []struct {
		name    string
		sub     Subscription
		current Tier
		to      Tier
		want    error
		why     string
	}{
		{
			name: "free tier", sub: activeSub(pro), current: pro, to: free,
			want: ErrNotPurchasable,
			why:  "reaching free is a cancellation; it has its own call, and a schedule pointed at it could never be charged",
		},
		{
			name: "unwired tier", sub: activeSub(pro), current: pro,
			to:   Tier{Name: "beta", PriceCents: 900, Rank: 5000},
			want: ErrNotPurchasable,
			why:  "a price alone does not make a tier sellable",
		},
		{
			name: "no schedule", sub: Subscription{Status: StatusActive, CurrentPeriodEnd: now.Add(time.Hour)},
			current: pro, to: max_,
			want: ErrNoSubscription,
			why:  "there is nothing to replace: the caller should open a checkout",
		},
		{
			name: "past_due", sub: func() Subscription { s := activeSub(pro); s.Status = StatusPastDue; return s }(),
			current: pro, to: max_,
			want: ErrChangeNotAllowed,
			why:  "they owe money on the tier they have; re-pricing would settle the debt at the new rate",
		},
		{
			name: "canceled", sub: func() Subscription { s := activeSub(pro); s.Status = StatusCanceled; return s }(),
			current: pro, to: max_,
			want: ErrChangeNotAllowed,
			why:  "they asked to leave: there is no renewal left for a new price to ride on",
		},
		{
			name: "lapsed period", sub: func() Subscription {
				s := activeSub(pro)
				s.CurrentPeriodEnd = now.Add(-time.Hour)
				return s
			}(),
			current: pro, to: max_,
			want: ErrChangeNotAllowed,
			why:  "a stale active status with nothing left paid must not authorise a charge",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PlanTierChange(tc.sub, tc.current, tc.to, now)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v — %s", err, tc.want, tc.why)
			}
		})
	}
}

// A refused change must never carry instructions: a caller that ignores the
// error must not find a plan telling it to move money.
func TestRefusalsCarryNoInstructions(t *testing.T) {
	sub := activeSub(pro)
	sub.Status = StatusPastDue

	got, err := PlanTierChange(sub, pro, max_, changeNow())
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if got.ApplyTierNow || got.ReplaceSchedule || got.RecordPendingPlan || got.WithdrawPendingPlan {
		t.Fatalf("a refused change returned actionable instructions: %+v", got)
	}
}
