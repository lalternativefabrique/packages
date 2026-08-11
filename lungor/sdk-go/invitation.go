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

// InvitationStatus is where an offer stands. Only Claimed and Expired say it
// was real, which is what tells an invitee that asking for a new link is worth
// it rather than doubting the address they were invited at.
type InvitationStatus string

const (
	InvitationClaimable InvitationStatus = "claimable"
	InvitationClaimed   InvitationStatus = "claimed"
	InvitationExpired   InvitationStatus = "expired"
)

// Invitation is what a token holder may be told about their own offer, before
// they sign up: the address it was issued to, and the tier it confers.
type Invitation struct {
	Email    string
	PlanCode string
	Status   InvitationStatus
	// ExpiresAt is zero when Lungor reported none.
	ExpiresAt time.Time
}

// Claimable reports whether the offer still stands.
func (i Invitation) Claimable() bool { return i.Status == InvitationClaimable }

// LookupInvitation reads an invitation WITHOUT consuming it, so an app can
// greet the invitee and pre-fill the form before they sign up.
//
// ClaimInvitation is what burns the token; this never does. That distinction is
// the whole point: an app that had to claim in order to learn the invited
// address would spend the offer on a visitor who then closed the tab.
//
// A token belonging to another app answers ErrNotFound, exactly as an unknown
// one does — an app holds a valid key of its own, so telling the two apart is
// what would let it probe offers made for another.
//
// A claimed or expired invitation is NOT an error: it answers with the status
// saying so. Only a token that does not exist is ErrNotFound.
func (c *Client) LookupInvitation(ctx context.Context, token string) (Invitation, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Invitation{}, ErrNotConfigured
	}
	if token == "" {
		return Invitation{}, fmt.Errorf("%w: token is required", ErrBadRequest)
	}

	var body wire.FinanceLookupResp
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.LookupInvitation(ctx, token)
	}); err != nil {
		return Invitation{}, err
	}
	return invitationFrom(body), nil
}

// invitationFrom converts the generated wire type into the one callers use.
//
// An absent status reads as expired rather than claimable: the generated
// pointers make "not said" indistinguishable from "empty", and defaulting to
// claimable would offer a tier on an answer Lungor never gave.
func invitationFrom(w wire.FinanceLookupResp) Invitation {
	out := Invitation{Status: InvitationExpired}
	if w.Email != nil {
		out.Email = *w.Email
	}
	if w.PlanCode != nil {
		out.PlanCode = *w.PlanCode
	}
	if w.Status != nil && *w.Status != "" {
		out.Status = InvitationStatus(*w.Status)
	}
	if w.ExpiresAt != nil {
		if t, err := time.Parse(time.RFC3339, *w.ExpiresAt); err == nil {
			out.ExpiresAt = t
		}
	}
	return out
}
