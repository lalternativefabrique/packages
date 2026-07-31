package billing

// Tier is a sellable subscription level, reduced to what BILLING needs to know
// about it: what it is called, what it costs, and whether it may be charged.
//
// It is deliberately NOT the full plan of any one product. Techtuel's plan also
// carries a credit allocation, a rate cap and per-job tariffs; those are
// metering concerns, they differ per product, and Lungor has no use for them.
// What every product's billing shares is this triple — and mixing the two is
// what kept the tier algebra locked inside transcript-api.
//
// A product keeps its own richer plan type and projects it onto a Tier at the
// billing boundary.
type Tier struct {
	// Name identifies the tier, and is what gets recorded on the subscription
	// so the tier billed and the tier provisioned are the same one.
	Name string
	// PriceCents is the advertised monthly price, in cents of the account
	// currency.
	//
	// It belongs to the tier, never to deployment configuration: a catalogue
	// price is a product decision, and the pricing page and the amount actually
	// charged must not be able to disagree. Techtuel learned this the hard way —
	// while the Pro price came from an env var and Max's from a constant, a
	// customer could be shown one amount and charged another.
	PriceCents int64
	// Rank orders tiers against each other. Higher means larger.
	//
	// Explicit rather than derived from PriceCents, because "which tier is
	// bigger" is a product statement and price is only its usual proxy: a
	// promotional tier priced below a smaller one would silently invert the
	// upgrade and downgrade paths.
	Rank int
	// Billable marks a tier the billing stack can actually charge for. Opt-IN,
	// and the zero value is deliberately false.
	//
	// The permissive rule ("anything not free is purchasable") is what let a tier
	// become checkoutable the day it was listed, before anything could charge its
	// price — customers clicked the Max price and were charged the Pro one. A
	// tier whose price the stack cannot express must stay false until someone
	// wires it on purpose.
	Billable bool
}

// Purchasable reports whether checkout, upgrade and downgrade may target this
// tier. It is the single gate in front of every money path.
//
// The free tier is never purchasable, so an unknown tier name — which callers
// resolve to free — is refused by the same rule rather than by a separate
// check.
func (t Tier) Purchasable() bool { return t.Billable }

// SmallerThan reports whether t grants less than other.
//
// This is what separates the two tier-change paths, and they are asymmetric on
// purpose: granting more never breaks anything, so an upgrade lands
// immediately, while taking away can leave a customer ABOVE the new ceiling —
// someone who bought the large tier and already spent past the small one's
// allowance would be refused on every request for a month they paid for. So a
// downgrade waits for the renewal.
func (t Tier) SmallerThan(other Tier) bool { return t.Rank < other.Rank }

// Change is what a requested move from one tier to another turns out to be.
type Change int

const (
	// ChangeNone — the customer already holds the requested tier. A no-op, not
	// an error: their stated intent already matches reality.
	ChangeNone Change = iota
	// ChangeUp — a larger tier. Immediate, and charged (see Prorate).
	ChangeUp
	// ChangeDown — a smaller tier. Scheduled for the next renewal, never applied
	// today.
	ChangeDown
)

// Classify reports what moving from `from` to `to` amounts to.
//
// It exists so the decision is made in ONE place, on tier ranks, rather than
// re-derived from prices or names at each call site. The handler, the adapter
// and the UI must agree on what an upgrade is; before this, each compared
// something slightly different.
func Classify(from, to Tier) Change {
	switch {
	case to.Rank == from.Rank:
		return ChangeNone
	case to.Rank > from.Rank:
		return ChangeUp
	default:
		return ChangeDown
	}
}
