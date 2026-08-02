package aggregate

import (
	"testing"

	"github.com/lalternative/packages/go/webhooks/domain/events"
)

var billingCatalog = events.Catalog{"subscription.renewed", "subscription.canceled"}

func TestCreateAcceptsCatalogedTypes(t *testing.T) {
	e := NewEndpoint("id-1")
	err := e.Create("tenant-1", "https://example.com/hook",
		[]string{"subscription.renewed"}, "", billingCatalog)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(e.EventTypes) != 1 || e.EventTypes[0] != "subscription.renewed" {
		t.Fatalf("event types = %v", e.EventTypes)
	}
}

// A type outside the catalog must be refused rather than stored: an endpoint
// subscribed to an event nobody emits waits forever, and nothing reports it.
func TestCreateRejectsUncatalogedType(t *testing.T) {
	e := NewEndpoint("id-2")
	err := e.Create("tenant-1", "https://example.com/hook",
		[]string{"email.sent"}, "", billingCatalog)
	if err == nil {
		t.Fatal("expected an error for a type outside the catalog")
	}
}

// The catalog is per-product, so one product's types must not be accepted by
// another's. This is the regression the extraction could have introduced.
func TestCatalogIsPerProduct(t *testing.T) {
	mail := events.Catalog{"email.sent"}
	if mail.Allows("subscription.renewed") {
		t.Fatal("mail catalog accepted a billing type")
	}
	if !mail.Allows("email.sent") {
		t.Fatal("mail catalog rejected its own type")
	}
}

func TestEmptyCatalogRejectsEverything(t *testing.T) {
	e := NewEndpoint("id-3")
	err := e.Create("tenant-1", "https://example.com/hook",
		[]string{"anything"}, "", nil)
	if err == nil {
		t.Fatal("expected an error: a service declaring no events can register none")
	}
}
