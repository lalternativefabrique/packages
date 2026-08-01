// Package sdk is the Go client for the Lungor billing API.
//
// It exists because every consumer was writing this by hand. Lungor exposes an
// HTTP API and publishes no client, so Synthiz shipped its own `LungorClient`,
// Techtuel would have shipped a second one, and each would have re-derived the
// same auth header, the same status handling and the same idea of what
// "entitled" means — from reading the server's source.
//
// The split from lungor/core is deliberate and worth keeping:
//
//   - lungor/core holds the RULES. What a tier costs, how tiers order, whether a
//     subscription entitles at an instant, how a billing period advances. Pure
//     functions over values you already hold, safe on a hot path.
//   - this package holds the WIRE. Who is entitled right now, according to the
//     service that owns that fact. One HTTP call, one answer.
//
// A product asks Lungor *who has what*, and asks lungor/core *what that means*.
// Putting either job in the other package is what produced the duplication this
// replaces.
package sdk

import (
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

// DefaultTimeout bounds a single call.
//
// Entitlement is read on request paths, so an unbounded wait would turn a slow
// billing service into a slow product. Five seconds is far past a healthy
// response and short enough that a hung backend degrades instead of hanging.
const DefaultTimeout = 5 * time.Second

// Client talks to a Lungor deployment on behalf of ONE app.
//
// The app API key identifies the calling application, never a user: Lungor
// resolves the app from the key and scopes every answer to it. A caller
// therefore cannot ask about another app's users, which is why the user id
// travels as an opaque parameter rather than as anything Lungor has to trust.
type Client struct {
	baseURL  string
	appKey   string
	tenantID string
	appID    string
	http     *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient supplies the underlying client — for a custom transport, or a
// test server. It replaces the timeout, so set one on the client you pass.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithTimeout overrides DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http = &http.Client{Timeout: d} }
}

// WithCheckoutIdentity supplies the tenant and app ids that POST /finance/checkout
// carries in its body.
//
// Separate from the key on purpose: Lungor verifies the key AND that the app id
// matches it, so a caller that only reads entitlement needs neither. Requiring
// them in New would make the common case carry configuration it never uses.
func WithCheckoutIdentity(tenantID, appID string) Option {
	return func(c *Client) {
		c.tenantID, c.appID = tenantID, appID
	}
}

// New builds a client for a Lungor deployment.
//
// baseURL is the API root WITHOUT the version segment (https://billing.example);
// the paths are appended by this package, so a caller cannot pin a version by
// accident and then be surprised when the SDK moves.
//
// A blank baseURL or appKey yields ErrNotConfigured on every call rather than a
// nil client: a deployment with billing switched off should degrade, not panic
// at boot.
func New(baseURL, appKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		appKey:  appKey,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Errors the caller is expected to branch on.
var (
	// ErrNotConfigured — no base URL or no app key. The call was never made.
	ErrNotConfigured = errors.New("lungor: not configured")
	// ErrUnauthorized — the app key was rejected (401/403). Operator error, not
	// a customer state: do not treat it as "not entitled".
	ErrUnauthorized = errors.New("lungor: unauthorized")
	// ErrBadRequest — Lungor refused the arguments (400). A bug in the caller.
	ErrBadRequest = errors.New("lungor: bad request")
	// ErrUnavailable — transport failure or a 5xx. Transient; retrying later is
	// reasonable, and degrading is safer than failing the user's request.
	ErrUnavailable = errors.New("lungor: unavailable")
)

// StatusNoSubscription is what Lungor reports for a user it has never seen.
//
// It is a normal answer, not an error: users exist in the product long before
// they exist in the billing system, and most never appear there at all.
const StatusNoSubscription = "no_subscription"

// Entitlement is what Lungor knows about one user of one app.
type Entitlement struct {
	// Entitled is the verdict: may this user be served the paid tier right now.
	//
	// Read THIS rather than deriving from Status. The mapping from a provider
	// status to access is Lungor's rule — past_due entitles until the paid
	// period lapses, canceled entitles until the same date — and re-deriving it
	// per consumer is how two products end up disagreeing about whether a
	// customer is cut off.
	Entitled bool `json:"entitled"`
	// Status is the raw subscription status (active, trialing, past_due,
	// canceled, unpaid, paused) or StatusNoSubscription. For display and
	// diagnostics; never for access decisions.
	Status string `json:"status"`
	// Balances holds the remaining allowance per metered unit, for the units
	// asked for. Absent when none were requested or none are known.
	Balances map[string]int64 `json:"balances,omitempty"`
}

// Balance returns the remaining allowance for a unit, and whether Lungor
// reported one. A missing unit is not zero — zero means "spent", missing means
// "unknown", and a caller that conflates them refuses work it should allow.
func (e Entitlement) Balance(unit string) (int64, bool) {
	if e.Balances == nil {
		return 0, false
	}
	v, ok := e.Balances[unit]
	return v, ok
}

// Entitlement resolves what the app's user is entitled to.
//
// units names the metered units whose balance to report alongside the verdict;
// pass none to skip the ledger reads entirely.
//
// externalUserID is the CALLER's own user id, opaque to Lungor. Nothing has to
// be registered first: a user Lungor has never seen resolves to
// StatusNoSubscription with Entitled false, which is the correct answer for
// everyone who has not paid.
func (c *Client) Entitlement(ctx context.Context, externalUserID string, units ...string) (Entitlement, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Entitlement{}, ErrNotConfigured
	}
	if externalUserID == "" {
		return Entitlement{}, fmt.Errorf("%w: empty external user id", ErrBadRequest)
	}

	q := url.Values{}
	q.Set("external_user_id", externalUserID)
	if len(units) > 0 {
		q.Set("units", strings.Join(units, ","))
	}

	var out Entitlement
	if err := c.do(ctx, http.MethodGet, "/api/v1/entitlements?"+q.Encode(), nil, &out); err != nil {
		return Entitlement{}, err
	}
	return out, nil
}

// CheckoutInput starts a hosted checkout for one user.
type CheckoutInput struct {
	// PriceID is the Lungor catalogue price being bought. The AMOUNT is never
	// sent: Lungor prices the tier itself, so the page shown and the amount
	// charged cannot disagree.
	PriceID string
	// ExternalUserID is who the resulting subscription belongs to, in the
	// caller's own id space — the same value Entitlement is asked about.
	ExternalUserID string
	Email          string
	// Country is the ISO-3166 alpha-2 code VAT is computed from (EU OSS rules
	// follow the customer's country). Empty lets Lungor decide.
	Country string
	// SuccessURL is where the PSP returns on success. CancelURL is optional and
	// falls back to Lungor's own page.
	SuccessURL string
	CancelURL  string
}

// Checkout is the session Lungor opened.
type Checkout struct {
	SessionID      string `json:"session_id"`
	SubscriptionID string `json:"subscription_id"`
	// RedirectURL is where the browser must be sent. It is the only field a
	// caller normally needs.
	RedirectURL string `json:"redirect_url"`
}

// Checkout opens a hosted checkout session and returns where to send the user.
//
// Requires WithCheckoutIdentity: Lungor checks the app key AND that the app id
// in the body matches it. Calling without it fails locally rather than sending
// a request that can only be refused.
func (c *Client) Checkout(ctx context.Context, in CheckoutInput) (Checkout, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Checkout{}, ErrNotConfigured
	}
	if c.tenantID == "" || c.appID == "" {
		return Checkout{}, fmt.Errorf("%w: checkout needs WithCheckoutIdentity", ErrNotConfigured)
	}
	if in.PriceID == "" || in.ExternalUserID == "" {
		return Checkout{}, fmt.Errorf("%w: price id and external user id are required", ErrBadRequest)
	}

	body := map[string]string{
		"tenant_id":        c.tenantID,
		"app_id":           c.appID,
		"price_id":         in.PriceID,
		"external_user_id": in.ExternalUserID,
		"email":            in.Email,
		"country":          in.Country,
		"success_url":      in.SuccessURL,
		"cancel_url":       in.CancelURL,
	}

	var out Checkout
	if err := c.do(ctx, http.MethodPost, "/api/v1/finance/checkout", body, &out); err != nil {
		return Checkout{}, err
	}
	return out, nil
}

// do issues one request and decodes the response.
//
// Every non-2xx is mapped onto one of this package's errors, so callers branch
// on meaning rather than on status codes. The distinction that matters most is
// ErrUnauthorized vs a "not entitled" answer: a rejected app key is an operator
// mistake, and silently reading it as "this customer has not paid" would cut off
// every paying user at once.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%w: encoding request: %v", ErrBadRequest, err)
		}
		reader = strings.NewReader(string(buf))
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("%w: building request: %v", ErrBadRequest, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.appKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrUnauthorized
	case resp.StatusCode == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, snippet(resp.Body))
	case resp.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 300:
		return fmt.Errorf("%w: unexpected status %d", ErrUnavailable, resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decoding response: %v", ErrUnavailable, err)
	}
	return nil
}

// snippet reads a bounded prefix of an error body, so a misbehaving server
// cannot put an unbounded string into the caller's logs.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil || len(b) == 0 {
		return "no detail"
	}
	return strings.TrimSpace(string(b))
}
