package billing

import (
	"testing"
	"time"
)

// The instant every case in this file is evaluated at. The tiers themselves
// (free / pro / max_) are the package-wide catalogue declared in plan_test.go.
var now = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func active(end time.Time) Subscription {
	return Subscription{Status: StatusActive, CurrentPeriodEnd: end}
}

func TestGrantActive_ZeroValueConfersNothing(t *testing.T) {
	if (Grant{}).Active(now) {
		t.Fatal("the zero grant must not entitle: a record with no tier confers nothing")
	}
}

func TestGrantActive_NilExpiryNeverLapses(t *testing.T) {
	g := Grant{Tier: "pro", Reason: ReasonAdmin}
	if !g.Active(now.AddDate(10, 0, 0)) {
		t.Fatal("a grant with no expiry must still entitle ten years on")
	}
}

func TestGrantActive_ExpiryIsExclusiveAtTheBoundary(t *testing.T) {
	exp := now
	g := Grant{Tier: "pro", ExpiresAt: &exp}
	if g.Active(now) {
		t.Fatal("the instant a grant expires it is over, as with CurrentPeriodEnd")
	}
	if !g.Active(now.Add(-time.Second)) {
		t.Fatal("a second before expiry the grant still entitles")
	}
}

// The defect this whole file exists to close: an operator with no subscription
// resolved to the free tier, metered like an anonymous visitor.
func TestResolve_GrantEntitlesWithoutAnySubscription(t *testing.T) {
	g := Grant{Tier: "max", Reason: ReasonAdmin}

	tier, src := Resolve(Subscription{}, false, free, g, max_, now)

	if tier.Name != "max" {
		t.Fatalf("granted tier = %q, want max", tier.Name)
	}
	if src != SourceGrant {
		t.Fatalf("source = %q, want grant", src)
	}
}

func TestResolve_NothingEntitlesWithoutSubscriptionOrGrant(t *testing.T) {
	tier, src := Resolve(Subscription{}, false, free, Grant{}, free, now)

	if src != SourceNone {
		t.Fatalf("source = %q, want none", src)
	}
	if tier.Name != "" {
		t.Fatalf("tier = %q, want the zero tier", tier.Name)
	}
}

// A collaborator granted Pro who then pays for Max must receive Max. Were the
// grant to win outright, they would be charged for a tier they never got.
func TestResolve_PaidTierAboveTheGrantWins(t *testing.T) {
	g := Grant{Tier: "pro", Reason: ReasonCollaborator}

	tier, src := Resolve(active(now.AddDate(0, 1, 0)), true, max_, g, pro, now)

	if tier.Name != "max" {
		t.Fatalf("tier = %q, want max: paying above a grant must not be capped by it", tier.Name)
	}
	if src != SourceSubscription {
		t.Fatalf("source = %q, want subscription", src)
	}
}

// The mirror case: a grant above the paid tier must not be reduced to it.
func TestResolve_GrantAboveThePaidTierWins(t *testing.T) {
	g := Grant{Tier: "max", Reason: ReasonAdmin}

	tier, src := Resolve(active(now.AddDate(0, 1, 0)), true, pro, g, max_, now)

	if tier.Name != "max" {
		t.Fatalf("tier = %q, want max", tier.Name)
	}
	if src != SourceGrant {
		t.Fatalf("source = %q, want grant", src)
	}
}

// A lapsed subscriber who is also a collaborator falls back to the GRANT, never
// to free. Degrading them to free would punish a payment problem by removing an
// entitlement that has nothing to do with payment.
func TestResolve_LapsedSubscriptionFallsBackToTheGrant(t *testing.T) {
	lapsed := Subscription{Status: StatusActive, CurrentPeriodEnd: now.Add(-time.Hour)}
	g := Grant{Tier: "pro", Reason: ReasonCollaborator}

	tier, src := Resolve(lapsed, true, max_, g, pro, now)

	if tier.Name != "pro" {
		t.Fatalf("tier = %q, want pro", tier.Name)
	}
	if src != SourceGrant {
		t.Fatalf("source = %q, want grant", src)
	}
}

// past_due never entitles — the money did not arrive — so the grant carries the
// holder alone.
func TestResolve_PastDueLeavesOnlyTheGrant(t *testing.T) {
	pastDue := Subscription{Status: StatusPastDue, CurrentPeriodEnd: now.AddDate(0, 1, 0)}
	g := Grant{Tier: "pro", Reason: ReasonCollaborator}

	tier, src := Resolve(pastDue, true, max_, g, pro, now)

	if tier.Name != "pro" || src != SourceGrant {
		t.Fatalf("tier = %q source = %q, want pro/grant", tier.Name, src)
	}
}

// An expired grant stops conferring, leaving the subscription to answer alone.
func TestResolve_ExpiredGrantLeavesTheSubscription(t *testing.T) {
	exp := now.Add(-time.Hour)
	g := Grant{Tier: "max", Reason: ReasonCollaborator, ExpiresAt: &exp}

	tier, src := Resolve(active(now.AddDate(0, 1, 0)), true, pro, g, max_, now)

	if tier.Name != "pro" {
		t.Fatalf("tier = %q, want pro: an expired grant confers nothing", tier.Name)
	}
	if src != SourceSubscription {
		t.Fatalf("source = %q, want subscription", src)
	}
}

// Equal ranks attribute the entitlement to the payment: there is nothing to
// gain by reporting a grant that grants no more.
func TestResolve_TieGoesToTheSubscription(t *testing.T) {
	g := Grant{Tier: "pro", Reason: ReasonCollaborator}

	_, src := Resolve(active(now.AddDate(0, 1, 0)), true, pro, g, pro, now)

	if src != SourceSubscription {
		t.Fatalf("source = %q, want subscription on a tie", src)
	}
}

// Reason must not change what a grant confers: it is audit metadata, and
// branching on it here would put authorization policy inside billing.
func TestResolve_ReasonDoesNotChangeTheOutcome(t *testing.T) {
	admin := Grant{Tier: "pro", Reason: ReasonAdmin}
	collab := Grant{Tier: "pro", Reason: ReasonCollaborator}

	a, aSrc := Resolve(Subscription{}, false, free, admin, pro, now)
	c, cSrc := Resolve(Subscription{}, false, free, collab, pro, now)

	if a.Name != c.Name || aSrc != cSrc {
		t.Fatalf("admin (%s/%s) and collaborator (%s/%s) must resolve identically",
			a.Name, aSrc, c.Name, cSrc)
	}
}
