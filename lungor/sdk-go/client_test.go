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

func TestCheckout_SendsNoIdentityTheKeyAlreadyProves(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay.example/s/1", SessionID: "s1"})
	c := New(srv.URL, "k")

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
	// Neither the app nor the tenant is sent: both are properties the API key
	// already proves. Stating either would only give a caller a value to copy
	// wrong, and a wrong one fails while a customer is trying to pay.
	for _, field := range []string{"app_id", "tenant_id"} {
		if _, sent := rec.body[field]; sent {
			t.Fatalf("the SDK must not send %s", field)
		}
	}
	// The amount is never sent: Lungor prices the tier, so the page shown and
	// the amount charged cannot disagree.
	if _, sent := rec.body["amount"]; sent {
		t.Fatal("the SDK must not send an amount")
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

// The deprecated option still compiles and still configures the app, so a
// caller upgrading the SDK is not broken by the rename.
func TestDeprecatedIdentityOptions_AreNoOps(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay.example/s/1"})
	c := New(srv.URL, "k", WithCheckoutIdentity("tenant-ignored", "app-ignored"))

	if _, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, field := range []string{"app_id", "tenant_id"} {
		if _, sent := rec.body[field]; sent {
			t.Fatalf("the deprecated option still sent %s", field)
		}
	}
}

// The plan code is what a caller matches on to know WHICH tier it is serving.
// Without it, every consumer keeps its own table of what each tier grants, and
// that copy is a second authority on the catalogue.
func TestEntitlement_ReportsTheSubscribedPlan(t *testing.T) {
	rank := 2
	srv, _ := server(t, 200, Entitlement{
		Entitled: true, Status: "active",
		PlanCode: "pro", PlanRank: &rank,
	})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PlanCode != "pro" {
		t.Fatalf("plan code = %q, want pro", got.PlanCode)
	}
	if got.PlanRank == nil || *got.PlanRank != 2 {
		t.Fatalf("plan rank = %v, want 2", got.PlanRank)
	}
}

// Rank 0 is a real rank — the smallest tier — and not a missing one. Flattening
// the pointer would report the smallest tier for a plan Lungor never named.
func TestEntitlement_ZeroRankIsNotAMissingRank(t *testing.T) {
	zero := 0
	srv, _ := server(t, 200, Entitlement{
		Entitled: true, Status: "active",
		PlanCode: "free", PlanRank: &zero,
	})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.PlanRank == nil || *got.PlanRank != 0 {
		t.Fatalf("plan rank = %v, want a present 0", got.PlanRank)
	}
}

// An older Lungor names no plan. The verdict still has to arrive, and the plan
// has to read as unknown rather than as the smallest tier.
func TestEntitlement_UnnamedPlanLeavesRankUnknown(t *testing.T) {
	srv, _ := server(t, 200, Entitlement{Entitled: true, Status: "active"})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Entitled {
		t.Fatalf("entitlement = %+v, want entitled", got)
	}
	if got.PlanCode != "" || got.PlanRank != nil {
		t.Fatalf("plan = (%q, %v), want unknown", got.PlanCode, got.PlanRank)
	}
}

// The date reaches a consuming app through the subscription webhook and nowhere
// else, so carrying it here is what lets one answer "active until" without
// persisting a projection a missed delivery leaves stale.
func TestEntitlement_CarriesWhenThePaidPeriodEnds(t *testing.T) {
	end := time.Date(2026, 9, 18, 10, 30, 0, 0, time.UTC)
	srv, _ := server(t, 200, map[string]any{
		"entitled": true, "status": "active",
		"current_period_end": end.Format(time.RFC3339),
	})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentPeriodEnd == nil {
		t.Fatal("no period end reported")
	}
	if !got.CurrentPeriodEnd.Equal(end) {
		t.Fatalf("period end = %s, want %s", got.CurrentPeriodEnd, end)
	}
}

// A user with no subscription has no paid period. Nil, not a zero time, which
// would read as access having ended in year one.
func TestEntitlement_NoSubscriptionCarriesNoPeriod(t *testing.T) {
	srv, _ := server(t, 200, Entitlement{Entitled: false, Status: StatusNoSubscription})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "stranger")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.CurrentPeriodEnd != nil {
		t.Fatalf("period end = %v, want none", got.CurrentPeriodEnd)
	}
}

// The verdict is what the caller asked for and it is settled before the date is
// read, so a value that cannot be parsed must not take the answer down with it.
func TestEntitlement_UnparseablePeriodStillAnswers(t *testing.T) {
	srv, _ := server(t, 200, map[string]any{
		"entitled": true, "status": "active", "current_period_end": "not-a-date",
	})
	c := New(srv.URL, "k")

	got, err := c.Entitlement(ctx(), "user-1")
	if err != nil {
		t.Fatalf("err = %v, want the verdict to survive", err)
	}
	if !got.Entitled {
		t.Fatal("the verdict was lost with the date")
	}
	if got.CurrentPeriodEnd != nil {
		t.Fatalf("period end = %v, want none", got.CurrentPeriodEnd)
	}
}
