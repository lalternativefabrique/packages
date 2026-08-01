package billing

import "time"

// Grant is an entitlement conferred by DECISION rather than by payment.
//
// It exists because Entitled answers a question about money — is there a live,
// paid, unlapsed subscription — and that is the wrong question for the operator
// of the product or for someone invited to help build it. They are entitled for
// a reason the billing stack cannot represent.
//
// Modelling one as a zero-priced subscription was considered and rejected: a
// subscription with no money behind it is still a subscription to the
// reconciler, whose whole job is to cancel rows the provider never confirms. It
// would revoke the grant by working correctly. A grant is therefore a SEPARATE
// record, and it never reaches a payment provider, produces no invoice, and
// carries no VAT.
type Grant struct {
	// Tier is the tier name conferred. Empty means the grant confers nothing —
	// see Active.
	Tier string
	// Reason is why the holder does not pay. It is carried for audit and is
	// deliberately NOT read by any rule here: a grant entitles the same way
	// whatever its motive, and branching on it would put authorization policy
	// inside a billing predicate.
	Reason Reason
	// GrantedBy identifies who conferred it, so a free tier is always traceable
	// to a person who decided it.
	GrantedBy string
	// ExpiresAt bounds the grant. Nil means it does not expire — the normal case
	// for an operator, whose entitlement should not lapse silently while they
	// run the product.
	ExpiresAt *time.Time
}

// Reason is the motive behind a grant.
//
// It is a FINANCIAL motive — "why this person is not charged" — and confers no
// power whatsoever. The authorization role of the same name, if any, lives in
// the application that owns accounts; a product must not read ReasonAdmin here
// and conclude someone may reach its admin routes.
type Reason string

const (
	// ReasonAdmin — the operator of the product. An operator resolving to the
	// free tier is the defect this exists to close: they hold every right over
	// the service while being metered like an anonymous visitor.
	ReasonAdmin Reason = "admin"
	// ReasonCollaborator — invited to help build the product. Entitled to a
	// working allowance, and to nothing else: a collaborator is not an operator.
	ReasonCollaborator Reason = "collaborator"
)

// Active reports whether the grant confers anything at instant t.
//
// A grant with no tier confers nothing — that is the zero value, and it must
// not entitle. Expiry is exclusive at the boundary, matching Entitled's
// treatment of CurrentPeriodEnd: the instant a period ends, it is over.
func (g Grant) Active(t time.Time) bool {
	if g.Tier == "" {
		return false
	}
	return g.ExpiresAt == nil || t.Before(*g.ExpiresAt)
}

// Resolve reports the tier a holder is entitled to, and where that entitlement
// came from, given both possible sources at instant t.
//
// The rule is MAX BY RANK, not "a grant wins". Either source may be the larger
// one, and neither may ever reduce what the other confers:
//
//   - a collaborator who later subscribes to a tier above their grant must get
//     what they paid for, not stay pinned to the granted one;
//   - a subscriber whose payment lapses must fall back to their grant, not to
//     free — they are still an operator or a collaborator.
//
// sub is the holder's subscription and hasSub reports whether they have one at
// all; grant is their grant, if any. current is the tier the subscription was
// bought at — billing does not carry a catalogue, so the caller resolves the
// name it recorded into a Tier before asking. granted is the tier the grant
// confers, resolved the same way.
//
// A subscription only counts when it Entitled: an unpaid or lapsed one confers
// nothing here, exactly as it confers nothing anywhere else.
func Resolve(sub Subscription, hasSub bool, current Tier, grant Grant, granted Tier, t time.Time) (Tier, Source) {
	subEntitles := hasSub && Entitled(sub, t)
	grantEntitles := grant.Active(t)

	switch {
	case subEntitles && grantEntitles:
		// Both confer. Ties go to the subscription: when the two are equal there
		// is nothing to gain by reporting the grant, and a paying customer's
		// entitlement should be attributed to their payment.
		if granted.SmallerThan(current) || granted.Rank == current.Rank {
			return current, SourceSubscription
		}
		return granted, SourceGrant
	case subEntitles:
		return current, SourceSubscription
	case grantEntitles:
		return granted, SourceGrant
	default:
		return Tier{}, SourceNone
	}
}

// Source names what entitled a holder, so a product can tell a paying customer
// from a granted one without re-deriving it.
//
// It is reported alongside the tier because the two answer different questions:
// the tier decides what to serve, the source decides what to SAY. Offering a
// granted collaborator an upgrade page, or warning them about a failed payment
// they never made, both follow from confusing the two.
type Source string

const (
	// SourceNone — nothing entitles. The holder falls back to whatever the
	// product serves unsubscribed users.
	SourceNone Source = "none"
	// SourceSubscription — a live, paid subscription.
	SourceSubscription Source = "subscription"
	// SourceGrant — a decision, not a payment. Nothing is owed and no payment
	// path applies.
	SourceGrant Source = "grant"
)
