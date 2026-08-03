package sdk_test

import (
	"context"
	"errors"
	"os"
	"testing"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

// Exercised against a real Lungor, which is the only thing that distinguishes
// "the SDK sends what the contract describes" from "Lungor answers it".
// Everything else in this package tests the former against a stub written from
// the same contract, so a contract that disagrees with the server passes both.
//
//	LUNGOR_LIVE_URL=http://127.0.0.1:4100 LUNGOR_LIVE_KEY=sk_live_app_... go test -run TestLive ./...
func liveClient(t *testing.T) *sdk.Client {
	t.Helper()
	base, key := os.Getenv("LUNGOR_LIVE_URL"), os.Getenv("LUNGOR_LIVE_KEY")
	if base == "" || key == "" {
		t.Skip("set LUNGOR_LIVE_URL and LUNGOR_LIVE_KEY to run against a real Lungor")
	}
	return sdk.New(base, key)
}

// The generated transport builds every path; this is where a wrong one stops
// being a passing test and becomes a 404.
func TestLiveWebhookEndpointLifecycle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	created, err := c.CreateWebhookEndpoint(ctx, sdk.CreateWebhookEndpointInput{
		URL:         "https://example.com/hooks/lungor",
		EventTypes:  []string{sdk.EventSubscriptionActivated, sdk.EventSubscriptionPastDue},
		Description: "sdk live test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = c.DeleteWebhookEndpoint(ctx, created.ID) })

	if created.ID == "" {
		t.Fatal("created endpoint has no id")
	}
	// Lungor returns the secret once, on creation. An empty one here means the
	// caller has no way to verify a delivery it later receives.
	if created.Secret == "" {
		t.Error("create returned no signing secret")
	}
	if !created.Active() {
		t.Errorf("status = %q, want active", created.Status)
	}

	got, err := c.GetWebhookEndpoint(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.URL != "https://example.com/hooks/lungor" {
		t.Errorf("url = %q", got.URL)
	}
	if len(got.EventTypes) != 2 {
		t.Errorf("event types = %v, want the two registered", got.EventTypes)
	}
	if got.CreatedAt.IsZero() {
		t.Error("createdAt did not decode")
	}

	list, err := c.ListWebhookEndpoints(ctx, 0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !containsID(list, created.ID) {
		t.Errorf("created endpoint absent from list of %d", len(list))
	}

	disabled := true
	if err := c.UpdateWebhookEndpoint(ctx, created.ID, sdk.UpdateWebhookEndpointInput{Disabled: &disabled}); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, err := c.GetWebhookEndpoint(ctx, created.ID)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if after.Active() {
		t.Errorf("status = %q after disabling, want disabled", after.Status)
	}

	rotated, err := c.RotateWebhookSecret(ctx, created.ID)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated == "" {
		t.Error("rotate returned no secret")
	}
	if rotated == created.Secret {
		t.Error("rotate returned the previous secret")
	}

	if err := c.DeleteWebhookEndpoint(ctx, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := c.GetWebhookEndpoint(ctx, created.ID); !errors.Is(err, sdk.ErrNotFound) {
		t.Errorf("after delete, get returned %v, want ErrNotFound", err)
	}
}

// A user Lungor has never seen is an ordinary answer, not a failure — the
// distinction every consumer depends on to decide whether to degrade.
func TestLiveEntitlementForUnknownUser(t *testing.T) {
	c := liveClient(t)

	ent, err := c.Entitlement(context.Background(), "user-that-never-subscribed")
	if err != nil {
		t.Fatalf("entitlement: %v", err)
	}
	if ent.Entitled {
		t.Error("an unknown user came back entitled")
	}
	if ent.Status != sdk.StatusNoSubscription {
		t.Errorf("status = %q, want %q", ent.Status, sdk.StatusNoSubscription)
	}
}

// A rejected key must not read as "not entitled": that mapping cuts off every
// paying user the moment a key is misconfigured.
func TestLiveBadKeyIsUnauthorized(t *testing.T) {
	base := os.Getenv("LUNGOR_LIVE_URL")
	if base == "" {
		t.Skip("set LUNGOR_LIVE_URL to run against a real Lungor")
	}

	c := sdk.New(base, "sk_live_app_definitelynotarealkey000000000000")
	if _, err := c.Entitlement(context.Background(), "u"); !errors.Is(err, sdk.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func containsID(list []sdk.WebhookEndpoint, id string) bool {
	for _, e := range list {
		if e.ID == id {
			return true
		}
	}
	return false
}
