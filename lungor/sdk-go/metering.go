package sdk

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// Usage is one metered consumption to record.
//
// It carries no window and no limit, and that is not an omission: WHICH CEILING
// applies is resolved by Lungor from the user's subscription. Stating it here
// would give the cap a second source of truth, and the two would eventually
// disagree — in the direction that serves someone more than they bought.
//
// The two regimes, because they behave differently and the difference matters:
//
//   - SUBSCRIPTION CAP — when the user's plan allocates this unit, the debit is
//     capped on the allowance for the CURRENT BILLING PERIOD, anchored on the
//     subscription's own renewal date rather than the 1st of the month.
//     Consumption outside that window does not count, so a user who exhausted
//     last period is served again as soon as it rolls over — no top-up, no
//     manual intervention.
//   - PREPAID WALLET — a user with no such allocation falls back to a lifetime
//     balance, topped up out of band with Topup.
//
// Read Decision.Balance for what is left under whichever regime applies.
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
// The check and the write happen in ONE serialized transaction server-side, so
// two concurrent debits cannot both observe the pre-debit total and both pass.
// That guarantee is why this is a single call and not a Balance-then-Consume
// pair: reading first leaves a window in which another debit lands.
//
// See Usage for which ceiling the debit is checked against.
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

// Topup credits a user's PREPAID balance for a unit — the lifetime wallet
// consumed when the user's plan allocates no allowance for it.
//
// It does not raise a subscription's per-period allowance: that comes from the
// plan, and topping up would let a cap be lifted without anyone changing what
// was sold.
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

// Balance is what a user has left of a unit, and the period it was measured
// over.
//
// It is computed on the same basis Consume decides on. A read that reported a
// lifetime total while debits were capped per period would show a number no
// refusal ever matches.
type Balance struct {
	Unit string
	// Remaining is what is left. Never negative: an allowance lowered
	// mid-period can leave Consumed above Limit, and reporting a debt the user
	// does not owe helps nobody — the raw figures below keep the overrun
	// visible.
	Remaining int64
	// Limit and Consumed describe the period Remaining was computed over, so a
	// caller renders "95 of 3000 left" without a second call. Both are zero
	// under the prepaid regime, where there is neither.
	Limit    int64
	Consumed int64
	// Periodic says which regime answered. It matters for what you tell the
	// user: "0 left, back at renewal" and "0 in the wallet, top it up" are the
	// same number meaning opposite things.
	Periodic bool
}

// Balance reports what a user has left of a unit.
//
// The number means whichever regime applies to them: what remains IN THE
// CURRENT BILLING PERIOD when their plan allocates the unit, or their lifetime
// prepaid balance otherwise. See Usage.
//
// Do not gate a consumption on this. Reading then debiting leaves a window in
// which another debit lands, and two callers can both pass a check neither
// would pass together — use Consume, whose check and write are one transaction.
func (c *Client) Balance(ctx context.Context, externalUserID, unit string) (Balance, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Balance{}, ErrNotConfigured
	}
	if externalUserID == "" || unit == "" {
		return Balance{}, fmt.Errorf("%w: external user id and unit are required", ErrBadRequest)
	}
	var out wire.MeteringBalanceResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetUsageBalance(ctx, &wire.GetUsageBalanceParams{
			ExternalUserId: externalUserID,
			Unit:           unit,
		})
	}); err != nil {
		return Balance{}, err
	}
	return balanceFrom(unit, out), nil
}

// LLMCost is what one end user's LLM consumption has cost the tenant over a
// period. It is internal expense accounting: never billed to the user, never
// on an invoice — Lungor reports it so the app can show it to the user anyway.
type LLMCost struct {
	// CostMicros is micro-cents (1 EUR = 1_000_000).
	CostMicros  int64
	Currency    string
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// MyLLMCost reports what externalUserID's LLM consumption has cost the tenant
// over [from, to). Both bounds are optional; omitted, Lungor defaults to the
// current calendar month (UTC) — the same reset window Balance's Periodic
// regime uses.
//
// ErrNotFound means externalUserID has never resolved to a customer — no
// first call recorded yet, not a zero-cost customer. Do not read it as
// "spent nothing": show nothing, or fall back the way a fresh Balance would.
func (c *Client) MyLLMCost(ctx context.Context, externalUserID string, from, to *time.Time) (LLMCost, error) {
	if c.baseURL == "" || c.appKey == "" {
		return LLMCost{}, ErrNotConfigured
	}
	if externalUserID == "" {
		return LLMCost{}, fmt.Errorf("%w: empty external user id", ErrBadRequest)
	}
	params := &wire.GetMyLLMCostReportParams{ExternalUserId: externalUserID}
	if from != nil {
		s := from.UTC().Format(time.RFC3339)
		params.From = &s
	}
	if to != nil {
		s := to.UTC().Format(time.RFC3339)
		params.To = &s
	}
	var out wire.CosttrackingMyCostReportResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetMyLLMCostReport(ctx, params)
	}); err != nil {
		return LLMCost{}, err
	}
	return llmCostFrom(out), nil
}

// AppModelCost is one (provider, model)'s share of an app's LLM spend.
type AppModelCost struct {
	Provider   string
	Model      string
	CostMicros int64
	Tokens     int64
	Calls      int
}

// AppLLMCostSummary is what the calling app's own LLM consumption has cost
// the tenant over a period, broken down by (provider, model) — the app-key
// counterpart of the tenant-wide report, which requires a console JWT.
type AppLLMCostSummary struct {
	// CostMicros is micro-cents (1 EUR = 1_000_000).
	CostMicros  int64
	Currency    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	ByModel     []AppModelCost
}

// AppLLMCostSummary reports what the calling app's LLM consumption has cost
// the tenant over [from, to), across every one of its end users, broken down
// by (provider, model). Both bounds are optional; omitted, Lungor defaults to
// the current calendar month (UTC).
//
// Unlike MyLLMCost, there is no per-app ErrNotFound: an app with no usage yet
// gets a zero summary, since the app itself is what the API key identifies.
func (c *Client) AppLLMCostSummary(ctx context.Context, from, to *time.Time) (AppLLMCostSummary, error) {
	if c.baseURL == "" || c.appKey == "" {
		return AppLLMCostSummary{}, ErrNotConfigured
	}
	params := &wire.GetAppLLMCostSummaryParams{}
	if from != nil {
		s := from.UTC().Format(time.RFC3339)
		params.From = &s
	}
	if to != nil {
		s := to.UTC().Format(time.RFC3339)
		params.To = &s
	}
	var out wire.CosttrackingCostSummaryResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetAppLLMCostSummary(ctx, params)
	}); err != nil {
		return AppLLMCostSummary{}, err
	}
	return appLLMCostSummaryFrom(out), nil
}

func appLLMCostSummaryFrom(w wire.CosttrackingCostSummaryResponse) AppLLMCostSummary {
	s := AppLLMCostSummary{Currency: "EUR"}
	if w.CostMicros != nil {
		s.CostMicros = int64(*w.CostMicros)
	}
	if w.Currency != nil {
		s.Currency = *w.Currency
	}
	if w.PeriodStart != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodStart); err == nil {
			s.PeriodStart = t
		}
	}
	if w.PeriodEnd != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodEnd); err == nil {
			s.PeriodEnd = t
		}
	}
	if w.ByModel != nil {
		s.ByModel = make([]AppModelCost, 0, len(*w.ByModel))
		for _, m := range *w.ByModel {
			mc := AppModelCost{}
			if m.Provider != nil {
				mc.Provider = *m.Provider
			}
			if m.Model != nil {
				mc.Model = *m.Model
			}
			if m.CostMicros != nil {
				mc.CostMicros = int64(*m.CostMicros)
			}
			if m.Tokens != nil {
				mc.Tokens = int64(*m.Tokens)
			}
			if m.Calls != nil {
				mc.Calls = *m.Calls
			}
			s.ByModel = append(s.ByModel, mc)
		}
	}
	return s
}

// AppCustomerModelCost is one (provider, model)'s share of a customer's
// spend within the calling app.
type AppCustomerModelCost struct {
	Provider   string
	Model      string
	CostMicros int64
	Tokens     int64
	Calls      int
}

// AppCustomerCost is one of the calling app's end users, and what that user
// cost the tenant over the period.
type AppCustomerCost struct {
	CustomerID    string
	CustomerName  string
	CustomerEmail string
	CostMicros    int64
	Tokens        int64
	Calls         int
	ByModel       []AppCustomerModelCost
}

// AppLLMCostByCustomer is AppLLMCostSummary broken down by end user instead
// of by (provider, model) — the app-key counterpart of the tenant-wide
// per-customer report, which requires a console JWT.
type AppLLMCostByCustomer struct {
	// CostMicros is micro-cents (1 EUR = 1_000_000).
	CostMicros  int64
	Currency    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Customers   []AppCustomerCost
}

// AppLLMCostByCustomer reports what each of the calling app's end users has
// cost the tenant over [from, to). Both bounds are optional; omitted, Lungor
// defaults to the current calendar month (UTC). See AppLLMCostSummary for
// the (provider, model) breakdown of the same spend.
func (c *Client) AppLLMCostByCustomer(ctx context.Context, from, to *time.Time) (AppLLMCostByCustomer, error) {
	if c.baseURL == "" || c.appKey == "" {
		return AppLLMCostByCustomer{}, ErrNotConfigured
	}
	params := &wire.GetAppLLMCostSummaryByCustomerParams{}
	if from != nil {
		s := from.UTC().Format(time.RFC3339)
		params.From = &s
	}
	if to != nil {
		s := to.UTC().Format(time.RFC3339)
		params.To = &s
	}
	var out wire.CosttrackingCostSummaryByCustomerResponse
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetAppLLMCostSummaryByCustomer(ctx, params)
	}); err != nil {
		return AppLLMCostByCustomer{}, err
	}
	return appLLMCostByCustomerFrom(out), nil
}

func appLLMCostByCustomerFrom(w wire.CosttrackingCostSummaryByCustomerResponse) AppLLMCostByCustomer {
	s := AppLLMCostByCustomer{Currency: "EUR"}
	if w.CostMicros != nil {
		s.CostMicros = int64(*w.CostMicros)
	}
	if w.Currency != nil {
		s.Currency = *w.Currency
	}
	if w.PeriodStart != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodStart); err == nil {
			s.PeriodStart = t
		}
	}
	if w.PeriodEnd != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodEnd); err == nil {
			s.PeriodEnd = t
		}
	}
	if w.Customers != nil {
		s.Customers = make([]AppCustomerCost, 0, len(*w.Customers))
		for _, cu := range *w.Customers {
			ac := AppCustomerCost{}
			if cu.CustomerId != nil {
				ac.CustomerID = *cu.CustomerId
			}
			if cu.CustomerName != nil {
				ac.CustomerName = *cu.CustomerName
			}
			if cu.CustomerEmail != nil {
				ac.CustomerEmail = *cu.CustomerEmail
			}
			if cu.CostMicros != nil {
				ac.CostMicros = int64(*cu.CostMicros)
			}
			if cu.Tokens != nil {
				ac.Tokens = int64(*cu.Tokens)
			}
			if cu.Calls != nil {
				ac.Calls = *cu.Calls
			}
			if cu.ByModel != nil {
				ac.ByModel = make([]AppCustomerModelCost, 0, len(*cu.ByModel))
				for _, m := range *cu.ByModel {
					mc := AppCustomerModelCost{}
					if m.Provider != nil {
						mc.Provider = *m.Provider
					}
					if m.Model != nil {
						mc.Model = *m.Model
					}
					if m.CostMicros != nil {
						mc.CostMicros = int64(*m.CostMicros)
					}
					if m.Tokens != nil {
						mc.Tokens = int64(*m.Tokens)
					}
					if m.Calls != nil {
						mc.Calls = *m.Calls
					}
					ac.ByModel = append(ac.ByModel, mc)
				}
			}
			s.Customers = append(s.Customers, ac)
		}
	}
	return s
}

func llmCostFrom(w wire.CosttrackingMyCostReportResponse) LLMCost {
	c := LLMCost{Currency: "EUR"}
	if w.CostMicros != nil {
		c.CostMicros = int64(*w.CostMicros)
	}
	if w.Currency != nil {
		c.Currency = *w.Currency
	}
	if w.PeriodStart != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodStart); err == nil {
			c.PeriodStart = t
		}
	}
	if w.PeriodEnd != nil {
		if t, err := time.Parse(time.RFC3339, *w.PeriodEnd); err == nil {
			c.PeriodEnd = t
		}
	}
	return c
}

func balanceFrom(unit string, w wire.MeteringBalanceResponse) Balance {
	b := Balance{Unit: unit}
	if w.Unit != nil {
		b.Unit = *w.Unit
	}
	if w.Balance != nil {
		b.Remaining = int64(*w.Balance)
	}
	if w.Limit != nil {
		b.Limit = int64(*w.Limit)
	}
	if w.Consumed != nil {
		b.Consumed = int64(*w.Consumed)
	}
	if w.Periodic != nil {
		b.Periodic = *w.Periodic
	}
	return b
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
