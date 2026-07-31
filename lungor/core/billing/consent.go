package billing

// RequiresConsent reports whether a tier change may only proceed against an
// explicit, affirmative act by the customer.
//
// The rule, in one line: consent is required whenever the customer will pay
// MORE — today, monthly, or both.
//
//   - An upgrade always requires it. Even when the proration falls under the
//     collection floor and nothing is debited today, the RECURRING price rises,
//     and that is the commitment being made.
//   - A downgrade does not. Nothing is charged, the tier in force does not move,
//     and the customer is committing to pay less — demanding a tick to spend
//     less is friction protecting no one.
//   - A no-op does not either: withdrawing a scheduled downgrade restores a
//     price the customer already agreed to, at no extra charge.
//
// This is billing algebra, not transport: it decides when money may be taken,
// it is the same rule for every product built on this package, and left in an
// HTTP handler the next caller re-invents it — or omits it, which is how a
// single unconfirmed click came to raise a recurring debit.
func RequiresConsent(change TierChange) bool {
	return change.Kind == ChangeUp
}

// Consent is what the customer must have been shown, and agreed to, before a
// change may be carried out.
//
// RequiresConsent answers WHETHER; this answers WHAT. Both halves are needed
// because agreeing to a one-off top-up is not agreeing to a raised monthly
// debit: a customer shown only the first figure has consented to the smaller
// part of the commitment. The wording, the checkbox and the language stay with
// the product; the figures that must appear in them are decided here.
type Consent struct {
	// Required mirrors RequiresConsent, carried on the value so a caller that
	// already built one does not have to ask twice.
	Required bool
	// AmountCents is what will be debited immediately. Legitimately zero when
	// the change takes no payment today — which still requires consent whenever
	// the recurring price goes up.
	AmountCents int64
	// RecurringCents is the price charged from the next renewal onward.
	RecurringCents int64
}

// ConsentFor builds the consent a change calls for.
//
// chargeNow is what will actually be taken immediately — the caller has it from
// Prorate, and passing it in keeps this free of period arithmetic it has no
// reason to repeat.
func ConsentFor(change TierChange, to Tier, chargeNow int64) Consent {
	if !RequiresConsent(change) {
		return Consent{}
	}
	return Consent{
		Required:       true,
		AmountCents:    chargeNow,
		RecurringCents: to.PriceCents,
	}
}

// Satisfies reports whether the customer's affirmative act covers this consent.
// agreed is that act; false is an absent tick, and it fails whenever consent is
// required at all.
func (c Consent) Satisfies(agreed bool) bool {
	if !c.Required {
		return true
	}
	return agreed
}

// ChargeableAmount is what may actually be taken, given the figure the customer
// was shown.
//
// It is the min of the two, never negative. The asymmetry is deliberate: an
// agreed figure is a CEILING, not a price the caller sets. Honouring it in both
// directions would let a crafted request raise someone's debit, while honouring
// it downward only means a stale page under-charges us — recoverable, where
// exceeding consent is not.
//
// agreedCents <= 0 means no ceiling was stated (an omitted field is not consent
// to free), and the owed amount stands.
func ChargeableAmount(owedCents, agreedCents int64) int64 {
	if owedCents <= 0 {
		return 0
	}
	if agreedCents > 0 && agreedCents < owedCents {
		return agreedCents
	}
	return owedCents
}
