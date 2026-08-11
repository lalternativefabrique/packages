package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// RegisterCustomerInput declares a user the app already has.
type RegisterCustomerInput struct {
	// Email identifies the customer to Lungor: it is the key the record is
	// stored under, so the same address twice is the same customer.
	Email string
	Name  string
	// ExternalUserID ties the record back to the app's own user. Optional, but
	// without it nothing but the email connects the two.
	ExternalUserID string
	Country        string
	// Silent suppresses the signup notification Lungor sends its operator.
	//
	// Set it when back-filling users who predate Lungor: they did not just sign
	// up, and announcing a batch of them as new both states something false and
	// buries the day's real signups. Leave it false for a genuine signup.
	Silent bool
}

// RegisterCustomerResult reports the customer as Lungor now holds it.
type RegisterCustomerResult struct {
	CustomerID string
	CreatedAt  time.Time
	// Created is false when Lungor already knew the email, so a re-run of an
	// import can report what it actually added rather than what it sent.
	Created bool
}

// RegisterCustomer makes Lungor aware of a user the app already has, with no
// subscription attached.
//
// Every other route to a customer creates a subscription alongside it: a
// checkout opens a payment session, and Grant refuses a priced plan outright.
// So an app arriving with an existing user base could only import it by putting
// everyone on a zero-priced plan, fabricating billing history nobody asked for.
// This creates the customer and nothing else.
//
// Safe to call repeatedly for the same email: Lungor keys the record on it and
// fills in blanks rather than overwriting what it already knows, so an import
// can be re-run after a partial failure. Created tells the two cases apart.
func (c *Client) RegisterCustomer(ctx context.Context, in RegisterCustomerInput) (RegisterCustomerResult, error) {
	if c.baseURL == "" || c.appKey == "" {
		return RegisterCustomerResult{}, ErrNotConfigured
	}
	if in.Email == "" {
		return RegisterCustomerResult{}, fmt.Errorf("%w: email is required", ErrBadRequest)
	}

	req := wire.FinanceAppRegisterCustomerRequest{Email: &in.Email}
	if in.Name != "" {
		req.Name = &in.Name
	}
	if in.ExternalUserID != "" {
		req.ExternalUserId = &in.ExternalUserID
	}
	if in.Country != "" {
		req.Country = &in.Country
	}
	if in.Silent {
		silent := true
		req.Silent = &silent
	}

	var body wire.FinanceAppRegisterCustomerResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppRegisterCustomer(ctx, req)
	}); err != nil {
		return RegisterCustomerResult{}, err
	}

	out := RegisterCustomerResult{}
	if body.CustomerId != nil {
		out.CustomerID = *body.CustomerId
	}
	if body.Created != nil {
		out.Created = *body.Created
	}
	if body.CreatedAt != nil {
		if t, err := time.Parse(time.RFC3339, *body.CreatedAt); err == nil {
			out.CreatedAt = t
		}
	}
	return out, nil
}
