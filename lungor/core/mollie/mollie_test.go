package mollie

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Money must survive the round trip exactly. Mollie speaks decimal strings; we
// speak integer cents. A float in the middle silently corrupts amounts like
// 0.29, which is how you end up billing the wrong number.
func TestAmountRoundTrip(t *testing.T) {
	cases := []struct {
		cents int64
		value string
	}{
		{0, "0.00"},
		{9, "0.09"},
		{29, "0.29"},
		{99, "0.99"},
		{100, "1.00"},
		{900, "9.00"},
		{123456, "1234.56"},
		{-500, "-5.00"},
	}
	for _, tc := range cases {
		if got := centsToValue(tc.cents); got != tc.value {
			t.Errorf("centsToValue(%d) = %q, want %q", tc.cents, got, tc.value)
		}
		back, err := valueToCents(tc.value)
		if err != nil {
			t.Errorf("valueToCents(%q): %v", tc.value, err)
			continue
		}
		if back != tc.cents {
			t.Errorf("valueToCents(%q) = %d, want %d", tc.value, back, tc.cents)
		}
	}
}

func TestValueToCents_Rejects(t *testing.T) {
	for _, bad := range []string{"", "9.", "1.234", "abc"} {
		if _, err := valueToCents(bad); err == nil {
			t.Errorf("valueToCents(%q) = nil error, want a rejection", bad)
		}
	}
}

// The webhook body is form-encoded ("id=tr_xxx"), never JSON. Parsing it as JSON
// is exactly the bug that silently dropped payments in lungor.
func TestPaymentIDFromWebhook(t *testing.T) {
	id, err := PaymentIDFromWebhook([]byte("id=tr_CyUoKgnkPi"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != "tr_CyUoKgnkPi" {
		t.Fatalf("id = %q, want tr_CyUoKgnkPi", id)
	}
}

func TestPaymentIDFromWebhook_Rejects(t *testing.T) {
	for _, bad := range []string{"", "nothing-useful", `{"id":"tr_1"}`} {
		if _, err := PaymentIDFromWebhook([]byte(bad)); err == nil {
			t.Errorf("PaymentIDFromWebhook(%q) = nil error, want a rejection", bad)
		}
	}
}

// New returns nil when unconfigured, so callers degrade instead of panicking.
func TestNew_NilWhenUnconfigured(t *testing.T) {
	if New("") != nil {
		t.Fatal("New(\"\") must be nil so the caller can detect 'not configured'")
	}
	if New("test_key") == nil {
		t.Fatal("New with a key must return a client")
	}
}

// stubMollie stands in for the Mollie API: it records the path it was called on
// and replies with a canned status + body. The client is pointed at it via
// NewWithBaseURL, which is the only reason that constructor exists.
func stubMollie(t *testing.T, status int, body string) (*Client, *string) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL("test_key", srv.URL), &gotPath
}

// The reconciler's whole job hangs on reading a schedule's status and next
// charge date back correctly.
func TestGetSubscription_ParsesStatusAndDates(t *testing.T) {
	c, path := stubMollie(t, http.StatusOK, `{
		"id": "sub_abc",
		"status": "active",
		"nextPaymentDate": "2026-09-01",
		"canceledAt": null
	}`)
	sub, err := c.GetSubscription(context.Background(), "cst_1", "sub_abc")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if *path != "/customers/cst_1/subscriptions/sub_abc" {
		t.Fatalf("path = %q", *path)
	}
	if sub.ID != "sub_abc" || sub.Status != "active" {
		t.Fatalf("sub = %+v", sub)
	}
	if !sub.IsActive() || sub.IsDead() {
		t.Fatalf("active subscription must be IsActive and not IsDead: %+v", sub)
	}
	if sub.NextPaymentDate == nil || sub.NextPaymentDate.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("nextPaymentDate = %v, want 2026-09-01", sub.NextPaymentDate)
	}
	if sub.CanceledAt != nil {
		t.Fatalf("canceledAt = %v, want nil", sub.CanceledAt)
	}
}

// A subscription Mollie killed after failed retries arrives exactly like this,
// with no webhook ever announcing it.
func TestGetSubscription_Canceled(t *testing.T) {
	c, _ := stubMollie(t, http.StatusOK, `{
		"id": "sub_dead",
		"status": "canceled",
		"canceledAt": "2026-07-01T10:00:00+00:00"
	}`)
	sub, err := c.GetSubscription(context.Background(), "cst_1", "sub_dead")
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if sub.IsActive() || !sub.IsDead() {
		t.Fatalf("canceled subscription must be IsDead: %+v", sub)
	}
	if sub.CanceledAt == nil || sub.CanceledAt.UTC().Format(time.RFC3339) != "2026-07-01T10:00:00Z" {
		t.Fatalf("canceledAt = %v", sub.CanceledAt)
	}
	if sub.NextPaymentDate != nil {
		t.Fatalf("a dead schedule must carry no next payment date: %v", sub.NextPaymentDate)
	}
}

// 404 means gone for good, and must be distinguishable from a transient fault:
// only the former may revoke a local subscription.
func TestGetSubscription_NotFound(t *testing.T) {
	c, _ := stubMollie(t, http.StatusNotFound, `{"status":404,"title":"Not Found"}`)
	_, err := c.GetSubscription(context.Background(), "cst_1", "sub_gone")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// A 5xx is a Mollie problem, not a verdict on the subscription. It must NOT
// masquerade as ErrNotFound, or an outage would cancel every paying customer.
func TestGetSubscription_ServerErrorIsNotNotFound(t *testing.T) {
	c, _ := stubMollie(t, http.StatusInternalServerError, `{"status":500}`)
	_, err := c.GetSubscription(context.Background(), "cst_1", "sub_x")
	if err == nil {
		t.Fatal("want an error on 500")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("a 500 must not be ErrNotFound: %v", err)
	}
}

// Cancelling something Mollie already dropped is the outcome we wanted, so it
// must not fail — account deletion depends on it.
func TestCancelSubscription_AlreadyGoneIsNotAnError(t *testing.T) {
	c, _ := stubMollie(t, http.StatusNotFound, `{"status":404}`)
	if err := c.CancelSubscription(context.Background(), "cst_1", "sub_gone"); err != nil {
		t.Fatalf("cancel of an already-gone subscription = %v, want nil", err)
	}
}
