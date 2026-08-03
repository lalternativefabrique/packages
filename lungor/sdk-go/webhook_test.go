package sdk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// signature reproduces what Lungor's dispatcher sends: HMAC-SHA256 over
// "<unix>.<body>", hex, prefixed "v1=".
func signature(t *testing.T, secret string, at time.Time, body []byte) (string, string) {
	t.Helper()
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return ts, "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func deliveryHeaders(t *testing.T, secret string, at time.Time, body []byte) http.Header {
	t.Helper()
	ts, sig := signature(t, secret, at, body)
	h := http.Header{}
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderSignature, sig)
	h.Set(HeaderEvent, EventSubscriptionActivated)
	h.Set(HeaderDeliveryID, "dlv_1")
	h.Set(HeaderSourceEventID, "sub_1:active:2026-09-01T00:00:00Z")
	return h
}

func TestVerifyWebhook_AcceptsAGenuineDelivery(t *testing.T) {
	now := time.Unix(1750000000, 0)
	body := []byte(`{"SubscriptionID":"sub_1","Status":"active"}`)

	got, err := VerifyWebhookAt("whsec_x", deliveryHeaders(t, "whsec_x", now, body), body, now, DefaultTolerance)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Type != EventSubscriptionActivated {
		t.Errorf("type = %q, want %q", got.Type, EventSubscriptionActivated)
	}
	if got.ID != "dlv_1" {
		t.Errorf("id = %q, want dlv_1", got.ID)
	}
	if got.SourceEventID != "sub_1:active:2026-09-01T00:00:00Z" {
		t.Errorf("source event id = %q", got.SourceEventID)
	}
	if !got.Timestamp.Equal(now) {
		t.Errorf("timestamp = %v, want %v", got.Timestamp, now)
	}
	if string(got.Payload) != string(body) {
		t.Errorf("payload = %q", got.Payload)
	}
}

// A body altered in flight must not verify — this is the whole point of
// signing, and the case a length-only or prefix comparison would let through.
func TestVerifyWebhook_RejectsATamperedBody(t *testing.T) {
	now := time.Unix(1750000000, 0)
	signed := []byte(`{"Status":"past_due"}`)
	headers := deliveryHeaders(t, "whsec_x", now, signed)

	_, err := VerifyWebhookAt("whsec_x", headers, []byte(`{"Status":"active"}`), now, DefaultTolerance)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhook_RejectsTheWrongSecret(t *testing.T) {
	now := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)

	_, err := VerifyWebhookAt("whsec_other", deliveryHeaders(t, "whsec_x", now, body), body, now, DefaultTolerance)
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

// A captured delivery replayed later must stop verifying, and one dated into
// the future must too — a forger controls the timestamp they present.
func TestVerifyWebhook_RejectsOutsideTheTolerance(t *testing.T) {
	signedAt := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)
	headers := deliveryHeaders(t, "whsec_x", signedAt, body)

	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{"replayed an hour later", signedAt.Add(time.Hour)},
		{"dated into the future", signedAt.Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyWebhookAt("whsec_x", headers, body, tc.now, DefaultTolerance); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("err = %v, want ErrInvalidSignature", err)
			}
		})
	}
}

func TestVerifyWebhook_AcceptsWithinTheTolerance(t *testing.T) {
	signedAt := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)
	headers := deliveryHeaders(t, "whsec_x", signedAt, body)

	if _, err := VerifyWebhookAt("whsec_x", headers, body, signedAt.Add(2*time.Minute), DefaultTolerance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// During a rotation both secrets may sign in-flight deliveries, so the header
// carries several signatures and any one matching is enough.
func TestVerifyWebhook_AcceptsOneOfSeveralSignatures(t *testing.T) {
	now := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)
	ts, oldSig := signature(t, "whsec_old", now, body)
	_, newSig := signature(t, "whsec_new", now, body)

	h := http.Header{}
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderSignature, oldSig+" "+newSig)

	if _, err := VerifyWebhookAt("whsec_new", h, body, now, DefaultTolerance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// An unknown scheme must fail closed. Accepting it because the digest happens
// to match would let a future weaker version be forced by an attacker.
func TestVerifyWebhook_RejectsAnUnknownSignatureVersion(t *testing.T) {
	now := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)
	ts, sig := signature(t, "whsec_x", now, body)

	h := http.Header{}
	h.Set(HeaderTimestamp, ts)
	h.Set(HeaderSignature, "v2="+sig[len("v1="):])

	if _, err := VerifyWebhookAt("whsec_x", h, body, now, DefaultTolerance); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("err = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyWebhook_RejectsMissingHeadersAndEmptySecret(t *testing.T) {
	now := time.Unix(1750000000, 0)
	body := []byte(`{"Status":"active"}`)
	full := deliveryHeaders(t, "whsec_x", now, body)

	noSig := full.Clone()
	noSig.Del(HeaderSignature)
	noTS := full.Clone()
	noTS.Del(HeaderTimestamp)
	badTS := full.Clone()
	badTS.Set(HeaderTimestamp, "not-a-number")

	for _, tc := range []struct {
		name    string
		secret  string
		headers http.Header
	}{
		{"no signature header", "whsec_x", noSig},
		{"no timestamp header", "whsec_x", noTS},
		{"malformed timestamp", "whsec_x", badTS},
		{"empty secret", "", full},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyWebhookAt(tc.secret, tc.headers, body, now, DefaultTolerance); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("err = %v, want ErrInvalidSignature", err)
			}
		})
	}
}

func TestCreateWebhookEndpoint_SendsTheRegistrationAndReturnsTheSecret(t *testing.T) {
	srv, rec := server(t, 201, map[string]any{
		"id":         "ep_1",
		"url":        "https://app.example/hooks",
		"eventTypes": []string{EventSubscriptionActivated},
		"status":     "active",
		"secret":     "whsec_once",
	})
	c := New(srv.URL, "sk_live_app_x")

	got, err := c.CreateWebhookEndpoint(ctx(), CreateWebhookEndpointInput{
		URL:        "https://app.example/hooks",
		EventTypes: []string{EventSubscriptionActivated},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/webhooks/endpoints" {
		t.Errorf("called %s %s", rec.method, rec.path)
	}
	if rec.auth != "Bearer sk_live_app_x" {
		t.Errorf("auth = %q", rec.auth)
	}
	if rec.body["url"] != "https://app.example/hooks" {
		t.Errorf("url sent = %v", rec.body["url"])
	}
	if got.Secret != "whsec_once" {
		t.Errorf("secret = %q, want whsec_once", got.Secret)
	}
	if !got.Active() {
		t.Errorf("status = %q, want active", got.Status)
	}
}

func TestCreateWebhookEndpoint_RefusesAnEmptyRegistrationWithoutCalling(t *testing.T) {
	srv, rec := server(t, 201, nil)
	c := New(srv.URL, "sk_live_app_x")

	for _, tc := range []struct {
		name string
		in   CreateWebhookEndpointInput
	}{
		{"no url", CreateWebhookEndpointInput{EventTypes: []string{EventSubscriptionActivated}}},
		{"no event types", CreateWebhookEndpointInput{URL: "https://app.example/hooks"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.CreateWebhookEndpoint(ctx(), tc.in); !errors.Is(err, ErrBadRequest) {
				t.Fatalf("err = %v, want ErrBadRequest", err)
			}
			if rec.method != "" {
				t.Errorf("server was called: %s %s", rec.method, rec.path)
			}
		})
	}
}

func TestListWebhookEndpoints_DecodesTheViewAndPages(t *testing.T) {
	srv, rec := server(t, 200, map[string]any{
		"items": []map[string]any{{
			"id":         "ep_1",
			"url":        "https://app.example/hooks",
			"eventTypes": []string{EventSubscriptionRenewed},
			"status":     "disabled",
			"createdAt":  "2026-08-01T10:00:00Z",
		}},
	})
	c := New(srv.URL, "sk_live_app_x")

	got, err := c.ListWebhookEndpoints(ctx(), 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/webhooks/endpoints?limit=10&offset=20" {
		t.Errorf("path = %q", rec.path)
	}
	if len(got) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(got))
	}
	if got[0].Active() {
		t.Errorf("status = %q, want disabled", got[0].Status)
	}
	if !got[0].CreatedAt.Equal(time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("createdAt = %v", got[0].CreatedAt)
	}
}

func TestListWebhookEndpoints_OmitsPagingWhenUnset(t *testing.T) {
	srv, rec := server(t, 200, map[string]any{"items": []map[string]any{}})
	c := New(srv.URL, "sk_live_app_x")

	if _, err := c.ListWebhookEndpoints(ctx(), 0, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/webhooks/endpoints" {
		t.Errorf("path = %q, want no query string", rec.path)
	}
}

func TestGetWebhookEndpoint_MapsAMissingEndpointToErrNotFound(t *testing.T) {
	srv, _ := server(t, 404, map[string]any{"message": "not found"})
	c := New(srv.URL, "sk_live_app_x")

	if _, err := c.GetWebhookEndpoint(ctx(), "ep_gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A nil field must not appear in the body at all: sending "description":"" would
// clear a description the caller never meant to touch.
func TestUpdateWebhookEndpoint_SendsOnlyTheFieldsGiven(t *testing.T) {
	srv, rec := server(t, 204, nil)
	c := New(srv.URL, "sk_live_app_x")

	disabled := true
	if err := c.UpdateWebhookEndpoint(ctx(), "ep_1", UpdateWebhookEndpointInput{Disabled: &disabled}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.method != http.MethodPatch || rec.path != "/api/v1/webhooks/endpoints/ep_1" {
		t.Errorf("called %s %s", rec.method, rec.path)
	}
	if rec.body["disabled"] != true {
		t.Errorf("disabled = %v", rec.body["disabled"])
	}
	if _, present := rec.body["description"]; present {
		t.Errorf("description was sent despite being nil: %v", rec.body)
	}
	if _, present := rec.body["url"]; present {
		t.Errorf("url was sent despite being nil: %v", rec.body)
	}
}

func TestDeleteWebhookEndpoint_AcceptsAnEmptyBody(t *testing.T) {
	srv, rec := server(t, 204, nil)
	c := New(srv.URL, "sk_live_app_x")

	if err := c.DeleteWebhookEndpoint(ctx(), "ep_1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.method != http.MethodDelete || rec.path != "/api/v1/webhooks/endpoints/ep_1" {
		t.Errorf("called %s %s", rec.method, rec.path)
	}
}

func TestRotateWebhookSecret_ReturnsTheNewSecret(t *testing.T) {
	srv, rec := server(t, 200, map[string]any{"id": "ep_1", "secret": "whsec_new"})
	c := New(srv.URL, "sk_live_app_x")

	got, err := c.RotateWebhookSecret(ctx(), "ep_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/webhooks/endpoints/ep_1/rotate-secret" {
		t.Errorf("path = %q", rec.path)
	}
	if got != "whsec_new" {
		t.Errorf("secret = %q, want whsec_new", got)
	}
}

// A bad app key must never read as "no endpoints" — the caller would show an
// empty list and conclude nothing is registered.
func TestWebhookEndpoints_MapARejectedKeyToErrUnauthorized(t *testing.T) {
	srv, _ := server(t, 401, map[string]any{"message": "bad key"})
	c := New(srv.URL, "sk_live_wrong")

	if _, err := c.ListWebhookEndpoints(ctx(), 0, 0); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestWebhookEndpoints_RequireConfiguration(t *testing.T) {
	c := New("", "")

	if _, err := c.ListWebhookEndpoints(ctx(), 0, 0); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("list: err = %v, want ErrNotConfigured", err)
	}
	if err := c.DeleteWebhookEndpoint(ctx(), "ep_1"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("delete: err = %v, want ErrNotConfigured", err)
	}
	if _, err := c.RotateWebhookSecret(ctx(), "ep_1"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("rotate: err = %v, want ErrNotConfigured", err)
	}
}
