package sdk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// apiRoot is the last hand-written string in a request path: the generated
// transport owns everything after it, but nothing checks the version segment
// itself against the contract.
//
// Asserted against a real request rather than against the constant, so it holds
// whatever the transport does when joining the two.
func TestAPIRootKeepsTheVersionSegment(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_live_app_x")
	if _, err := c.Entitlement(ctx(), "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != "/api/v1/entitlements" {
		t.Errorf("requested %q, want /api/v1/entitlements", got)
	}
}

// A base URL the caller ended with a slash must not double it: "//api/v1/..."
// is a different path to most routers and 404s on some.
func TestTrailingSlashInBaseURLIsAbsorbed(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "sk_live_app_x")
	if _, err := c.Entitlement(ctx(), "user-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(got, "//") {
		t.Errorf("requested %q, which doubles a slash", got)
	}
	if got != "/api/v1/entitlements" {
		t.Errorf("requested %q, want /api/v1/entitlements", got)
	}
}

// Every operation must reach the versioned root, not just the one above: the
// prefix is applied in a single place now, so a regression there breaks all of
// them at once and this is what says so.
func TestEveryOperationIsVersioned(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "sk_live_app_x", WithCheckoutIdentity("t_1", "a_1"))

	for _, tc := range []struct {
		name string
		call func() error
		want string
	}{
		{"entitlement", func() error { _, err := c.Entitlement(ctx(), "u"); return err }, "/api/v1/entitlements"},
		{"checkout", func() error {
			_, err := c.Checkout(ctx(), CheckoutInput{PriceID: "p", ExternalUserID: "u"})
			return err
		}, "/api/v1/finance/checkout"},
		{"cancel", func() error { _, err := c.Cancel(ctx(), "u", true); return err }, "/api/v1/subscriptions/cancel"},
		{"change plan", func() error {
			_, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u", PlanCode: "max"})
			return err
		}, "/api/v1/subscriptions/change-plan"},
		{"withdraw pending", func() error {
			_, _, err := c.WithdrawPendingPlan(ctx(), "u")
			return err
		}, "/api/v1/subscriptions/withdraw-pending-plan"},
		{"list endpoints", func() error { _, err := c.ListWebhookEndpoints(ctx(), 0, 0); return err }, "/api/v1/webhooks/endpoints"},
		{"create endpoint", func() error {
			_, err := c.CreateWebhookEndpoint(ctx(), CreateWebhookEndpointInput{URL: "https://e/h", EventTypes: []string{EventSubscriptionActivated}})
			return err
		}, "/api/v1/webhooks/endpoints"},
		{"get endpoint", func() error { _, err := c.GetWebhookEndpoint(ctx(), "ep_1"); return err }, "/api/v1/webhooks/endpoints/ep_1"},
		{"update endpoint", func() error {
			return c.UpdateWebhookEndpoint(ctx(), "ep_1", UpdateWebhookEndpointInput{})
		}, "/api/v1/webhooks/endpoints/ep_1"},
		{"delete endpoint", func() error { return c.DeleteWebhookEndpoint(ctx(), "ep_1") }, "/api/v1/webhooks/endpoints/ep_1"},
		{"rotate secret", func() error { _, err := c.RotateWebhookSecret(ctx(), "ep_1"); return err }, "/api/v1/webhooks/endpoints/ep_1/rotate-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got = ""
			if err := tc.call(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("requested %q, want %q", got, tc.want)
			}
		})
	}
}
