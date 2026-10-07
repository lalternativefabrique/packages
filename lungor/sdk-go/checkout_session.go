package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// CheckoutStatus is where a checkout session stands.
//
// The provider's redirect says nothing about the outcome — Mollie sends the
// payer back to the same URL paid or refused — so the return page reads the
// session instead. Two statuses are still in flight and the page keeps asking;
// the four others are final.
type CheckoutStatus string

const (
	// CheckoutPending — opened, the payer has not reached the provider yet.
	CheckoutPending CheckoutStatus = "pending"
	// CheckoutRedirected — the payer is on the provider's page, or has just
	// left it and the provider's notification is on its way.
	CheckoutRedirected CheckoutStatus = "redirected"
	// CheckoutCompleted — the provider settled the payment.
	CheckoutCompleted CheckoutStatus = "completed"
	// CheckoutFailed — the provider refused the payment. FailureReason says why,
	// in the provider's words; offer a new checkout.
	CheckoutFailed CheckoutStatus = "failed"
	// CheckoutCanceled — the payer gave up on the provider's page.
	CheckoutCanceled CheckoutStatus = "canceled"
	// CheckoutExpired — nobody acted before the session's window closed.
	CheckoutExpired CheckoutStatus = "expired"
)

// Final reports whether the status can still change. A page polling the
// session stops here.
func (s CheckoutStatus) Final() bool {
	switch s {
	case CheckoutCompleted, CheckoutFailed, CheckoutCanceled, CheckoutExpired:
		return true
	}
	return false
}

// CheckoutSession is what Lungor reports about a checkout the app opened.
type CheckoutSession struct {
	SessionID string
	Status    CheckoutStatus
	// Paid is the one field to open access on: true once the provider settled
	// the payment. For a recurring plan the subscription is active too.
	Paid bool
	// FailureReason is the provider's own word for a refusal
	// (insufficient_funds, invalid_card_number…). Set on Failed only.
	FailureReason      string
	SubscriptionID     string
	SubscriptionStatus string
	PlanID             string
	ExternalUserID     string
	BuyerKind          BuyerKind
	ExpiresAt          time.Time
	CreatedAt          time.Time
}

// SessionIDParam is the query parameter Lungor stamps on both return URLs at
// checkout, carrying the session id. The page the provider returns to reads it
// and asks CheckoutSession how things ended.
const SessionIDParam = "lungor_session_id"

// CheckoutSession reads how a checkout ended, computed from Lungor's database
// at call time.
//
// It is the fallback when the subscription webhook is late or missed: call it
// on the return page and open access on Paid rather than on the redirect
// alone. Keep asking while Status is not Final — the provider's notification
// can land a few seconds after the payer does.
//
// ErrNotFound covers a session of another app as well as one that does not
// exist; the two are indistinguishable on purpose.
func (c *Client) CheckoutSession(ctx context.Context, sessionID string) (CheckoutSession, error) {
	if c.baseURL == "" || c.appKey == "" {
		return CheckoutSession{}, ErrNotConfigured
	}
	if sessionID == "" {
		return CheckoutSession{}, fmt.Errorf("%w: empty session id", ErrBadRequest)
	}

	var body wire.FinanceCheckoutSessionResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.GetCheckoutSession(ctx, sessionID)
	}); err != nil {
		return CheckoutSession{}, err
	}
	return checkoutSessionFrom(body), nil
}

func checkoutSessionFrom(w wire.FinanceCheckoutSessionResponse) CheckoutSession {
	out := CheckoutSession{
		SessionID:          deref(w.SessionId),
		Status:             CheckoutStatus(deref(w.Status)),
		FailureReason:      deref(w.FailureReason),
		SubscriptionID:     deref(w.SubscriptionId),
		SubscriptionStatus: deref(w.SubscriptionStatus),
		PlanID:             deref(w.PlanId),
		ExternalUserID:     deref(w.ExternalUserId),
	}
	if w.BuyerKind != nil {
		out.BuyerKind = BuyerKind(*w.BuyerKind)
	}
	if w.Paid != nil {
		out.Paid = *w.Paid
	}
	if w.ExpiresAt != nil {
		out.ExpiresAt, _ = time.Parse(time.RFC3339, *w.ExpiresAt)
	}
	if w.CreatedAt != nil {
		out.CreatedAt, _ = time.Parse(time.RFC3339, *w.CreatedAt)
	}
	return out
}
