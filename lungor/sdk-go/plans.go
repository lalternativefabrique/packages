package sdk

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// Plan is one purchasable entry of an app's catalogue.
//
// It is what a pricing page renders and what a checkout is opened against, so
// the price shown and the price charged come from the same read. Restating
// either in the caller's own configuration is what this type exists to stop: a
// price stated twice eventually disagrees with itself, and it does so at the
// moment a customer is charged.
type Plan struct {
	// ID is the value Checkout takes as PriceID.
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
	// Amount is in minor units of Currency (2900 = 29.00 EUR). Never a float: a
	// cent lost to binary rounding is a cent that fails an audit.
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	// Interval and IntervalCount give the billing cadence ("month", 1).
	Interval      string `json:"interval"`
	IntervalCount int    `json:"interval_count"`
	// Rank orders the plans against each other: higher means larger. It is what
	// separates an upgrade from a downgrade, and it is a product statement
	// rather than a function of Amount — a promotional tier priced below a
	// smaller one would otherwise invert the two paths.
	Rank int `json:"rank"`
	// Allocations is what the plan includes per billing period, per metered
	// unit. Empty when the plan caps nothing.
	//
	// Read THIS rather than restating the allowance in configuration: the same
	// figures are what the ledger enforces, so a local copy is a second
	// authority that disagrees the first time one side is edited.
	Allocations []Allocation `json:"allocations,omitempty"`
}

// Allocation is one metered unit's allowance on a plan, per billing period.
//
// It is a CEILING, not a wallet: nothing is credited at the start of a period
// and nothing carries over at the end.
type Allocation struct {
	Unit   string `json:"unit"`
	Amount int64  `json:"amount"`
}

// Allowance returns what the plan includes of a unit, and whether it governs it
// at all.
//
// A missing unit is not zero — zero is a real ceiling that includes none of the
// unit, while missing means the plan does not cap it. A caller that conflates
// them refuses work the plan never limited.
func (p Plan) Allowance(unit string) (int64, bool) {
	for _, a := range p.Allocations {
		if a.Unit == unit {
			return a.Amount, true
		}
	}
	return 0, false
}

// Plans is an app's catalogue, ordered by rank.
type Plans []Plan

// ByCode returns the plan with that code, and whether the catalogue holds one.
//
// A missing code is NOT an error here: a tier a product knows about may simply
// not be on sale yet. The caller decides whether that is fatal — Checkout does,
// since there is nothing to charge against.
func (p Plans) ByCode(code string) (Plan, bool) {
	for _, plan := range p {
		if plan.Code == code {
			return plan, true
		}
	}
	return Plan{}, false
}

// ListPlans returns the catalogue of the app the API key belongs to.
//
// Use it to resolve the price a checkout opens against, instead of carrying the
// id in configuration. Inactive plans are already withheld by Lungor:
// deactivating a plan is how a tenant withdraws it from sale, so a caller that
// could still read one would keep offering something no longer sold.
func (c *Client) ListPlans(ctx context.Context) (Plans, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}
	var body wire.FinancePlansResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.ListAppPlans(ctx)
	}); err != nil {
		return nil, err
	}
	return plansFrom(body), nil
}

// ListPublicPlans returns an app's catalogue without a credential.
//
// It is the read a PRICING PAGE makes, from a visitor's browser, before anyone
// has signed up — which is why the app is named rather than authenticated. It
// serves the same rows as ListPlans; a page and a checkout that disagreed would
// show one price and charge another.
func (c *Client) ListPublicPlans(ctx context.Context, appID string) (Plans, error) {
	if c.baseURL == "" {
		return nil, ErrNotConfigured
	}
	if appID == "" {
		return nil, fmt.Errorf("%w: empty app id", ErrBadRequest)
	}
	var body wire.FinancePlansResponse
	if err := c.send(ctx, &body, func() (*http.Response, error) {
		return c.wire.ListPublicPlans(ctx, appID)
	}); err != nil {
		return nil, err
	}
	return plansFrom(body), nil
}

// plansFrom converts the generated wire type into the one callers use.
//
// swag emits OpenAPI 2.0, which has no `required`, so every generated field is
// a pointer. Those pointers stop at this boundary: a caller reading a price
// must not have to distinguish "zero" from "absent" on every field of every
// plan.
func plansFrom(w wire.FinancePlansResponse) Plans {
	if w.Plans == nil {
		return Plans{}
	}
	out := make(Plans, 0, len(*w.Plans))
	for _, p := range *w.Plans {
		out = append(out, Plan{
			ID:            deref(p.Id),
			Code:          deref(p.Code),
			Name:          deref(p.Name),
			Amount:        int64(derefInt(p.Amount)),
			Currency:      deref(p.Currency),
			Interval:      deref(p.Interval),
			IntervalCount: derefInt(p.IntervalCount),
			Rank:          derefInt(p.Rank),
			Allocations:   allocationsFrom(p.Allocations),
		})
	}
	return out
}

// allocationsFrom keeps nil as nil: a plan Lungor reported no allowance for
// governs nothing, which an empty slice would render the same way as a plan
// that caps every unit at zero.
func allocationsFrom(w *[]wire.FinancePlanAllocationAmount) []Allocation {
	if w == nil || len(*w) == 0 {
		return nil
	}
	out := make([]Allocation, 0, len(*w))
	for _, a := range *w {
		out = append(out, Allocation{Unit: deref(a.Unit), Amount: int64(derefInt(a.Amount))})
	}
	return out
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
