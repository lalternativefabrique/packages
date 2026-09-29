package membership

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/busevents"
	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

// DeletionHandler consumes urbangate's account.deletion_requested (its ADR
// 0006) and moves the member to erasing; the worker does the rest. Run it
// with consumer.Run.
func (s *Service) DeletionHandler() consumer.EventHandler {
	return &deletionHandler{s: s}
}

type deletionHandler struct{ s *Service }

func (h *deletionHandler) Name() string { return h.s.p.Name + "-membership-deletion" }
func (h *deletionHandler) Subject() string {
	return busevents.Subject(busevents.Urbangate, busevents.AccountDeletionRequested)
}
func (h *deletionHandler) DurableName() string { return h.s.p.Name + "-membership-deletion" }
func (h *deletionHandler) MaxDeliver() int     { return 5 }

func (h *deletionHandler) Handle(ctx context.Context, msg *nats.Msg) error {
	ev, err := busevents.Decode[busevents.DeletionRequested](msg.Data)
	if err != nil {
		return consumer.Permanent(fmt.Errorf("deletion_requested: %w", err))
	}
	if ev.Product != h.s.p.Name {
		return nil
	}
	if err := h.s.RequestErase(ctx, ev.IdentityID); err != nil {
		return fmt.Errorf("erase %s: %w", ev.IdentityID, err)
	}
	return nil
}
