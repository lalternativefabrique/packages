package update_endpoint

import (
	"context"
	"testing"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/infrastructure/memstore"
)

const (
	tenant = "app-1"
	id     = "ep-1"
)

var catalog = events.Catalog{"subscription.activated", "subscription.renewed"}

func seed(t *testing.T) (*memstore.EventStore, *Handler) {
	t.Helper()
	store := memstore.NewEventStore()

	e := aggregate.NewEndpoint(id)
	if err := e.Create(tenant, "https://app.example/hooks", []string{"subscription.activated"}, "original", catalog); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	if err := store.Save(context.Background(), e); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	return store, NewHandler(store, catalog)
}

func load(t *testing.T, store *memstore.EventStore) *aggregate.Endpoint {
	t.Helper()
	e, err := store.Load(context.Background(), id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return e
}

// Disabling an endpoint is the one update a caller makes without touching
// anything else, and it was rejected outright: with plain-value fields an
// omitted url arrived as "", which validation refused as missing.
func TestDisableOnlyLeavesTheDefinitionAlone(t *testing.T) {
	store, h := seed(t)
	disabled := true

	if err := h.Handle(context.Background(), Command{TenantID: tenant, ID: id, Disabled: &disabled}); err != nil {
		t.Fatalf("disable: %v", err)
	}

	got := load(t, store)
	if got.Status != aggregate.StatusDisabled {
		t.Errorf("status = %q, want disabled", got.Status)
	}
	if got.URL != "https://app.example/hooks" {
		t.Errorf("url = %q — a pause wiped the destination", got.URL)
	}
	if len(got.EventTypes) != 1 || got.EventTypes[0] != "subscription.activated" {
		t.Errorf("event types = %v — a pause wiped the subscription", got.EventTypes)
	}
	if got.Description != "original" {
		t.Errorf("description = %q, want it untouched", got.Description)
	}
}

func TestReEnableRestoresDelivery(t *testing.T) {
	store, h := seed(t)
	ctx := context.Background()
	yes, no := true, false

	if err := h.Handle(ctx, Command{TenantID: tenant, ID: id, Disabled: &yes}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := h.Handle(ctx, Command{TenantID: tenant, ID: id, Disabled: &no}); err != nil {
		t.Fatalf("re-enable: %v", err)
	}

	if got := load(t, store); got.Status != aggregate.StatusActive {
		t.Errorf("status = %q, want active", got.Status)
	}
}

// A field named alone must move alone. Filling the others from a zero value
// rather than from current state is what turned a rename into a wipe.
func TestUpdatingOneFieldKeepsTheRest(t *testing.T) {
	store, h := seed(t)
	url := "https://app.example/hooks/v2"

	if err := h.Handle(context.Background(), Command{TenantID: tenant, ID: id, URL: &url}); err != nil {
		t.Fatalf("update url: %v", err)
	}

	got := load(t, store)
	if got.URL != url {
		t.Errorf("url = %q, want %q", got.URL, url)
	}
	if got.Description != "original" {
		t.Errorf("description = %q, want it untouched", got.Description)
	}
	if len(got.EventTypes) != 1 {
		t.Errorf("event types = %v, want them untouched", got.EventTypes)
	}
	if got.Status != aggregate.StatusActive {
		t.Errorf("status = %q — updating a url paused delivery", got.Status)
	}
}

// Clearing a field is a caller saying so, not a caller staying silent — the
// distinction plain values cannot express.
func TestEmptyDescriptionClearsIt(t *testing.T) {
	store, h := seed(t)
	empty := ""

	if err := h.Handle(context.Background(), Command{TenantID: tenant, ID: id, Description: &empty}); err != nil {
		t.Fatalf("clear description: %v", err)
	}

	if got := load(t, store); got.Description != "" {
		t.Errorf("description = %q, want cleared", got.Description)
	}
}

// An endpoint belonging to another app must be indistinguishable from one that
// does not exist.
func TestAnotherTenantSeesNothing(t *testing.T) {
	_, h := seed(t)
	disabled := true

	err := h.Handle(context.Background(), Command{TenantID: "app-2", ID: id, Disabled: &disabled})
	if err == nil {
		t.Fatal("another app updated an endpoint it does not own")
	}
}
