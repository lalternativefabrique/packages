package sdk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The event types Lungor delivers. An app registers for these by name and
// switches on them on receipt, so they are a public contract: types may be
// added, never renamed.
//
// They are constants here because the alternative is every consumer writing the
// string by hand and discovering its typo in production — a subscribed endpoint
// that never fires looks exactly like a customer who never subscribed.
const (
	EventSubscriptionActivated = "subscription.activated"
	EventSubscriptionRenewed   = "subscription.renewed"
	EventSubscriptionCanceled  = "subscription.canceled"
	EventSubscriptionPastDue   = "subscription.past_due"
	EventEntitlementChanged    = "entitlement.changed"
)

// Headers Lungor sets on every delivery. The names carry the brand; the signing
// scheme behind them is shared with every other product using the same
// dispatcher.
const (
	HeaderSignature     = "Lungor-Signature"
	HeaderTimestamp     = "Lungor-Timestamp"
	HeaderEvent         = "Lungor-Event"
	HeaderDeliveryID    = "Lungor-Delivery-ID"
	HeaderSourceEventID = "Lungor-Source-Event-ID"
)

// DefaultTolerance bounds how old a delivery may be and still verify.
//
// Without a bound, a signature stays valid forever and anyone who captures one
// delivery can replay it indefinitely. Five minutes absorbs ordinary clock skew
// and retry delay while keeping a captured request useless within the hour.
const DefaultTolerance = 5 * time.Minute

// ErrInvalidSignature — the delivery did not come from Lungor, or was altered,
// or is older than the tolerance. Answer 400 and do not process the body.
//
// Never fall back to trusting an unverified body when this is returned: an
// endpoint URL is public, so anything that reaches it is attacker-controlled
// until the signature says otherwise.
var ErrInvalidSignature = errors.New("lungor: invalid webhook signature")

// WebhookEndpoint is a delivery destination registered for one app.
type WebhookEndpoint struct {
	ID          string
	URL         string
	EventTypes  []string
	Description string
	// Status is "active" or "disabled". A disabled endpoint keeps its
	// registration and its secret but receives nothing.
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Active reports whether this endpoint currently receives deliveries.
func (e WebhookEndpoint) Active() bool { return e.Status == "active" }

// CreateWebhookEndpointInput registers a destination.
type CreateWebhookEndpointInput struct {
	// URL must be publicly reachable over http(s). Lungor refuses private and
	// link-local addresses at dispatch time, so a localhost URL registers and
	// then never delivers.
	URL string
	// EventTypes are the types to receive, from the Event* constants. Lungor
	// rejects a type outside its catalogue rather than accepting an endpoint
	// that would stay silent.
	EventTypes []string
	// Description is a free-form label for the operator reading the list back.
	Description string
}

// CreatedWebhookEndpoint is a registration plus the one and only sight of its
// secret.
type CreatedWebhookEndpoint struct {
	WebhookEndpoint
	// Secret signs every delivery to this endpoint. Lungor returns it HERE AND
	// NOWHERE ELSE — reading the endpoint back never discloses it again. Store
	// it before discarding this value; recovering from a lost secret means
	// RotateWebhookSecret, which invalidates deliveries signed with the old one.
	Secret string
}

// CreateWebhookEndpoint registers a destination and returns its signing secret.
func (c *Client) CreateWebhookEndpoint(ctx context.Context, in CreateWebhookEndpointInput) (CreatedWebhookEndpoint, error) {
	if c.baseURL == "" || c.appKey == "" {
		return CreatedWebhookEndpoint{}, ErrNotConfigured
	}
	if in.URL == "" {
		return CreatedWebhookEndpoint{}, fmt.Errorf("%w: empty url", ErrBadRequest)
	}
	if len(in.EventTypes) == 0 {
		return CreatedWebhookEndpoint{}, fmt.Errorf("%w: no event types", ErrBadRequest)
	}

	body := CreateEndpointCreateEndpointRequest{
		Url:         &in.URL,
		EventTypes:  &in.EventTypes,
		Description: &in.Description,
	}
	var wire CreateEndpointResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/webhooks/endpoints", body, &wire); err != nil {
		return CreatedWebhookEndpoint{}, err
	}

	out := CreatedWebhookEndpoint{
		WebhookEndpoint: WebhookEndpoint{
			ID:          deref(wire.Id),
			URL:         deref(wire.Url),
			Description: deref(wire.Description),
			Status:      deref(wire.Status),
		},
		Secret: deref(wire.Secret),
	}
	if wire.EventTypes != nil {
		out.EventTypes = *wire.EventTypes
	}
	return out, nil
}

// ListWebhookEndpoints returns the app's registered destinations.
//
// limit <= 0 leaves paging to Lungor's default.
func (c *Client) ListWebhookEndpoints(ctx context.Context, limit, offset int) ([]WebhookEndpoint, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}

	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	path := "/api/v1/webhooks/endpoints"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	var wire ListEndpointsResult
	if err := c.do(ctx, http.MethodGet, path, nil, &wire); err != nil {
		return nil, err
	}
	if wire.Items == nil {
		return nil, nil
	}
	out := make([]WebhookEndpoint, 0, len(*wire.Items))
	for _, v := range *wire.Items {
		out = append(out, endpointFrom(v))
	}
	return out, nil
}

// GetWebhookEndpoint reads one destination. ErrNotFound means it does not exist
// or belongs to another app — the two are indistinguishable on purpose.
func (c *Client) GetWebhookEndpoint(ctx context.Context, id string) (WebhookEndpoint, error) {
	if c.baseURL == "" || c.appKey == "" {
		return WebhookEndpoint{}, ErrNotConfigured
	}
	if id == "" {
		return WebhookEndpoint{}, fmt.Errorf("%w: empty endpoint id", ErrBadRequest)
	}

	var wire RepositoryEndpointView
	if err := c.do(ctx, http.MethodGet, "/api/v1/webhooks/endpoints/"+url.PathEscape(id), nil, &wire); err != nil {
		return WebhookEndpoint{}, err
	}
	return endpointFrom(wire), nil
}

// UpdateWebhookEndpointInput carries only the fields to change. A nil field is
// left alone, which is why these are pointers: distinguishing "clear the
// description" from "do not touch it" is impossible with plain strings.
type UpdateWebhookEndpointInput struct {
	URL         *string
	EventTypes  *[]string
	Description *string
	// Disabled stops or resumes delivery without losing the registration or
	// rotating the secret.
	Disabled *bool
}

// UpdateWebhookEndpoint changes a destination in place.
func (c *Client) UpdateWebhookEndpoint(ctx context.Context, id string, in UpdateWebhookEndpointInput) error {
	if c.baseURL == "" || c.appKey == "" {
		return ErrNotConfigured
	}
	if id == "" {
		return fmt.Errorf("%w: empty endpoint id", ErrBadRequest)
	}

	body := UpdateEndpointUpdateEndpointRequest{
		Url:         in.URL,
		EventTypes:  in.EventTypes,
		Description: in.Description,
		Disabled:    in.Disabled,
	}
	return c.do(ctx, http.MethodPatch, "/api/v1/webhooks/endpoints/"+url.PathEscape(id), body, nil)
}

// DeleteWebhookEndpoint removes a destination permanently. To stop delivery
// reversibly, update it with Disabled instead.
func (c *Client) DeleteWebhookEndpoint(ctx context.Context, id string) error {
	if c.baseURL == "" || c.appKey == "" {
		return ErrNotConfigured
	}
	if id == "" {
		return fmt.Errorf("%w: empty endpoint id", ErrBadRequest)
	}
	return c.do(ctx, http.MethodDelete, "/api/v1/webhooks/endpoints/"+url.PathEscape(id), nil, nil)
}

// RotateWebhookSecret issues a new signing secret and returns it once.
//
// The old secret stops verifying immediately, so a delivery already in flight
// fails its signature check and is retried under the new one. Store the result
// before the next delivery arrives.
func (c *Client) RotateWebhookSecret(ctx context.Context, id string) (string, error) {
	if c.baseURL == "" || c.appKey == "" {
		return "", ErrNotConfigured
	}
	if id == "" {
		return "", fmt.Errorf("%w: empty endpoint id", ErrBadRequest)
	}

	var wire RotateSecretResult
	if err := c.do(ctx, http.MethodPost, "/api/v1/webhooks/endpoints/"+url.PathEscape(id)+"/rotate-secret", nil, &wire); err != nil {
		return "", err
	}
	return deref(wire.Secret), nil
}

func endpointFrom(w RepositoryEndpointView) WebhookEndpoint {
	out := WebhookEndpoint{
		ID:          deref(w.Id),
		URL:         deref(w.Url),
		Description: deref(w.Description),
		Status:      deref(w.Status),
	}
	if w.EventTypes != nil {
		out.EventTypes = *w.EventTypes
	}
	if w.CreatedAt != nil {
		out.CreatedAt, _ = time.Parse(time.RFC3339, *w.CreatedAt)
	}
	if w.UpdatedAt != nil {
		out.UpdatedAt, _ = time.Parse(time.RFC3339, *w.UpdatedAt)
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Delivery is one verified webhook: what happened, and the raw body to decode.
type Delivery struct {
	// Type is one of the Event* constants.
	Type string
	// ID identifies this delivery attempt.
	ID string
	// SourceEventID identifies the FACT behind it, and is stable across
	// retries and across two publications of the same event. Deduplicate on
	// this, never on ID: Lungor guarantees at-least-once delivery, so the same
	// activation can arrive twice with two different delivery ids.
	SourceEventID string
	// Timestamp is when Lungor signed the delivery.
	Timestamp time.Time
	// Payload is the raw JSON body, verified before being handed over.
	Payload []byte
}

// VerifyWebhook authenticates a delivery and returns it.
//
// Call it with the secret of the endpoint the request arrived on, BEFORE
// reading the body for anything: an endpoint URL is public, so an unverified
// body is attacker-controlled data that happens to be shaped like a
// subscription event. Acting on one is how a forged request grants a paid tier.
//
//	body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
//	d, err := sdk.VerifyWebhook(secret, r.Header, body)
//	if err != nil {
//	    http.Error(w, "bad signature", http.StatusBadRequest)
//	    return
//	}
//	switch d.Type {
//	case sdk.EventSubscriptionActivated:
//	    // ...
//	}
//	w.WriteHeader(http.StatusOK)
//
// Answer 2xx once the delivery is durably recorded. Any other status is a
// failure Lungor retries with backoff, so answering 500 on a duplicate turns a
// harmless repeat into a delivery that never completes.
func VerifyWebhook(secret string, headers http.Header, body []byte) (Delivery, error) {
	return VerifyWebhookAt(secret, headers, body, time.Now(), DefaultTolerance)
}

// VerifyWebhookAt is VerifyWebhook with the clock and tolerance supplied, for
// tests and for callers whose skew justifies a different window.
//
// tolerance <= 0 disables the freshness check, which leaves every past
// signature valid forever. Only do that with an independent replay guard on
// SourceEventID.
func VerifyWebhookAt(secret string, headers http.Header, body []byte, now time.Time, tolerance time.Duration) (Delivery, error) {
	if secret == "" {
		return Delivery{}, fmt.Errorf("%w: no secret", ErrInvalidSignature)
	}

	rawTS := headers.Get(HeaderTimestamp)
	if rawTS == "" {
		return Delivery{}, fmt.Errorf("%w: missing %s", ErrInvalidSignature, HeaderTimestamp)
	}
	epoch, err := strconv.ParseInt(rawTS, 10, 64)
	if err != nil {
		return Delivery{}, fmt.Errorf("%w: malformed timestamp", ErrInvalidSignature)
	}
	signedAt := time.Unix(epoch, 0)

	// Checked in both directions: a future timestamp is as much a forgery
	// signal as an expired one, and skipping it lets a captured delivery be
	// replayed by dating it forward.
	if tolerance > 0 {
		if drift := now.Sub(signedAt); drift > tolerance || drift < -tolerance {
			return Delivery{}, fmt.Errorf("%w: timestamp outside tolerance", ErrInvalidSignature)
		}
	}

	presented := headers.Get(HeaderSignature)
	if presented == "" {
		return Delivery{}, fmt.Errorf("%w: missing %s", ErrInvalidSignature, HeaderSignature)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(rawTS))
	mac.Write([]byte("."))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	// The header carries a scheme prefix so the algorithm can change without
	// breaking parsing; a version this SDK does not know is a failure, never a
	// silent accept. Multiple space-separated signatures let a rotation be
	// verified against either secret.
	ok := false
	for _, candidate := range strings.Fields(presented) {
		version, digest, found := strings.Cut(candidate, "=")
		if !found || version != "v1" {
			continue
		}
		// Constant time: a byte-by-byte comparison leaks how much of a guessed
		// signature was right, which is enough to forge one.
		if hmac.Equal([]byte(digest), []byte(expected)) {
			ok = true
		}
	}
	if !ok {
		return Delivery{}, ErrInvalidSignature
	}

	return Delivery{
		Type:          headers.Get(HeaderEvent),
		ID:            headers.Get(HeaderDeliveryID),
		SourceEventID: headers.Get(HeaderSourceEventID),
		Timestamp:     signedAt,
		Payload:       body,
	}, nil
}
