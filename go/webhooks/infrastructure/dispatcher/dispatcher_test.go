package dispatcher

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lalternative/packages/go/webhooks/domain/repository"
	"github.com/lalternative/packages/go/webhooks/infrastructure/memstore"
	"github.com/nats-io/nats.go"
)

func newDispatcher(src Source) (*Dispatcher, *memstore.Outbox, *memstore.Lookup) {
	out, lookup := memstore.NewOutbox(), memstore.NewLookup()
	lookup.Add(repository.ActiveEndpoint{
		ID: "ep-1", TenantID: "scope-1", URL: "https://sub.test/hook",
		Secret: "whsec_x", EventTypes: []string{"public.thing"},
	})
	return &Dispatcher{lookup: lookup, outbox: out, source: src}, out, lookup
}

// A publisher that sets no Event-Type header — the subject already carries the
// type — must still be routed. Without the fallback every one of its events is
// dropped in silence.
func TestFallsBackToSubjectWhenHeaderAbsent(t *testing.T) {
	d, out, _ := newDispatcher(Source{
		PublicType: func(u string) string {
			if u == "finance.subscription.renewed" {
				return "public.thing"
			}
			return ""
		},
		Envelope: func(raw []byte) (Envelope, error) {
			return Envelope{Scope: "scope-1", EventID: "e-1"}, nil
		},
	})

	msg := &nats.Msg{
		Subject: "finance.subscription.renewed",
		Data:    []byte(`{"AppID":"scope-1"}`),
		Header:  nats.Header{},
	}
	if err := d.handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := len(out.Jobs()); got != 1 {
		t.Fatalf("published %d jobs, want 1 — the subject fallback did not fire", got)
	}
}

// The header still wins when present, so a product that sets one keeps its
// mapping even if the subject differs.
func TestHeaderWinsOverSubject(t *testing.T) {
	var asked string
	d, _, _ := newDispatcher(Source{
		PublicType: func(u string) string { asked = u; return "" },
	})
	h := nats.Header{}
	h.Set("Event-Type", "message-sent")
	_ = d.handle(context.Background(), &nats.Msg{
		Subject: "message.sent", Data: []byte(`{}`), Header: h,
	})
	if asked != "message-sent" {
		t.Fatalf("PublicType asked about %q, want the header value", asked)
	}
}

// A flat event body with a product-specific scope key routes through a custom
// Envelope. The default reader would find no tenantId and drop it.
func TestCustomEnvelopeRoutesFlatEvents(t *testing.T) {
	d, out, _ := newDispatcher(Source{
		PublicType: func(string) string { return "public.thing" },
		Envelope: func(raw []byte) (Envelope, error) {
			var e struct {
				AppID      string    `json:"AppID"`
				OccurredAt time.Time `json:"OccurredAt"`
			}
			if err := json.Unmarshal(raw, &e); err != nil {
				return Envelope{}, err
			}
			return Envelope{Scope: e.AppID, EventID: "src-1", Timestamp: e.OccurredAt}, nil
		},
	})

	err := d.handle(context.Background(), &nats.Msg{
		Subject: "finance.x", Data: []byte(`{"AppID":"scope-1"}`), Header: nats.Header{},
	})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	jobs := out.Jobs()
	if len(jobs) != 1 {
		t.Fatalf("published %d jobs, want 1", len(jobs))
	}
	if jobs[0].EndpointID != "ep-1" {
		t.Fatalf("job routed to %q", jobs[0].EndpointID)
	}
}

func TestDefaultEnvelopeReadsMetadataShape(t *testing.T) {
	d, out, _ := newDispatcher(Source{
		PublicType: func(string) string { return "public.thing" },
	})
	body := []byte(`{"metadata":{"eventId":"e-9","tenantId":"scope-1","timestamp":"2026-01-01T00:00:00Z"}}`)
	if err := d.handle(context.Background(), &nats.Msg{
		Subject: "message.sent", Data: body, Header: nats.Header{},
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	jobs := out.Jobs()
	if len(jobs) != 1 || jobs[0].SourceEventID != "e-9" {
		t.Fatalf("default envelope did not route the metadata shape: %+v", jobs)
	}
}

// An event nobody can be reached for is dropped, not published to no one.
func TestUnknownScopeIsDropped(t *testing.T) {
	d, out, _ := newDispatcher(Source{
		PublicType: func(string) string { return "public.thing" },
		Envelope: func([]byte) (Envelope, error) {
			return Envelope{Scope: "someone-else", EventID: "e-1"}, nil
		},
	})
	if err := d.handle(context.Background(), &nats.Msg{
		Subject: "x", Data: []byte(`{}`), Header: nats.Header{},
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := len(out.Jobs()); got != 0 {
		t.Fatalf("published %d jobs for an unknown scope", got)
	}
}

// An endpoint only receives the types it registered for.
func TestUnsubscribedTypeIsNotDelivered(t *testing.T) {
	d, out, _ := newDispatcher(Source{
		PublicType: func(string) string { return "public.other" },
		Envelope: func([]byte) (Envelope, error) {
			return Envelope{Scope: "scope-1", EventID: "e-1"}, nil
		},
	})
	if err := d.handle(context.Background(), &nats.Msg{
		Subject: "x", Data: []byte(`{}`), Header: nats.Header{},
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if got := len(out.Jobs()); got != 0 {
		t.Fatalf("delivered %d jobs for a type the endpoint never registered for", got)
	}
}

// Replays must reuse the delivery id rather than fan out a duplicate.
func TestDeliveryIDIsDeterministic(t *testing.T) {
	src := Source{
		PublicType: func(string) string { return "public.thing" },
		Envelope: func([]byte) (Envelope, error) {
			return Envelope{Scope: "scope-1", EventID: "e-1"}, nil
		},
	}
	d, out, _ := newDispatcher(src)
	msg := &nats.Msg{Subject: "x", Data: []byte(`{}`), Header: nats.Header{}}
	_ = d.handle(context.Background(), msg)
	_ = d.handle(context.Background(), msg)

	jobs := out.Jobs()
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs", len(jobs))
	}
	if jobs[0].DeliveryID != jobs[1].DeliveryID {
		t.Fatal("a replayed event produced a different delivery id")
	}
}
