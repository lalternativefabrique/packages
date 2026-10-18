package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// Addon is one quantity add-on the app sells and what a user holds of it.
// Each unit of Quantity raises the user's allowance of Unit by one, on top of
// the plan's allocation.
type Addon struct {
	Code          string
	Name          string
	Unit          string
	Quantity      int64
	AmountCents   int64
	Currency      string
	Interval      string
	IntervalCount int
}

var (
	// ErrAddonQuantity — the quantity is negative (422 quantity).
	ErrAddonQuantity = fmt.Errorf("%w: invalid addon quantity", ErrUnprocessable)
	// ErrAddonPlanRequired — the user holds no plan the add-on allows (409 plan_required).
	ErrAddonPlanRequired = fmt.Errorf("%w: plan required for this addon", ErrConflict)
	// ErrAddonNotFound — the app sells no add-on by that code (404 addon).
	ErrAddonNotFound = fmt.Errorf("%w: no such addon", ErrNotFound)
	// ErrPaymentRequired — an increase needs a saved payment method the user
	// lacks (402 payment_required). PaymentRequiredError carries the checkout
	// URL when Lungor returns one.
	ErrPaymentRequired = fmt.Errorf("lungor: payment required")
)

// PaymentRequiredError is returned with ErrPaymentRequired.
type PaymentRequiredError struct{ CheckoutURL string }

func (e *PaymentRequiredError) Error() string { return ErrPaymentRequired.Error() }

func (e *PaymentRequiredError) Unwrap() error { return ErrPaymentRequired }

// Addons lists every add-on the app sells, with what the user holds (0 when
// never bought).
func (c *Client) Addons(ctx context.Context, externalUserID string) ([]Addon, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}
	if externalUserID == "" {
		return nil, fmt.Errorf("%w: external user id is required", ErrBadRequest)
	}
	var body wire.FinanceAddonsResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.AppListAddons(ctx, &wire.AppListAddonsParams{ExternalUserId: externalUserID})
	}); err != nil {
		return nil, err
	}
	var out []Addon
	if body.Addons != nil {
		for _, a := range *body.Addons {
			out = append(out, Addon{
				Code: deref(a.Code), Name: deref(a.Name), Unit: deref(a.Unit),
				Quantity: int64(derefInt(a.Quantity)), AmountCents: int64(derefInt(a.AmountCents)),
				Currency: deref(a.Currency), Interval: deref(a.Interval), IntervalCount: derefInt(a.IntervalCount),
			})
		}
	}
	return out, nil
}

// SetAddonQuantity sets how many units of an add-on the user holds and returns
// the quantity now held. An increase is charged by Lungor, prorated over the
// paid period; a decrease refunds nothing. Lowering below what the app has
// handed out is the app's own check to make first.
func (c *Client) SetAddonQuantity(ctx context.Context, externalUserID, code string, quantity int64) (int64, error) {
	if c.baseURL == "" || c.appKey == "" {
		return 0, ErrNotConfigured
	}
	if externalUserID == "" || code == "" {
		return 0, fmt.Errorf("%w: external user id and addon code are required", ErrBadRequest)
	}
	if c.wire == nil {
		return 0, ErrNotConfigured
	}
	q := int(quantity)
	resp, err := c.wire.AppSetAddonQuantity(ctx, code, wire.FinanceSetAddonQuantityRequest{
		ExternalUserId: &externalUserID, Quantity: &q,
	})
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out wire.FinanceSetAddonQuantityResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return 0, fmt.Errorf("%w: decoding response: %v", ErrUnavailable, err)
		}
		return int64(derefInt(out.Quantity)), nil
	case http.StatusPaymentRequired, http.StatusConflict, http.StatusNotFound, http.StatusUnprocessableEntity:
		return 0, addonError(resp.StatusCode, resp.Body)
	}
	return 0, mapStatus(resp)
}

func addonError(status int, body io.Reader) error {
	var e struct {
		Error       string `json:"error"`
		CheckoutURL string `json:"checkout_url"`
	}
	_ = json.NewDecoder(io.LimitReader(body, 4096)).Decode(&e)
	switch {
	case status == http.StatusPaymentRequired:
		return &PaymentRequiredError{CheckoutURL: e.CheckoutURL}
	case status == http.StatusConflict:
		return ErrAddonPlanRequired
	case status == http.StatusUnprocessableEntity:
		return ErrAddonQuantity
	case status == http.StatusNotFound && e.Error == "addon":
		return ErrAddonNotFound
	}
	return fmt.Errorf("%w: status %d", ErrNotFound, status)
}

func mapStatus(resp *http.Response) error {
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, snippet(resp.Body))
	}
	return fmt.Errorf("%w: unexpected status %d", ErrUnavailable, resp.StatusCode)
}
