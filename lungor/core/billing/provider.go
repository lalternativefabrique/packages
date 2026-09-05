package billing

import (
	"context"
	"errors"
	"time"
)

// Provider is the port onto a payment service provider: Mollie today, Lungor or
// another tomorrow. It is the seam that keeps every vendor detail out of the
// rules in this package.
//
// No provider type may appear in these signatures. Identifiers travel as opaque
// strings on Subscription (ProviderCustomerID, ProviderScheduleID), amounts as
// cents, tiers as Tier. An implementation that needs to leak its own type has
// found a gap in the port, not a reason to widen it.
//
// Two families sit behind this port and need different halves of a Tier:
//
//   - amount-driven providers (Mollie) charge PriceCents directly;
//   - catalog-driven providers (Lungor) resolve a price from their own
//     catalogue by id.
//
// This is why the whole Tier is passed rather than an amount or a price id.
// Passing only an id once forced an amount-driven adapter onto a single
// configured price, which is how a Max checkout came to charge the Pro amount.
type Provider interface {
	// Entitlement returns the owner's current subscription state.
	Entitlement(ctx context.Context, ownerID string) (Subscription, error)

	// Checkout opens the FIRST payment: it collects the first period and
	// establishes the mandate that later charges ride on. It returns a hosted
	// checkout URL to redirect the customer to.
	//
	// Returns ErrAlreadySubscribed when the owner already holds an entitling
	// subscription. That refusal is structural, not cosmetic: a second checkout
	// opens a second mandate on the same owner, and only the newest one stays
	// reachable — the older keeps charging with nothing pointing at it.
	Checkout(ctx context.Context, in CheckoutInput) (string, error)

	// Upgrade moves an entitled owner to a LARGER tier by replacing their
	// recurring schedule. The new allowance is granted immediately; the new
	// price applies from the next renewal, and the period already paid is
	// neither re-charged nor refunded.
	//
	// Returns ErrNoSubscription when there is no live schedule to replace, and
	// ErrChangeNotAllowed when the subscription may not move (unpaid, or
	// cancelled and running out).
	Upgrade(ctx context.Context, tier Tier, ownerID string) error

	// Downgrade schedules a move to a SMALLER tier, effective at the next
	// renewal, and returns when that is. Nothing changes today — see
	// Tier.SmallerThan for why the asymmetry with Upgrade is deliberate.
	//
	// Returns ErrNotADowngrade when the target is not smaller (a larger tier is
	// the immediate, charged Upgrade path; the free tier is reached by
	// cancelling, which has its own meaning and its own call).
	Downgrade(ctx context.Context, tier Tier, ownerID string) (time.Time, error)

	// CancelPendingChange withdraws a scheduled change that has not fired yet.
	// Idempotent: withdrawing when nothing is scheduled succeeds.
	CancelPendingChange(ctx context.Context, ownerID string) error

	// Cancel stops future charges. It does NOT revoke access: the period already
	// paid stays owed and keeps entitling until it lapses. Idempotent, so it is
	// safe both on the self-service route and on account deletion.
	Cancel(ctx context.Context, ownerID string) error

	// Charge takes a ONE-OFF amount on the mandate the owner already has, and
	// returns the provider's payment id.
	//
	// This is what an upgrade proration rides on, and it must never open a
	// checkout: the mandate exists from the first payment, and a second one
	// would double-bill (see Checkout).
	//
	// idempotencyKey makes a retry safe. It is required, not optional: a double
	// click, a webhook replay or a network retry must never charge twice, and
	// leaving that to each adapter is how it eventually does.
	Charge(ctx context.Context, in ChargeInput) (string, error)

	// CheckoutMethods lists how a tier may be paid for, in the order to offer
	// them. Asked rather than assumed: what a tier accepts follows from what it
	// sells, and the provider refuses at checkout what it omits here — so a
	// hardcoded list is a 400 the buyer sees after committing.
	//
	// A provider that cannot answer returns nil, which reads as "do not ask":
	// the caller checks out with no preselection, exactly as before.
	CheckoutMethods(ctx context.Context, tier Tier) ([]CheckoutMethod, error)
}

// CheckoutInput is what opening a first payment needs. Grouped into a struct
// rather than passed as six positional arguments, where the three adjacent
// strings (email, name, ownerID) could be — and were — transposed silently.
type CheckoutInput struct {
	Tier       Tier
	OwnerID    string
	Email      string
	Name       string
	SuccessURL string
	// PaymentMethod is one of the ids CheckoutMethods returned for this tier.
	// Empty leaves the choice to the provider's own selection screen, which is
	// what every caller got before the field existed.
	PaymentMethod string
}

// CheckoutMethod is one way a tier may be paid for.
type CheckoutMethod struct {
	// ID travels back as CheckoutInput.PaymentMethod.
	ID string
	// Label names the method for the buyer.
	Label string
}

// ChargeInput is a one-off charge on an existing mandate.
type ChargeInput struct {
	OwnerID     string
	AmountCents int64
	Currency    string
	// Description is what the customer reads on their statement. It must name
	// the product plainly: an unrecognised line is a chargeback.
	Description string
	// IdempotencyKey deduplicates retries of the SAME logical charge. Derive it
	// from the intent (owner, tiers, period end), never from a clock or a random
	// value — both defeat the purpose on the retry that matters.
	IdempotencyKey string
}

// SelfProrating marks a provider that collects an upgrade's proration ITSELF,
// inside Upgrade, rather than leaving the caller to charge it first.
//
// The Provider port is shaped around a PSP the caller drives: charge the
// difference, then move the tier, in that order, because the money must be
// confirmed before the allowance is granted. A billing hub that owns the
// subscription does both in one call — asking it to Charge separately would bill
// the customer twice for one upgrade.
//
// It is an optional interface rather than a method on Provider so that adding
// it breaks no existing implementation: a provider that stays silent is charged
// by the caller, which is the behaviour every adapter already has.
//
// Callers MUST consult it before charging:
//
//	if sp, ok := provider.(billing.SelfProrating); !ok || !sp.ProratesOnUpgrade() {
//	    // charge the difference first
//	}
type SelfProrating interface {
	// ProratesOnUpgrade reports whether Upgrade collects the proration. When it
	// does, the caller must NOT call Charge for the same move.
	ProratesOnUpgrade() bool
}

// Errors every Provider implementation must speak, so callers can branch on the
// customer's situation without knowing which provider answered.
//
// They are the caller's STATE, not outages: each maps to a refusal the client
// can explain, never to a retry.
var (
	// ErrNoSubscription — the owner holds no subscription, or none with a live
	// schedule. On a tier change this means "open a checkout instead", so it must
	// not read as "you may not have this tier".
	ErrNoSubscription = errors.New("billing: no subscription")
	// ErrAlreadySubscribed — the owner already holds an entitling subscription.
	// A second checkout would open a second mandate; see Checkout.
	ErrAlreadySubscribed = errors.New("billing: already subscribed")
	// ErrChangeNotAllowed — the subscription exists but may not move: unpaid
	// (it owes money on the tier it has) or cancelled (it asked to leave).
	ErrChangeNotAllowed = errors.New("billing: subscription cannot change tier")
	// ErrNotADowngrade — the requested tier is not smaller than the one held.
	ErrNotADowngrade = errors.New("billing: not a downgrade")
	// ErrNotAnUpgrade — the requested tier is smaller than the one held, on a
	// path that only moves up. The caller routes to the downgrade path, which is
	// deferred and free; applying it here would cut an allowance already paid
	// for.
	ErrNotAnUpgrade = errors.New("billing: not an upgrade")
	// ErrNotPurchasable — the tier may not be charged for. Reached by an unknown
	// tier name too, since callers resolve those to free.
	ErrNotPurchasable = errors.New("billing: tier is not purchasable")
	// ErrChargeDeclined — the provider refused the payment (card declined,
	// insufficient funds). The customer's problem to fix, and the reason a paid
	// upgrade must not be applied before it is confirmed.
	ErrChargeDeclined = errors.New("billing: charge declined")
)
