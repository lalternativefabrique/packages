package billing

// TierEconomics is one tier's month: what it earned, what it cost to serve, and
// what is left.
//
// The question it answers is whether a price holds. Revenue alone cannot say:
// two tiers earning the same can cost wildly different amounts to serve, and a
// tier only reveals itself as underpriced once enough subscribers settle on it
// to make the change expensive.
type TierEconomics struct {
	Tier        string `json:"tier"`
	Subscribers int    `json:"subscribers"`

	// RevenueCents is what the tier bills per period, before costs. Minor units,
	// single currency: mixing currencies in one figure would be meaningless, and
	// a caller with several should report several.
	RevenueCents int64 `json:"revenue_cents"`

	// CostCents is what serving those subscribers cost. Supplied by the caller,
	// because only the application knows what its own service costs — a GPU
	// second, an API call, a stored gigabyte. Zero is honest for a tier whose
	// cost is not measured rather than a claim that it is free.
	CostCents int64 `json:"cost_cents"`
}

// MarginCents is what the tier leaves after the cost of serving it. Negative is
// a real answer, and the one worth surfacing: a tier that loses money on every
// subscriber gets worse as it succeeds.
func (t TierEconomics) MarginCents() int64 {
	return t.RevenueCents - t.CostCents
}

// MarginPercent is the margin as a share of revenue. Zero when nothing was
// earned, rather than undefined — a tier with no subscribers has no margin
// problem, and a dashboard rendering NaN teaches nobody anything.
func (t TierEconomics) MarginPercent() float64 {
	if t.RevenueCents == 0 {
		return 0
	}
	return float64(t.MarginCents()) / float64(t.RevenueCents) * 100
}

// ProfitabilityReport is every tier plus the total, for one period and one
// currency.
type ProfitabilityReport struct {
	Currency string          `json:"currency"`
	Tiers    []TierEconomics `json:"tiers"`
	Total    TierEconomics   `json:"total"`
}

// Profitability totals the tiers and labels the aggregate.
//
// Tiers are reported in the order given: the caller knows whether that should
// be by price, by name, or by subscriber count, and re-sorting here would
// silently override a deliberate choice.
func Profitability(currency string, tiers []TierEconomics) ProfitabilityReport {
	rep := ProfitabilityReport{
		Currency: currency,
		Tiers:    tiers,
		Total:    TierEconomics{Tier: "total"},
	}
	for _, t := range tiers {
		rep.Total.Subscribers += t.Subscribers
		rep.Total.RevenueCents += t.RevenueCents
		rep.Total.CostCents += t.CostCents
	}
	return rep
}
