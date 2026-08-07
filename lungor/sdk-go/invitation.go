package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// ClaimInput redeems an invitation for a user who has just signed up.
type ClaimInput struct {
	// Token is the single-use value carried by the invitation link, which
	// Lungor built on the app's own registration URL.
	Token string
	// ExternalUserID is the account the granted tier lands on, in the app's own
	// id space — the same one every other call here uses.
	ExternalUserID string
	// Country sets the tax jurisdiction of the subscription being created.
	Country string
}

// ClaimResult reports the tier the invitation conferred.
type ClaimResult struct {
	PlanCode string
	// PeriodEnd is when the granted allowance renews.
	PeriodEnd time.Time
}

// ClaimInvitation redeems an invitation token, subscribing the invitee to the
// tier they were offered.
//
// The token is single-use and scoped to one app: Lungor checks it was issued
// for the app whose key signs this call, so holding a valid key of its own does
// not let an app spend an offer made for another.
//
// The three refusals are worth telling apart, and each maps to its own error —
// only ErrInvitationExpired and ErrConflict say the offer was real, which is
// what tells an invitee that asking for a new link is worth it rather than
// doubting the address they were invited at:
//
//   - ErrNotFound: no such token, or it belongs to another app.
//   - ErrConflict: already claimed.
//   - ErrInvitationExpired: the link lapsed.
func (c *Client) ClaimInvitation(ctx context.Context, in ClaimInput) (ClaimResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return ClaimResult{}, ErrNotConfigured
	}
	if in.Token == "" || in.ExternalUserID == "" {
		return ClaimResult{}, fmt.Errorf("%w: token and external user id are required", ErrBadRequest)
	}

	req := wire.FinanceClaimReq{
		Token:          &in.Token,
		ExternalUserId: &in.ExternalUserID,
	}
	if in.Country != "" {
		req.Country = &in.Country
	}

	var body wire.FinanceClaimResp
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.ClaimInvitation(ctx, req)
	}); err != nil {
		return ClaimResult{}, err
	}

	out := ClaimResult{}
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
