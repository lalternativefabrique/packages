// Package mollie is a minimal client for the Mollie API, covering exactly what
// Synthiz needs to sell one monthly subscription: create a customer, open a
// first payment that establishes a recurring mandate, hand the recurring
// schedule over to Mollie, and read a payment back.
//
// Mollie specifics this client encodes, learned the hard way:
//
//  1. MONEY IS A STRING. Amounts are {"value":"9.00","currency":"EUR"}, never
//     integer cents. Conversion happens at the wire boundary, in integer
//     arithmetic — money must never round-trip through a float.
//
//  2. THE API KEY SELECTS THE MODE. A test_xxx key talks to the sandbox, a
//     live_xxx key to production. There is no mode parameter.
//
//  3. RECURRING NEEDS A MANDATE FIRST. A subscription can only exist once the
//     customer has a valid mandate, and a mandate only appears after a payment
//     with sequenceType "first" is PAID. So: first payment -> webhook -> then
//     create the subscription.
//
//  4. WEBHOOKS ARE NOT SIGNED and carry no state — the body is just
//     "id=tr_xxx", form-encoded. The truth comes from re-fetching the payment
//     with the API key (GetPayment below), never from the body.
//
//  5. SUBSCRIPTION STATUS CHANGES ARE NEVER PUSHED. Mollie only webhooks about
//     PAYMENTS. When it gives up on a failing mandate (5 daily retries, or
//     immediately on reasons like AC04/MD07) it cancels the subscription in
//     silence. The only way to learn about it is to poll — see GetSubscription,
//     which the reconciler calls on a schedule.
package mollie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const baseURL = "https://api.mollie.com/v2"

// ErrNotFound is returned when Mollie answers 404: the resource is gone (or
// never existed). Callers distinguish it from a transient failure with
// errors.Is — the difference decides whether a subscription gets reconciled away
// or merely retried later.
var ErrNotFound = errors.New("mollie: resource not found")

// Client talks to Mollie for one merchant account (one API key).
type Client struct {
	apiKey string
	base   string
	http   *http.Client
}

// New returns nil when apiKey is empty, so a caller can treat "not configured"
// as a nil client and degrade gracefully.
func New(apiKey string) *Client {
	if apiKey == "" {
		return nil
	}
	return &Client{
		apiKey: apiKey,
		base:   baseURL,
		http:   &http.Client{Timeout: 15 * time.Second},
	}
}

// NewWithBaseURL is New with the API root overridden. Only useful to point the
// client at a stub in tests; production always uses New.
func NewWithBaseURL(apiKey, base string) *Client {
	c := New(apiKey)
	if c == nil {
		return nil
	}
	c.base = strings.TrimRight(base, "/")
	return c
}

// Payment is the subset of a Mollie payment Synthiz reads.
type Payment struct {
	ID          string
	Status      string
	AmountCents int64
	Currency    string
	CustomerID  string
	Metadata    map[string]string
	PaidAt      *time.Time
}

// IsPaid reports whether the payment settled. Only "paid" means money moved.
func (p Payment) IsPaid() bool { return p.Status == "paid" }

// CreateCustomer provisions the Mollie customer that will own the mandate.
func (c *Client) CreateCustomer(ctx context.Context, name, email string) (string, error) {
	if name == "" {
		name = email
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/customers",
		map[string]any{"name": name, "email": email}, &out); err != nil {
		return "", fmt.Errorf("create customer: %w", err)
	}
	if out.ID == "" {
		return "", fmt.Errorf("create customer: empty id in response")
	}
	return out.ID, nil
}

// FirstPayment opens the hosted payment that both collects the first month AND
// establishes the mandate future charges ride on (sequenceType "first").
// Returns the payment id and the URL to send the customer to.
func (c *Client) FirstPayment(ctx context.Context, in FirstPaymentInput) (id, checkoutURL string, err error) {
	body := map[string]any{
		"amount":       map[string]string{"value": centsToValue(in.AmountCents), "currency": in.Currency},
		"description":  in.Description,
		"redirectUrl":  in.RedirectURL,
		"webhookUrl":   in.WebhookURL,
		"customerId":   in.CustomerID,
		"sequenceType": "first",
		"metadata":     in.Metadata,
		// Cards only, and pinned HERE rather than left to the dashboard.
		//
		// The mandate established by this payment is what every later charge rides
		// on, including a mid-period upgrade's proration — and that proration is
		// only safe to collect because a card answers synchronously: the caller
		// learns whether the money moved before deciding to grant the new tier. A
		// SEPA or iDEAL mandate would answer "pending" for days, forcing a choice
		// between making the customer wait for a few euros and granting a tier that
		// may never be paid for.
		//
		// Sending no method at all offers whatever the Mollie account has enabled,
		// so "cards only" was an intention nothing enforced: a single toggle in a
		// dashboard nobody diffs could silently break the upgrade path.
		"method": "creditcard",
	}
	var out struct {
		ID    string `json:"id"`
		Links struct {
			Checkout struct {
				Href string `json:"href"`
			} `json:"checkout"`
		} `json:"_links"`
	}
	if err := c.do(ctx, http.MethodPost, "/payments", body, &out); err != nil {
		return "", "", fmt.Errorf("first payment: %w", err)
	}
	return out.ID, out.Links.Checkout.Href, nil
}

// FirstPaymentInput is the envelope for FirstPayment.
type FirstPaymentInput struct {
	CustomerID  string
	AmountCents int64
	Currency    string
	Description string
	RedirectURL string
	WebhookURL  string
	Metadata    map[string]string
}

// RecurringPayment charges an amount ONCE on the mandate the customer already
// has (sequenceType "recurring"). No checkout, no redirect: the customer is not
// present, and there is nothing for them to approve — the mandate they gave at
// the first payment is the authorisation.
//
// This is what a mid-period upgrade's proration rides on. It must never be an
// extra checkout: a second checkout opens a second mandate on the same customer,
// and only the newest stays reachable while the older keeps charging.
//
// The call is SYNCHRONOUS in outcome for card mandates — the returned status
// says whether the money moved — which is what lets a caller refuse to apply an
// upgrade whose payment failed. That guarantee is why the payment methods are
// pinned to cards (see FirstPayment): an asynchronous method would return
// "pending" here and leave the caller to grant or withhold on a guess.
func (c *Client) RecurringPayment(ctx context.Context, in RecurringPaymentInput) (id, status string, err error) {
	body := map[string]any{
		"amount":       map[string]string{"value": centsToValue(in.AmountCents), "currency": in.Currency},
		"description":  in.Description,
		"customerId":   in.CustomerID,
		"sequenceType": "recurring",
		"webhookUrl":   in.WebhookURL,
		"metadata":     in.Metadata,
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := c.do(ctx, http.MethodPost, "/payments", body, &out); err != nil {
		return "", "", fmt.Errorf("recurring payment: %w", err)
	}
	return out.ID, out.Status, nil
}

// RecurringPaymentInput is the envelope for RecurringPayment.
type RecurringPaymentInput struct {
	CustomerID  string
	AmountCents int64
	Currency    string
	// Description is what the customer reads on their statement. An
	// unrecognisable line is a chargeback, so it must name the product and the
	// reason for the charge.
	Description string
	WebhookURL  string
	// Metadata travels back on the webhook. Carry the idempotency key here: it is
	// how a replayed webhook is recognised as the same logical charge.
	Metadata map[string]string
}

// PaymentPaid is the status Mollie reports for money that actually moved. Any
// other value means it did not — "open", "pending", "failed", "canceled",
// "expired" — and a caller granting entitlement must treat all of them as a
// refusal rather than allow-listing the failures it happens to know about.
const PaymentPaid = "paid"

// CreateSubscription hands the recurring schedule to Mollie: from here on it
// charges the mandate on its own and calls WebhookURL for every resulting
// payment. Requires a customer with a valid mandate (i.e. a PAID first payment).
// interval uses Mollie's format, e.g. "1 month".
func (c *Client) CreateSubscription(ctx context.Context, in SubscriptionInput) (string, error) {
	body := map[string]any{
		"amount":      map[string]string{"value": centsToValue(in.AmountCents), "currency": in.Currency},
		"interval":    in.Interval,
		"description": in.Description,
		"webhookUrl":  in.WebhookURL,
		"metadata":    in.Metadata,
	}
	if in.StartDate != "" {
		body["startDate"] = in.StartDate
	}
	var out struct {
		ID string `json:"id"`
	}
	path := "/customers/" + url.PathEscape(in.CustomerID) + "/subscriptions"
	if err := c.do(ctx, http.MethodPost, path, body, &out); err != nil {
		return "", fmt.Errorf("create subscription: %w", err)
	}
	return out.ID, nil
}

// SubscriptionDescription builds the description Mollie shows the customer.
//
// Mollie requires it to be UNIQUE PER CUSTOMER: a fixed string like "Synthiz
// subscription" makes CreateSubscription fail the moment the same customer
// subscribes a second time — i.e. exactly when someone who cancelled comes back,
// the one case we most want to work. Suffixing the owner id makes it
// deterministic AND unique without inventing a random token nobody can trace
// back.
//
// It also lands on the customer's bank statement, so the product label stays
// first and the id trails behind it.
func SubscriptionDescription(product, ownerID string) string {
	if product == "" {
		product = "Subscription"
	}
	if ownerID == "" {
		return product
	}
	return product + " " + ownerID
}

// SubscriptionInput is the envelope for CreateSubscription.
type SubscriptionInput struct {
	CustomerID  string
	AmountCents int64
	Currency    string
	Interval    string // Mollie format: "1 month"
	Description string
	WebhookURL  string
	// StartDate (YYYY-MM-DD) delays the first recurring charge. The first month
	// is already paid by FirstPayment, so callers set this to a month out to
	// avoid charging twice.
	StartDate string
	Metadata  map[string]string
}

// CancelSubscription stops future charges. Idempotent enough for our use: a
// 404/410 (already gone) is not an error worth surfacing.
func (c *Client) CancelSubscription(ctx context.Context, customerID, subscriptionID string) error {
	path := "/customers/" + url.PathEscape(customerID) + "/subscriptions/" + url.PathEscape(subscriptionID)
	if err := c.do(ctx, http.MethodDelete, path, nil, nil); err != nil {
		// Already gone at Mollie is the outcome we wanted. Swallowing it here is
		// what makes cancellation safe to retry — and what keeps account deletion
		// from being blocked by a schedule Mollie already dropped.
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("cancel subscription: %w", err)
	}
	return nil
}

// Subscription is the subset of a Mollie recurring schedule Synthiz reads back
// when reconciling. We poll it because Mollie never notifies us of a status
// change (see the package doc): a subscription it killed after failed retries
// looks perfectly alive in our DB until someone asks.
type Subscription struct {
	ID     string
	Status string
	// NextPaymentDate is when Mollie will next charge the mandate. Absent once
	// the schedule is dead. Mollie sends a bare YYYY-MM-DD, so this is a date at
	// UTC midnight, not an instant.
	NextPaymentDate *time.Time
	// CanceledAt is set once the schedule is canceled, whoever canceled it — us
	// or Mollie giving up on the mandate.
	CanceledAt *time.Time
}

// IsActive reports whether Mollie will still charge this schedule. "pending"
// (waiting on a start date) deliberately does NOT count as active, but it is not
// dead either — it must not trigger a reconcile.
func (s Subscription) IsActive() bool { return s.Status == "active" }

// IsDead reports whether the schedule will never charge again. This is the only
// signal that justifies revoking a local recurring id: canceled (by us or by
// Mollie after failed retries), suspended (mandate revoked), completed (fixed
// number of charges exhausted).
func (s Subscription) IsDead() bool {
	switch s.Status {
	case "canceled", "suspended", "completed":
		return true
	}
	return false
}

// GetSubscription reads one recurring schedule back from Mollie. A 404 comes
// back as ErrNotFound, which the reconciler treats as "this schedule is gone"
// — the same outcome as a canceled status, and a distinction that must never be
// confused with a network blip (which must NOT revoke anything).
func (c *Client) GetSubscription(ctx context.Context, customerID, subscriptionID string) (Subscription, error) {
	var out struct {
		ID              string `json:"id"`
		Status          string `json:"status"`
		NextPaymentDate string `json:"nextPaymentDate"`
		CanceledAt      string `json:"canceledAt"`
	}
	path := "/customers/" + url.PathEscape(customerID) + "/subscriptions/" + url.PathEscape(subscriptionID)
	if err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return Subscription{}, fmt.Errorf("get subscription %s: %w", subscriptionID, err)
	}
	sub := Subscription{ID: out.ID, Status: out.Status}
	// nextPaymentDate is a bare date (YYYY-MM-DD); canceledAt is a full RFC3339
	// instant. Different shapes, same "unparseable → leave nil" tolerance: a date
	// we cannot read must never turn into a wrong one.
	if out.NextPaymentDate != "" {
		if t, perr := time.Parse("2006-01-02", out.NextPaymentDate); perr == nil {
			sub.NextPaymentDate = &t
		}
	}
	if out.CanceledAt != "" {
		if t, perr := time.Parse(time.RFC3339, out.CanceledAt); perr == nil {
			sub.CanceledAt = &t
		}
	}
	return sub, nil
}

// GetPayment re-fetches a payment. This is the webhook's security boundary: the
// notification body is unsigned and says only "id=tr_xxx", so the authenticated
// read is what makes the status trustworthy.
func (c *Client) GetPayment(ctx context.Context, paymentID string) (Payment, error) {
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Amount struct {
			Value    string `json:"value"`
			Currency string `json:"currency"`
		} `json:"amount"`
		CustomerID string            `json:"customerId"`
		Metadata   map[string]string `json:"metadata"`
		PaidAt     string            `json:"paidAt"`
	}
	if err := c.do(ctx, http.MethodGet, "/payments/"+url.PathEscape(paymentID), nil, &out); err != nil {
		return Payment{}, fmt.Errorf("get payment %s: %w", paymentID, err)
	}
	cents, err := valueToCents(out.Amount.Value)
	if err != nil {
		return Payment{}, err
	}
	p := Payment{
		ID: out.ID, Status: out.Status, AmountCents: cents,
		Currency: out.Amount.Currency, CustomerID: out.CustomerID, Metadata: out.Metadata,
	}
	if out.PaidAt != "" {
		if t, perr := time.Parse(time.RFC3339, out.PaidAt); perr == nil {
			p.PaidAt = &t
		}
	}
	return p, nil
}

// PaymentIDFromWebhook extracts the payment id from a Mollie webhook body. The
// body is form-encoded ("id=tr_xxx") — NOT JSON, a distinction that has bitten
// us before.
func PaymentIDFromWebhook(rawBody []byte) (string, error) {
	values, err := url.ParseQuery(string(rawBody))
	if err != nil {
		return "", fmt.Errorf("parse webhook body: %w", err)
	}
	id := values.Get("id")
	if id == "" {
		return "", fmt.Errorf("webhook body carries no payment id")
	}
	return id, nil
}

// centsToValue renders integer cents as Mollie's decimal string: 900 -> "9.00".
func centsToValue(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	s := fmt.Sprintf("%d.%02d", cents/100, cents%100)
	if neg {
		s = "-" + s
	}
	return s
}

// valueToCents parses Mollie's decimal string back to integer cents ("9.00" ->
// 900). Pure integer arithmetic: money never goes through a binary float, where
// values like 0.29 lose precision.
func valueToCents(value string) (int64, error) {
	s := strings.TrimSpace(value)
	if s == "" {
		return 0, fmt.Errorf("parse amount %q: empty", value)
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	switch len(frac) {
	case 0:
		if hasDot {
			return 0, fmt.Errorf("parse amount %q: trailing decimal point", value)
		}
		frac = "00"
	case 1:
		frac += "0"
	case 2:
	default:
		return 0, fmt.Errorf("parse amount %q: more than two decimals", value)
	}
	var w, f int64
	if _, err := fmt.Sscanf(whole, "%d", &w); err != nil {
		return 0, fmt.Errorf("parse amount %q: %w", value, err)
	}
	if _, err := fmt.Sscanf(frac, "%d", &f); err != nil {
		return 0, fmt.Errorf("parse amount %q: %w", value, err)
	}
	cents := w*100 + f
	if neg {
		cents = -cents
	}
	return cents, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		r = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call mollie: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// Wrapped, not returned bare, so the caller keeps the context AND can
		// still errors.Is it: "gone for good" and "call failed" must never be
		// told apart by string matching.
		return fmt.Errorf("mollie %s %s: %w", method, path, ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mollie %s %s: status %d: %s", method, path, resp.StatusCode, string(raw))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
