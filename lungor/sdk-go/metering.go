package sdk

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// Usage is one metered consumption to record.
type Usage struct {
	// ExternalUserID is who consumed, in the caller's own id space.
	ExternalUserID string
	// Unit is the usage unit code, as declared for the app.
	Unit string
	// Quantity is how much of it. Integer, never a float: a fraction of a unit
	// lost to rounding is a unit somebody is not billed for.
	Quantity int64
	// IdempotencyKey deduplicates retries of the SAME consumption. Derive it
	// from the act being metered — a job id, a message id — never from a clock
	// or a random value, both of which defeat the purpose on the retry that
	// matters.
	IdempotencyKey string
	// SubscriptionID scopes the usage to a subscription when the app tracks
	// several. Optional.
	SubscriptionID string
}

// Decision is what Lungor answered about a consumption.
//
// Allowed is the verdict, and it is a normal answer either way: a refusal means
// the user hit their cap, not that anything failed. Read THIS rather than
// branching on an error — Consume returns no error for a refusal, precisely so
// a cap cannot be mistaken for an outage and retried.
type Decision struct {
	Allowed bool `json:"allowed"`
	// Reason names why a refusal happened, for display. Empty when allowed.
	Reason string `json:"reason,omitempty"`
	// Balance is what remains after this call.
	Balance int64 `json:"balance"`
	// Recorded reports whether the ledger was written. False on a refusal, and
	// false on a duplicate the idempotency key caught.
	Recorded bool `json:"recorded"`
}

// Consume records a consumption and reports whether it was allowed.
//
// A REFUSAL IS NOT AN ERROR. Lungor answers 402 when the user is at their cap,
// which is a state the caller must show them — not a failure to retry. The
// error return is reserved for what actually went wrong: a rejected key, a
// malformed request, Lungor being unreachable.
//
//	d, err := c.Consume(ctx, usage)
//	if err != nil { /* outage or caller bug */ }
//	if !d.Allowed { /* show the cap; do NOT retry */ }
func (c *Client) Consume(ctx context.Context, in Usage) (Decision, error) {
	body, err := in.request()
	if err != nil {
		return Decision{}, err
	}
	if c.baseURL == "" || c.appKey == "" {
		return Decision{}, ErrNotConfigured
	}
	var out wire.MeteringDecisionResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.ConsumeUsage(ctx, body)
	}); err != nil {
		return Decision{}, err
	}
	return decisionFrom(out), nil
}

// Topup credits a user's prepaid balance for a unit.
//
// Quantity is what is ADDED, not the balance to reach: a top-up is an event, so
// two identical calls credit twice unless they carry the same idempotency key.
func (c *Client) Topup(ctx context.Context, in Usage) (Decision, error) {
	body, err := in.request()
	if err != nil {
		return Decision{}, err
	}
	if c.baseURL == "" || c.appKey == "" {
		return Decision{}, ErrNotConfigured
	}
	var out wire.MeteringDecisionResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.TopupUsage(ctx, body)
	}); err != nil {
		return Decision{}, err
	}
	return decisionFrom(out), nil
}

// Balance reports what a user has left of a unit.
func (c *Client) Balance(ctx context.Context, externalUserID, unit string) (int64, error) {
	if c.baseURL == "" || c.appKey == "" {
		return 0, ErrNotConfigured
	}
	if externalUserID == "" || unit == "" {
		return 0, fmt.Errorf("%w: external user id and unit are required", ErrBadRequest)
	}
	var out wire.MeteringBalanceResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetUsageBalance(ctx, &wire.GetUsageBalanceParams{
			ExternalUserId: externalUserID,
			Unit:           unit,
		})
	}); err != nil {
		return 0, err
	}
	if out.Balance == nil {
		return 0, nil
	}
	return int64(*out.Balance), nil
}

// request validates a usage and builds the generated wire body.
//
// The idempotency key is required rather than optional: metering is the one
// path where a retry that double-counts costs the customer money, and leaving
// the key to each caller's judgement is how one of them omits it.
func (u Usage) request() (wire.MeteringConsumeRequest, error) {
	switch {
	case u.ExternalUserID == "":
		return wire.MeteringConsumeRequest{}, fmt.Errorf("%w: empty external user id", ErrBadRequest)
	case u.Unit == "":
		return wire.MeteringConsumeRequest{}, fmt.Errorf("%w: empty unit", ErrBadRequest)
	case u.IdempotencyKey == "":
		return wire.MeteringConsumeRequest{}, fmt.Errorf("%w: an idempotency key is required, or a retry double-counts", ErrBadRequest)
	}
	q := int(u.Quantity)
	return wire.MeteringConsumeRequest{
		ExternalUserId: &u.ExternalUserID,
		Unit:           &u.Unit,
		Quantity:       &q,
		IdempotencyKey: &u.IdempotencyKey,
		SubscriptionId: &u.SubscriptionID,
	}, nil
}

func decisionFrom(w wire.MeteringDecisionResponse) Decision {
	d := Decision{}
	if w.Allowed != nil {
		d.Allowed = *w.Allowed
	}
	if w.Reason != nil {
		d.Reason = *w.Reason
	}
	if w.Balance != nil {
		d.Balance = int64(*w.Balance)
	}
	if w.Recorded != nil {
		d.Recorded = *w.Recorded
	}
	return d
}
