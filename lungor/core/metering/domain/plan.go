package domain

// Unlimited marks a plan allocation that is never capped. A ceiling this high is
// unreachable for any realistic monthly usage, so an unlimited plan needs no
// special case on the consumption path — the debit is still recorded, keeping
// the ledger a complete journal.
const Unlimited int64 = 1_000_000_000

// Allocation is a plan's allowance of one unit for one billing period: `Amount`
// units of `Unit` that may be consumed between one renewal date and the next.
// Amount == Unlimited means the unit is effectively uncapped.
//
// It is a CEILING, not a wallet. Nothing is credited at the start of a period
// and nothing is left over at the end of one: the allowance is compared against
// what was consumed inside the window, so unused units expire and a spent period
// never weighs on the next.
type Allocation struct {
	Unit   string
	Amount int64
}

// Unlimited reports whether this allocation is uncapped.
func (a Allocation) IsUnlimited() bool { return a.Amount >= Unlimited }
