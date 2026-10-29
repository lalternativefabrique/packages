package outbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/lalternative/packages/go/webhooks/infrastructure/memstore"
	"github.com/lalternativefabrique/eventkit/pkg/consumer"
	"github.com/nats-io/nats.go"
)

const testCatalog = "email.sent"

// seedEndpoint stores a live endpoint and returns its id.
func seedEndpoint(t *testing.T, store *memstore.EventStore) string {
	t.Helper()
	e := aggregate.NewEndpoint("ep-1")
	if err := e.Create("tenant-1", "https://sub.test/hook",
		[]string{testCatalog}, "", events.Catalog{testCatalog}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Save(context.Background(), e); err != nil {
		t.Fatalf("save: %v", err)
	}
	return e.ID
}

func jobMsg(t *testing.T, endpointID string) *nats.Msg {
	t.Helper()
	payload, err := EncodeJob(providers.DeliveryJob{
		DeliveryID:    "d-1",
		EndpointID:    endpointID,
		TenantID:      "tenant-1",
		URL:           "https://sub.test/hook",
		Secret:        "whsec_x",
		EventType:     testCatalog,
		SourceEventID: "src-1",
		Payload:       []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("encode job: %v", err)
	}
	return &nats.Msg{Data: payload}
}

func TestWorkerRecordsSuccess(t *testing.T) {
	store := memstore.NewEventStore()
	id := seedEndpoint(t, store)
	disp := memstore.NewDispatcher(providers.HTTPResult{StatusCode: 200})

	w := NewWorker(store, disp)
	if err := w.Handle(context.Background(), jobMsg(t, id)); err != nil {
		t.Fatalf("handle: %v", err)
	}

	kinds := store.KindsFor(id)
	if !contains(kinds, events.DeliverySucceededType) {
		t.Fatalf("no success recorded, got %v", kinds)
	}
	if !contains(kinds, events.DeliveryAttemptedType) {
		t.Fatalf("no attempt recorded, got %v", kinds)
	}
}

// A 4xx is the subscriber refusing the event. Retrying wastes a day of
// redeliveries on an answer that will not change, so it must terminate now.
func TestWorkerTerminatesOnPermanentFailure(t *testing.T) {
	store := memstore.NewEventStore()
	id := seedEndpoint(t, store)
	disp := memstore.NewDispatcher(providers.HTTPResult{
		StatusCode: 400, Err: errors.New("http 400"),
	})

	w := NewWorker(store, disp)
	err := w.Handle(context.Background(), jobMsg(t, id))
	if err == nil {
		t.Fatal("expected an error for a 4xx")
	}
	if !errors.Is(err, consumer.ErrPermanent) {
		t.Fatalf("expected a permanent error, got %v", err)
	}
	if kinds := store.KindsFor(id); !contains(kinds, events.DeliveryFailedType) {
		t.Fatalf("no failure recorded, got %v", kinds)
	}
}

// A 5xx is the subscriber being briefly unavailable: it must retry, and must
// NOT be recorded as a terminal failure — the delivery is still in flight.
func TestWorkerRetriesOnTransientFailure(t *testing.T) {
	store := memstore.NewEventStore()
	id := seedEndpoint(t, store)
	disp := memstore.NewDispatcher(providers.HTTPResult{
		StatusCode: 503, Err: errors.New("http 503"),
	})

	w := NewWorker(store, disp)
	err := w.Handle(context.Background(), jobMsg(t, id))
	if err == nil {
		t.Fatal("expected an error for a 5xx")
	}
	if errors.Is(err, consumer.ErrPermanent) {
		t.Fatal("a 5xx must retry, not terminate")
	}
	if kinds := store.KindsFor(id); contains(kinds, events.DeliveryFailedType) {
		t.Fatalf("a retryable delivery must not be marked failed, got %v", kinds)
	}
}

// An endpoint deleted while its job was queued must not resurrect: the worker
// records nothing and does not fail the message.
func TestWorkerSkipsVanishedEndpoint(t *testing.T) {
	store := memstore.NewEventStore()
	disp := memstore.NewDispatcher(providers.HTTPResult{StatusCode: 200})

	w := NewWorker(store, disp)
	if err := w.Handle(context.Background(), jobMsg(t, "never-existed")); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if kinds := store.KindsFor("never-existed"); len(kinds) != 0 {
		t.Fatalf("recorded events against a missing endpoint: %v", kinds)
	}
}

func TestWorkerRejectsUndecodableJob(t *testing.T) {
	w := NewWorker(memstore.NewEventStore(), memstore.NewDispatcher())
	err := w.Handle(context.Background(), &nats.Msg{Data: []byte("not json")})
	if !errors.Is(err, consumer.ErrPermanent) {
		t.Fatalf("a malformed job must terminate, got %v", err)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestWorkerLogsAbandonedDelivery(t *testing.T) {
	store := memstore.NewEventStore()
	id := seedEndpoint(t, store)
	disp := memstore.NewDispatcher(providers.HTTPResult{StatusCode: 401, Err: errors.New("http 401")})
	var buf bytes.Buffer
	w := NewWorker(store, disp)
	w.logger = slog.New(slog.NewTextHandler(&buf, nil))

	_ = w.Handle(context.Background(), jobMsg(t, id))

	out := buf.String()
	for _, want := range []string{"level=ERROR", "endpoint_id=" + id, "tenant_id=tenant-1", "event_type=" + testCatalog, "source_event_id=src-1", "status_code=401", "attempts=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("log lacks %q: %s", want, out)
		}
	}
}

func TestWorkerDoesNotLogAbandonOnTransientFailure(t *testing.T) {
	store := memstore.NewEventStore()
	id := seedEndpoint(t, store)
	disp := memstore.NewDispatcher(providers.HTTPResult{StatusCode: 503, Err: errors.New("http 503")})
	var buf bytes.Buffer
	w := NewWorker(store, disp)
	w.logger = slog.New(slog.NewTextHandler(&buf, nil))

	_ = w.Handle(context.Background(), jobMsg(t, id))

	if buf.Len() != 0 {
		t.Fatalf("unexpected log: %s", buf.String())
	}
}
