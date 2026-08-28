package application

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/reminder/domain"
)

// queueGroup names the NATS queue group every replica's dispatcher joins, so
// a reminder that fires while several core instances are running is
// delivered exactly once, not once per replica.
const queueGroup = "reminder-dispatcher"

// Dispatcher delivers a fired reminder through its configured channels.
// A reminder with no channels reaches nobody here — the API/chat surface is
// still the way to see it.
type Dispatcher struct {
	channels map[string]domain.Channel
}

func NewDispatcher(channels ...domain.Channel) *Dispatcher {
	byType := make(map[string]domain.Channel, len(channels))
	for _, c := range channels {
		byType[c.Type()] = c
	}
	return &Dispatcher{channels: byType}
}

// Start subscribes to SubjectReminderDue and delivers every event received
// until ctx is cancelled.
func (d *Dispatcher) Start(ctx context.Context, nc *nats.Conn) error {
	sub, err := nc.QueueSubscribe(SubjectReminderDue, queueGroup, func(msg *nats.Msg) {
		d.handle(ctx, msg.Data)
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = sub.Unsubscribe()
	}()
	slog.Info("reminder: dispatcher started", "queue_group", queueGroup, "channels", len(d.channels))
	return nil
}

func (d *Dispatcher) handle(ctx context.Context, data []byte) {
	var evt DueEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		slog.Error("reminder: decode due event", "error", err)
		return
	}
	for _, cfg := range evt.Channels {
		ch, ok := d.channels[cfg.Type]
		if !ok {
			slog.Warn("reminder: no channel registered", "type", cfg.Type, "reminder_id", evt.ReminderID)
			continue
		}
		if err := ch.Send(ctx, "Reminder", evt.Body, cfg.Target); err != nil {
			slog.Error("reminder: deliver", "type", cfg.Type, "reminder_id", evt.ReminderID, "error", err)
		}
	}
}
