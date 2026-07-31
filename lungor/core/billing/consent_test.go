package billing

import "testing"

// Consent is required whenever the customer will pay MORE — today, monthly, or
// both. The under-the-floor case is the one worth pinning: nothing is debited
// now, and consent is STILL required, because the recurring price rises and
// that is the commitment.
func TestRequiresConsent(t *testing.T) {
	cases := []struct {
		name string
		kind Change
		want bool
		why  string
	}{
		{"upgrade", ChangeUp, true,
			"the recurring debit rises: that is what is being agreed to"},
		{"downgrade", ChangeDown, false,
			"they are committing to pay less; a tick to spend less protects no one"},
		{"no-op", ChangeNone, false,
			"withdrawing a scheduled downgrade restores a price already agreed to"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequiresConsent(TierChange{Kind: tc.kind}); got != tc.want {
				t.Fatalf("RequiresConsent(%v) = %v, want %v — %s", tc.kind, got, tc.want, tc.why)
			}
		})
	}
}

// ConsentFor answers WHAT must be shown. Both figures travel, because agreeing
// to a one-off top-up is not agreeing to a raised monthly debit.
func TestConsentFor(t *testing.T) {
	t.Run("carries both figures", func(t *testing.T) {
		got := ConsentFor(TierChange{Kind: ChangeUp}, max_, 350)
		if !got.Required {
			t.Fatal("an upgrade requires consent")
		}
		if got.AmountCents != 350 {
			t.Fatalf("AmountCents = %d, want 350", got.AmountCents)
		}
		if got.RecurringCents != max_.PriceCents {
			t.Fatalf("RecurringCents = %d, want %d — a customer shown only the top-up has agreed "+
				"to the smaller half of the commitment", got.RecurringCents, max_.PriceCents)
		}
	})

	// Nothing today, and consent is still required: the monthly price is what
	// changed.
	t.Run("under the floor still requires consent", func(t *testing.T) {
		got := ConsentFor(TierChange{Kind: ChangeUp}, max_, 0)
		if !got.Required || got.AmountCents != 0 || got.RecurringCents != max_.PriceCents {
			t.Fatalf("got %+v, want a required consent for 0 today and the full monthly price", got)
		}
	})

	t.Run("a downgrade asks for nothing", func(t *testing.T) {
		if got := ConsentFor(TierChange{Kind: ChangeDown}, pro, 0); got.Required {
			t.Fatalf("got %+v, want no consent required", got)
		}
	})
}

// An absent tick fails whenever consent is required, and is irrelevant when it
// is not.
func TestConsentSatisfies(t *testing.T) {
	required := ConsentFor(TierChange{Kind: ChangeUp}, max_, 350)
	if required.Satisfies(false) {
		t.Fatal("an absent tick satisfied a required consent: this is the single click that raised a recurring debit")
	}
	if !required.Satisfies(true) {
		t.Fatal("an affirmative act must satisfy consent")
	}
	if !ConsentFor(TierChange{Kind: ChangeDown}, pro, 0).Satisfies(false) {
		t.Fatal("a downgrade must not demand a tick")
	}
}

// The agreed figure is a CEILING. It may only ever lower the charge — honouring
// it in both directions would let a crafted request raise a debit.
func TestChargeableAmount(t *testing.T) {
	cases := []struct {
		name         string
		owed, agreed int64
		want         int64
		why          string
	}{
		{"agreed matches", 350, 350, 350, "the ordinary case"},
		{"agreed is lower", 350, 100, 100,
			"a stale page showed less: honour what was on screen, under-charging is recoverable"},
		{"agreed is higher", 350, 999999, 350,
			"a client-supplied figure must never raise what is owed"},
		{"no ceiling stated", 350, 0, 350,
			"an omitted field is not consent to free"},
		{"negative ceiling", 350, -500, 350,
			"a negative would be a refund the customer granted themselves"},
		{"nothing owed", 0, 350, 0,
			"no debt, no charge, whatever was agreed"},
		{"negative owed", -100, 350, 0,
			"a downgrade refunds nothing here; a negative charge would credit the customer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ChargeableAmount(tc.owed, tc.agreed); got != tc.want {
				t.Fatalf("ChargeableAmount(%d, %d) = %d, want %d — %s",
					tc.owed, tc.agreed, got, tc.want, tc.why)
			}
		})
	}
}

// The directional variants exist so no caller re-derives the comparison. That
// re-derivation, written per handler, is what let an upgrade endpoint apply a
// downgrade immediately.
func TestDirectionalPlanning(t *testing.T) {
	now := changeNow()

	t.Run("upgrade refuses a smaller tier", func(t *testing.T) {
		if _, err := PlanUpgrade(activeSub(max_), max_, pro, now); err != ErrNotAnUpgrade {
			t.Fatalf("err = %v, want ErrNotAnUpgrade — applying it would cut an allowance already paid for", err)
		}
	})

	t.Run("downgrade refuses a larger tier", func(t *testing.T) {
		if _, err := PlanDowngrade(activeSub(pro), pro, max_, now); err != ErrNotADowngrade {
			t.Fatalf("err = %v, want ErrNotADowngrade — a larger tier is the charged path and must be deliberate", err)
		}
	})

	t.Run("each allows its own direction", func(t *testing.T) {
		if _, err := PlanUpgrade(activeSub(pro), pro, max_, now); err != nil {
			t.Fatalf("a genuine upgrade was refused: %v", err)
		}
		if _, err := PlanDowngrade(activeSub(max_), max_, pro, now); err != nil {
			t.Fatalf("a genuine downgrade was refused: %v", err)
		}
	})

	// Staying put is not a direction violation: it is how a scheduled downgrade
	// is withdrawn, and neither path may refuse it.
	t.Run("both allow the held tier", func(t *testing.T) {
		if _, err := PlanUpgrade(activeSub(max_), max_, max_, now); err != nil {
			t.Fatalf("upgrade to the held tier refused: %v", err)
		}
		if _, err := PlanDowngrade(activeSub(max_), max_, max_, now); err != nil {
			t.Fatalf("downgrade to the held tier refused: %v", err)
		}
	})

	// A refusal from the underlying decision must reach the caller UNCHANGED.
	// Both variants wrap PlanTierChange, and a wrapper that flattened
	// "unpaid" into "wrong direction" would send the customer to the downgrade
	// path to fix a payment problem.
	t.Run("underlying refusals pass through", func(t *testing.T) {
		unpaid := activeSub(pro)
		unpaid.Status = StatusPastDue

		if _, err := PlanUpgrade(unpaid, pro, max_, now); err != ErrChangeNotAllowed {
			t.Fatalf("PlanUpgrade err = %v, want ErrChangeNotAllowed", err)
		}
		if _, err := PlanDowngrade(unpaid, pro, free, now); err == nil || err == ErrNotADowngrade {
			t.Fatalf("PlanDowngrade err = %v, want the underlying refusal, not a direction verdict", err)
		}
	})

	// A refusal must carry no instructions: a caller ignoring the error must not
	// find a plan telling it to move money.
	t.Run("refusals carry no instructions", func(t *testing.T) {
		got, err := PlanUpgrade(activeSub(max_), max_, pro, now)
		if err == nil {
			t.Fatal("expected a refusal")
		}
		if got.ReplaceSchedule || got.ApplyTierNow || got.RecordPendingPlan {
			t.Fatalf("a refused change returned actionable instructions: %+v", got)
		}
	})
}

// The two rules must not drift: anything requiring consent has to be a move the
// upgrade path accepts.
func TestConsentAndDirectionAgree(t *testing.T) {
	now := changeNow()
	for _, tc := range []struct{ from, to Tier }{
		{pro, max_}, {max_, pro}, {max_, max_}, {pro, pro},
	} {
		change, err := PlanTierChange(activeSub(tc.from), tc.from, tc.to, now)
		if err != nil {
			continue
		}
		if _, upErr := PlanUpgrade(activeSub(tc.from), tc.from, tc.to, now); RequiresConsent(change) && upErr != nil {
			t.Fatalf("%s→%s requires consent but is not an upgrade: the two rules disagree",
				tc.from.Name, tc.to.Name)
		}
	}
}
