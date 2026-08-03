package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func ctx() context.Context { return context.Background() }

// server spins up a stub Lungor, recording what the SDK actually sent.
type recorded struct {
	path   string
	method string
	auth   string
	body   map[string]any
}

func server(t *testing.T, status int, payload any) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.RequestURI()
		rec.method = r.Method
		rec.auth = r.Header.Get("Authorization")
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&rec.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestEntitlement_SendsTheAppKeyAndTheUserID(t *testing.T) {
	srv, rec := server(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := New(srv.URL, "sk_live_app_x")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !got.Entitled || got.Status != "active" {
		t.Fatalf("entitlement = %+v", got)
	}
	if rec.auth != "Bearer sk_live_app_x" {
		t.Fatalf("auth = %q, want a bearer app key", rec.auth)
	}
	if rec.path != "/api/v1/entitlements?external_user_id=user-1" {
		t.Fatalf("path = %q", rec.path)
	}
}

func TestEntitlement_RequestsBalancesForTheNamedUnits(t *testing.T) {
	srv, rec := server(t, 200, Entitlement{
		Entitled: true, Status: "active",
		Balances: map[string]int64{"credit": 2840},
	})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1", "credit", "seat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.path != "/api/v1/entitlements?external_user_id=user-1&units=credit%2Cseat" {
		t.Fatalf("path = %q, want comma-joined units", rec.path)
	}
	if v, ok := got.Balance("credit"); !ok || v != 2840 {
		t.Fatalf("credit balance = (%d, %v), want (2840, true)", v, ok)
	}
	// A unit Lungor said nothing about is UNKNOWN, not zero. Reading it as zero
	// would refuse work the customer is allowed to do.
	if v, ok := got.Balance("seat"); ok {
		t.Fatalf("seat balance = (%d, true), want unknown", v)
	}
}

// A user Lungor has never seen is the common case — most users never subscribe.
// It must be an ordinary answer, not an error.
func TestEntitlement_UnknownUserIsNotAnError(t *testing.T) {
	srv, _ := server(t, 200, Entitlement{Entitled: false, Status: StatusNoSubscription})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "stranger")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got.Entitled || got.Status != StatusNoSubscription {
		t.Fatalf("entitlement = %+v", got)
	}
}

// THE distinction that matters: a rejected app key is an operator mistake. Were
// it reported as "not entitled", a bad key would cut off every paying customer
// at once, silently.
func TestEntitlement_RejectedKeyIsNotAnUnentitledAnswer(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := server(t, status, nil)
		c := New(srv.URL, "wrong")

		_, err := c.Entitlement(ctx(), "user-1")

		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("status %d: err = %v, want ErrUnauthorized", status, err)
		}
	}
}

func TestEntitlement_MapsServerFailuresOntoErrUnavailable(t *testing.T) {
	srv, _ := server(t, http.StatusBadGateway, nil)
	c := New(srv.URL, "k")

	if _, err := c.Entitlement(ctx(), "user-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestEntitlement_RefusesToCallWhenUnconfigured(t *testing.T) {
	if _, err := New("", "k").Entitlement(ctx(), "u"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no base URL: err = %v", err)
	}
	if _, err := New("http://x", "").Entitlement(ctx(), "u"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no app key: err = %v", err)
	}
}

func TestCheckout_SendsTheIdentityLungorVerifies(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay.example/s/1", SessionID: "s1"})
	c := New(srv.URL, "k", WithCheckoutIdentity("tenant-1", "app-1"))

	got, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1",
		Email: "marie@example.fr", SuccessURL: "https://app/ok",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.RedirectURL != "https://pay.example/s/1" {
		t.Fatalf("redirect = %q", got.RedirectURL)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/finance/checkout" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body["tenant_id"] != "tenant-1" || rec.body["app_id"] != "app-1" {
		t.Fatalf("body = %+v, want the checkout identity", rec.body)
	}
	// The amount is never sent: Lungor prices the tier, so the page shown and
	// the amount charged cannot disagree.
	if _, sent := rec.body["amount"]; sent {
		t.Fatal("the SDK must not send an amount")
	}
}

// Failing locally beats sending a request that can only be refused.
func TestCheckout_RefusesWithoutTheIdentity(t *testing.T) {
	srv, rec := server(t, 200, Checkout{})
	c := New(srv.URL, "k") // no WithCheckoutIdentity

	_, err := c.Checkout(ctx(), CheckoutInput{PriceID: "p", ExternalUserID: "u"})

	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	if rec.method != "" {
		t.Fatal("no request should have been sent")
	}
}

func TestCheckout_RefusesIncompleteInput(t *testing.T) {
	srv, _ := server(t, 200, Checkout{})
	c := New(srv.URL, "k", WithCheckoutIdentity("t", "a"))

	if _, err := c.Checkout(ctx(), CheckoutInput{ExternalUserID: "u"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing price: err = %v", err)
	}
	if _, err := c.Checkout(ctx(), CheckoutInput{PriceID: "p"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing user: err = %v", err)
	}
}

func TestClient_HonoursContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	cancelled, cancel := context.WithCancel(ctx())
	cancel()

	if _, err := c.Entitlement(cancelled, "user-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

// A 409 is a customer state — "already subscribed" — not an outage. Folded into
// ErrUnavailable it read as "Lungor is down", so a caller answered 503 to a
// customer who merely clicked subscribe twice, and could retry a request that
// must not be retried.
func TestConflictIsItsOwnErrorNotAnOutage(t *testing.T) {
	srv, _ := server(t, http.StatusConflict, map[string]string{"error": "already subscribed"})
	defer srv.Close()

	c := New(srv.URL, "key", WithCheckoutIdentity("t-1", "a-1"))
	_, err := c.Checkout(ctx(), CheckoutInput{PriceID: "p-1", ExternalUserID: "u-1"})

	if !errors.Is(err, ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Fatal("a conflict must not read as an outage: the caller would retry and answer 503")
	}
}
