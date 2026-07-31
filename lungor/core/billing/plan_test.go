package billing

import (
	"testing"
	"time"
)

// The tiers used across this package's tests, shaped like a real catalogue:
// a free tier nobody can buy, and two paid ones.
var (
	free = Tier{Name: "free", PriceCents: 0, Rank: 100}
	pro  = Tier{Name: "pro", PriceCents: 500, Rank: 2000, Billable: true}
	max_ = Tier{Name: "max", PriceCents: 1200, Rank: 10000, Billable: true}
)

// Purchasable is the single gate in front of every money path. The case that
// matters is the zero value: a tier declared but not yet wired must not become
// sellable by omission.
func TestPurchasable(t *testing.T) {
	cases := []struct {
		name string
		tier Tier
		want bool
		why  string
	}{
		{"free", free, false, "nothing to buy; it is also where unknown names land"},
		{"pro", pro, true, "wired on purpose"},
		{"max", max_, true, "wired on purpose"},
		{"zero value", Tier{}, false,
			"opt-in: a new tier stays unsellable until someone wires its price"},
		{"priced but not billable", Tier{Name: "beta", PriceCents: 900}, false,
			"a price alone never opens checkout — this is the rule that once let a tier be sold at another tier's amount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tier.Purchasable(); got != tc.want {
				t.Fatalf("Purchasable() = %v, want %v — %s", got, tc.want, tc.why)
			}
		})
	}
}

// SmallerThan orders on Rank, and Rank is the ALLOWANCE, not the price. The
// promotional case below is the whole reason: ordering on price would classify a
// cheap-but-larger tier as a downgrade and defer a change that grants MORE.
func TestSmallerThanOrdersOnAllowanceNotPrice(t *testing.T) {
	if !pro.SmallerThan(max_) {
		t.Fatal("pro must be smaller than max")
	}
	if max_.SmallerThan(pro) {
		t.Fatal("max must not be smaller than pro")
	}
	if pro.SmallerThan(pro) {
		t.Fatal("a tier is not smaller than itself — that case is ChangeNone, not a downgrade")
	}

	// A promotional tier: cheaper than pro, yet granting more than max.
	promo := Tier{Name: "promo", PriceCents: 100, Rank: 20000, Billable: true}
	if promo.SmallerThan(max_) {
		t.Fatal("a cheaper tier granting MORE is not a downgrade: " +
			"ordering on price here would defer a change that only ever adds allowance")
	}
}

// Classify is what the handler, the adapter and the UI must all agree on. One
// function so they cannot each derive it slightly differently.
func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		from, to Tier
		want     Change
	}{
		{"pro to max", pro, max_, ChangeUp},
		{"max to pro", max_, pro, ChangeDown},
		{"pro to pro", pro, pro, ChangeNone},
		{"free to pro", free, pro, ChangeUp},
		{"pro to free", pro, free, ChangeDown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.from, tc.to); got != tc.want {
				t.Fatalf("Classify(%s → %s) = %v, want %v", tc.from.Name, tc.to.Name, got, tc.want)
			}
		})
	}
}

// CanChangeTier decides who may be RE-CHARGED, so every false below is money
// not taken from someone who does not owe it.
func TestCanChangeTier(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * 24 * time.Hour)
	past := now.Add(-1 * time.Hour)

	cases := []struct {
		name      string
		status    string
		periodEnd time.Time
		want      bool
		why       string
	}{
		{"active, period running", StatusActive, future, true,
			"the only state that may move tier"},
		{"past_due", StatusPastDue, future, false,
			"they owe money on the tier they already have; re-pricing it first would settle a debt at the new rate"},
		{"canceled, period running", StatusCanceled, future, false,
			"they asked to leave: still entitled, but there is no renewal for a new price to ride on"},
		{"active, period lapsed", StatusActive, past, false,
			"a stale active status with nothing left paid must not authorise a charge"},
		{"unknown status", "whatever", future, false,
			"fail closed"},
		{"zero value", "", time.Time{}, false,
			"no subscription, nothing to change"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sub := Subscription{Status: tc.status, CurrentPeriodEnd: tc.periodEnd}
			if got := CanChangeTier(sub, now); got != tc.want {
				t.Fatalf("CanChangeTier(%s, end=%v) = %v, want %v — %s",
					tc.status, tc.periodEnd, got, tc.want, tc.why)
			}
		})
	}
}

// A subscription that may change tier must always be entitled — the converse is
// not true (a cancelled subscriber is entitled but may not move). Stated as an
// invariant because the two predicates are edited independently.
func TestCanChangeTierImpliesEntitled(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{StatusActive, StatusPastDue, StatusCanceled, "unknown"} {
		for _, end := range []time.Time{now.Add(time.Hour), now.Add(-time.Hour)} {
			sub := Subscription{Status: status, CurrentPeriodEnd: end}
			if CanChangeTier(sub, now) && !Entitled(sub, now) {
				t.Fatalf("status %q (end=%v) may change tier while not entitled: "+
					"the customer would be re-charged for access they do not have", status, end)
			}
		}
	}
}

// HasSchedule gates every tier change: there must be something to replace.
func TestHasSchedule(t *testing.T) {
	full := Subscription{ProviderCustomerID: "cst_1", ProviderScheduleID: "sub_1"}
	if !full.HasSchedule() {
		t.Fatal("a subscription with both ids has a schedule")
	}
	for _, tc := range []struct{ name, cust, sched string }{
		{"no schedule id", "cst_1", ""},
		{"no customer id", "", "sub_1"},
		{"neither", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Subscription{ProviderCustomerID: tc.cust, ProviderScheduleID: tc.sched}
			if s.HasSchedule() {
				t.Fatal("a half-identified subscription has no schedule to replace")
			}
		})
	}
}
