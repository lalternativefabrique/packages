package kvstore

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

type endpoint struct {
	ID       string
	TenantID string
	Status   string
}

func TestProjector_EventHandlerContract(t *testing.T) {
	p := NewProjector[string, endpoint](nil, ProjectorConfig[string, endpoint]{
		Name:    "webhooks-kv",
		Subject: "webhook.>",
		Durable: "webhooks-kv-readmodel",
	})
	if p.Name() != "webhooks-kv" {
		t.Errorf("Name = %q", p.Name())
	}
	if p.Subject() != "webhook.>" {
		t.Errorf("Subject = %q", p.Subject())
	}
	if p.DurableName() != "webhooks-kv-readmodel" {
		t.Errorf("DurableName = %q", p.DurableName())
	}
	if p.MaxDeliver() != 5 {
		t.Errorf("MaxDeliver default = %d, want 5", p.MaxDeliver())
	}
	// Compile-time: a Projector is a consumer.EventHandler.
	var _ consumer.EventHandler = p
}

func TestProjector_MaxDeliverOverride(t *testing.T) {
	p := NewProjector[string, endpoint](nil, ProjectorConfig[string, endpoint]{
		Name: "x", Subject: "y", Durable: "z", MaxDeliver: 9,
	})
	if p.MaxDeliver() != 9 {
		t.Errorf("MaxDeliver = %d, want 9", p.MaxDeliver())
	}
}

func TestProjector_Routing(t *testing.T) {
	// A Projector calls store.Put / store.Delete. We can't inject an interface
	// (Store is concrete), so we validate the mutation the Project func yields
	// for each event kind — the decision logic — which is what an app owns.
	project := func(_ context.Context, msg *nats.Msg) (Mutation[string, endpoint], error) {
		switch msg.Header.Get("Event-Type") {
		case "created":
			return Put("e1", endpoint{ID: "e1", TenantID: "t1", Status: "active"}), nil
		case "deleted":
			return Delete[string, endpoint]("e1"), nil
		case "ignored":
			return Noop[string, endpoint](), nil
		default:
			return Noop[string, endpoint](), consumer.Permanent(errors.New("unknown event"))
		}
	}

	cases := []struct {
		evt     string
		wantOp  mutationOp
		wantErr bool
	}{
		{"created", opPut, false},
		{"deleted", opDelete, false},
		{"ignored", opNoop, false},
		{"garbage", opNoop, true},
	}
	for _, tc := range cases {
		t.Run(tc.evt, func(t *testing.T) {
			msg := &nats.Msg{Header: nats.Header{"Event-Type": []string{tc.evt}}}
			m, err := project(context.Background(), msg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tc.evt)
				}
				if !errors.Is(err, consumer.ErrPermanent) {
					t.Errorf("error should be permanent for undecodable event")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.op != tc.wantOp {
				t.Errorf("op = %d, want %d", m.op, tc.wantOp)
			}
		})
	}
}

func TestMutationConstructors(t *testing.T) {
	put := Put("k", endpoint{ID: "k"})
	if put.op != opPut || put.key != "k" || put.value.ID != "k" {
		t.Errorf("Put constructed wrong: %+v", put)
	}
	del := Delete[string, endpoint]("k")
	if del.op != opDelete || del.key != "k" {
		t.Errorf("Delete constructed wrong: %+v", del)
	}
	noop := Noop[string, endpoint]()
	if noop.op != opNoop {
		t.Errorf("Noop constructed wrong: %+v", noop)
	}
}
