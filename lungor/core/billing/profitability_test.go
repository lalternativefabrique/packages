package billing_test

import (
	"math"
	"testing"

	"github.com/lalternative/packages/lungor/core/billing"
)

// The finding a pricing review exists to produce: a tier that earns well and
// still loses money, because serving it costs more than it bills.
func TestTierEconomics_NegativeMarginIsReported(t *testing.T) {
	tier := billing.TierEconomics{
		Tier: "max", Subscribers: 3,
		RevenueCents: 5100, // 3 × 17€
		CostCents:    5400, // GPU, and then some
	}
	if got := tier.MarginCents(); got != -300 {
		t.Fatalf("margin = %d, want -300 — a loss must not be clamped to zero", got)
	}
	if got := tier.MarginPercent(); math.Abs(got-(-5.88235294)) > 1e-6 {
		t.Errorf("margin %% = %v, want ≈ -5.88", got)
	}
}

// A tier nobody bought has no margin problem. Reporting NaN would make the
// dashboard render garbage on the most common state of a young product.
func TestTierEconomics_NoRevenueIsZeroNotNaN(t *testing.T) {
	empty := billing.TierEconomics{Tier: "solo"}
	if got := empty.MarginPercent(); got != 0 || math.IsNaN(got) {
		t.Fatalf("margin %% with no revenue = %v, want 0", got)
	}
	// Cost without revenue is still a real number: trial usage costs money
	// before anyone subscribes.
	costOnly := billing.TierEconomics{Tier: "solo", CostCents: 240}
	if got := costOnly.MarginCents(); got != -240 {
		t.Errorf("margin = %d, want -240 — cost before revenue is a loss, not zero", got)
	}
}

func TestProfitability_TotalsEveryTier(t *testing.T) {
	rep := billing.Profitability("EUR", []billing.TierEconomics{
		{Tier: "solo", Subscribers: 12, RevenueCents: 6000, CostCents: 420},
		{Tier: "pro", Subscribers: 8, RevenueCents: 7200, CostCents: 1840},
		{Tier: "max", Subscribers: 3, RevenueCents: 5100, CostCents: 4790},
	})

	if rep.Total.Subscribers != 23 {
		t.Errorf("total subscribers = %d, want 23", rep.Total.Subscribers)
	}
	if rep.Total.RevenueCents != 18300 {
		t.Errorf("total revenue = %d, want 18300", rep.Total.RevenueCents)
	}
	if rep.Total.MarginCents() != 11250 {
		t.Errorf("total margin = %d, want 11250", rep.Total.MarginCents())
	}
	// Order is the caller's: re-sorting here would override a deliberate choice
	// about how the table reads.
	if rep.Tiers[0].Tier != "solo" || rep.Tiers[2].Tier != "max" {
		t.Errorf("tier order changed: %q … %q", rep.Tiers[0].Tier, rep.Tiers[2].Tier)
	}
}

func TestProfitability_EmptyIsUsable(t *testing.T) {
	rep := billing.Profitability("EUR", nil)
	if rep.Total.Subscribers != 0 || rep.Total.MarginCents() != 0 {
		t.Fatalf("empty total = %+v, want zeroed", rep.Total)
	}
	if rep.Currency != "EUR" {
		t.Errorf("currency = %q, want EUR even with no tiers", rep.Currency)
	}
}
