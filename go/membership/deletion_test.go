package membership_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/busevents"
	"github.com/lalternative/packages/go/eda/pkg/consumer"
	"github.com/lalternative/packages/go/eda/pkg/consumer/edatest"
	"github.com/lalternative/packages/go/membership"
)

func deletion(t *testing.T, identityID, product string) *nats.Msg {
	t.Helper()
	body, _, err := busevents.Encode(busevents.DeletionRequested{EventID: uuid.NewString(), IdentityID: identityID, Product: product})
	if err != nil {
		t.Fatal(err)
	}
	return &nats.Msg{Subject: busevents.Subject(busevents.Urbangate, busevents.AccountDeletionRequested), Data: body}
}

func TestDeletionHandlerErasesOnlyThisProduct(t *testing.T) {
	svc, store := newService(t, nil)
	ctx := context.Background()
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"})
	h := svc.DeletionHandler()
	edatest.VerifyHandlers(t, h)

	if err := h.Handle(ctx, deletion(t, "id-1", "partage")); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, "id-1"); m.State != membership.StateProvisioning {
		t.Fatalf("a partage deletion touched the messag member: %s", m.State)
	}
	if err := h.Handle(ctx, deletion(t, "id-1", "messag")); err != nil {
		t.Fatal(err)
	}
	if m, _ := store.Get(ctx, "id-1"); m.State != membership.StateErasing {
		t.Fatalf("state %s, want erasing", m.State)
	}
	if err := h.Handle(ctx, deletion(t, "id-1", "messag")); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	if err := h.Handle(ctx, &nats.Msg{Data: []byte(`{"event_id":"e","product":"messag"}`)}); !errors.Is(err, consumer.ErrPermanent) {
		t.Fatalf("a payload without identity must be terminated, got %v", err)
	}
	_ = time.Now
}
