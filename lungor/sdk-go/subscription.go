package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// ChangeDirection constrains a tier move.
//
// Naming the intended direction turns a mispriced catalogue into a refusal
// rather than a silent move the wrong way: a caller meaning to upgrade would
// otherwise downgrade the customer and charge nothing.
type ChangeDirection string

const (
	// DirectionAny accepts whichever direction the target tier implies.
	DirectionAny ChangeDirection = ""
	// DirectionUp refuses the change unless the target is larger.
	DirectionUp ChangeDirection = "up"
	// DirectionDown refuses the change unless the target is smaller.
	DirectionDown ChangeDirection = "down"
)

// ChangePlanInput moves one of the app's users to another tier.
type ChangePlanInput struct {
	ExternalUserID string
	// PlanCode names the target in the app's own catalogue.
	PlanCode  string
	Direction ChangeDirection
	// Agreed carries the customer's acceptance of an upgrade's immediate charge,
	// with the amount they were shown. Both are required: an agreement to a
	// figure the customer never saw is not one.
	Agreed            bool
	AgreedAmountCents int64
}

// ChangePlanResult reports what the move amounted to.
type ChangePlanResult struct {
	// Kind is what Lungor classified the move as (upgrade, downgrade, none).
	Kind string
	// AppliedNow is true for an upgrade, which lands immediately. A downgrade
	// waits for the renewal, and EffectiveAt says when.
	AppliedNow    bool
	EffectiveAt   time.Time
	ProratedCents int64
	// ConsentRequired means nothing changed: the customer must agree to the
	// figures below, which the caller resends via Agreed/AgreedAmountCents.
	ConsentRequired  bool
	ConsentAmount    int64
	ConsentRecurring int64
}

// ErrConsentRequired is returned when an upgrade needs the customer's explicit
// agreement to its immediate charge.
//
// A distinct error, not a plain failure: the request was well-formed and the
// move is allowed. The figures to show come back on the result, so the caller
// does not re-derive an amount that must match what Lungor will charge.
var ErrConsentRequired = fmt.Errorf("lungor: customer consent required")

// ChangePlan moves the app's user to another tier.
//
// An upgrade applies immediately and is prorated; a downgrade takes effect at
// the next renewal. Returns ErrConsentRequired — with the result still
// populated — when the customer must agree first.
func (c *Client) ChangePlan(ctx context.Context, in ChangePlanInput) (ChangePlanResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return ChangePlanResult{}, ErrNotConfigured
	}
	if in.ExternalUserID == "" || in.PlanCode == "" {
		return ChangePlanResult{}, fmt.Errorf("%w: external user id and plan code are required", ErrBadRequest)
	}

	req := wire.FinanceAppChangePlanRequest{
		ExternalUserId: &in.ExternalUserID,
		PlanCode:       &in.PlanCode,
	}
	if in.Direction != DirectionAny {
		d := string(in.Direction)
		req.Direction = &d
	}
	if in.Agreed {
		agreed := true
		amount := int(in.AgreedAmountCents)
		req.Agreed, req.AgreedAmountCents = &agreed, &amount
	}

	var body wire.FinanceAppChangePlanResponse
	status, err := c.sendStatus(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppChangePlan(ctx, req)
	})
	if err != nil {
		return ChangePlanResult{}, err
	}
	out := changePlanFrom(body)
	if status == http.StatusPaymentRequired {
		return out, ErrConsentRequired
	}
	return out, nil
}

// GrantInput puts one of the app's users on a plan that costs nothing.
type GrantInput struct {
	ExternalUserID string
	// PlanCode names a plan priced at zero in the app's catalogue. A priced one
	// is refused with ErrBadRequest — that is what a checkout is for.
	PlanCode string
	Email    string
	Country  string
}

// GrantResult reports the subscription that was created.
type GrantResult struct {
	SubscriptionID string
	PlanCode       string
	// PeriodEnd is when the granted allowance renews.
	PeriodEnd time.Time
}

// Grant subscribes the app's user to a free plan, with no checkout.
//
// It is the only way onto a free tier. A checkout needs a PSP credential to
// open the payment session it creates alongside the subscription, which a plan
// charging nothing does not have; ChangePlan moves an existing subscription and
// will not invent one. So a free, collaborator or admin tier could be declared
// in the catalogue and never assigned to anyone.
//
// The subscription is active on return — there is no payment to wait for.
//
// Returns ErrConflict when the user already holds one: moving between plans is
// ChangePlan's job, and granting a second would leave two live allowances for
// the same customer.
func (c *Client) Grant(ctx context.Context, in GrantInput) (GrantResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return GrantResult{}, ErrNotConfigured
	}
	if in.ExternalUserID == "" || in.PlanCode == "" || in.Email == "" {
		return GrantResult{}, fmt.Errorf("%w: external user id, plan code and email are required", ErrBadRequest)
	}

	req := wire.FinanceAppGrantRequest{
		ExternalUserId: &in.ExternalUserID,
		PlanCode:       &in.PlanCode,
		Email:          &in.Email,
	}
	if in.Country != "" {
		req.Country = &in.Country
	}

	var body wire.FinanceAppGrantResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppGrantSubscription(ctx, req)
	}); err != nil {
		return GrantResult{}, err
	}

	out := GrantResult{}
	if body.SubscriptionId != nil {
		out.SubscriptionID = *body.SubscriptionId
	}
	if body.PlanCode != nil {
		out.PlanCode = *body.PlanCode
	}
	if body.PeriodEnd != nil {
		if t, err := time.Parse(time.RFC3339, *body.PeriodEnd); err == nil {
			out.PeriodEnd = t
		}
	}
	return out, nil
}

func changePlanFrom(w wire.FinanceAppChangePlanResponse) ChangePlanResult {
	out := ChangePlanResult{}
	if w.Kind != nil {
		out.Kind = *w.Kind
	}
	if w.AppliedNow != nil {
		out.AppliedNow = *w.AppliedNow
	}
	if w.ProratedCents != nil {
		out.ProratedCents = int64(*w.ProratedCents)
	}
	if w.ConsentRequired != nil {
		out.ConsentRequired = *w.ConsentRequired
	}
	if w.ConsentAmountCents != nil {
		out.ConsentAmount = int64(*w.ConsentAmountCents)
	}
	if w.ConsentRecurringCents != nil {
		out.ConsentRecurring = int64(*w.ConsentRecurringCents)
	}
	if w.EffectiveAt != nil {
		if t, err := time.Parse(time.RFC3339, *w.EffectiveAt); err == nil {
			out.EffectiveAt = t
		}
	}
	return out
}

// CancelResult reports the state the subscription was left in.
type CancelResult struct {
	Status string
	// EffectiveAt is when access actually stops, and is zero for an immediate
	// cancellation.
	EffectiveAt time.Time
}

// Cancel ends the app user's subscription.
//
// atPeriodEnd true keeps the period already paid for; false stops it now.
// Cancelling at period end does NOT revoke access — Entitlement keeps reporting
// the user as entitled until that date, which is what the customer bought.
func (c *Client) Cancel(ctx context.Context, externalUserID string, atPeriodEnd bool) (CancelResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return CancelResult{}, ErrNotConfigured
	}
	if externalUserID == "" {
		return CancelResult{}, fmt.Errorf("%w: empty external user id", ErrBadRequest)
	}

	req := wire.FinanceAppCancelRequest{ExternalUserId: &externalUserID, AtPeriodEnd: &atPeriodEnd}
	var body wire.FinanceAppCancelResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppCancelSubscription(ctx, req)
	}); err != nil {
		return CancelResult{}, err
	}

	out := CancelResult{}
	if body.Status != nil {
		out.Status = *body.Status
	}
	if body.EffectiveAt != nil {
		if t, err := time.Parse(time.RFC3339, *body.EffectiveAt); err == nil {
			out.EffectiveAt = t
		}
	}
	return out, nil
}

// WithdrawPendingPlan cancels a tier change scheduled for the next renewal.
//
// Reports whether anything was actually withdrawn, and the tier it would have
// moved to. Withdrawing when none is scheduled is not an error: a customer
// clicking twice must not be told off for a promise that is already gone.
func (c *Client) WithdrawPendingPlan(ctx context.Context, externalUserID string) (withdrawn bool, planCode string, err error) {
	if c.baseURL == "" || c.appKey == "" {
		return false, "", ErrNotConfigured
	}
	if externalUserID == "" {
		return false, "", fmt.Errorf("%w: empty external user id", ErrBadRequest)
	}

	req := wire.FinanceAppWithdrawPendingRequest{ExternalUserId: &externalUserID}
	var body wire.FinanceAppWithdrawPendingResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppWithdrawPendingPlan(ctx, req)
	}); err != nil {
		return false, "", err
	}
	if body.Withdrawn != nil {
		withdrawn = *body.Withdrawn
	}
	if body.PlanCode != nil {
		planCode = *body.PlanCode
	}
	return withdrawn, planCode, nil
}
