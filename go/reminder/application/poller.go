// Package application delivers reminders whose time has come.
package application

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/reminder/domain"
)

// SubjectReminderDue carries a reminder at the moment it comes due, so
// anything that can reach the person — a websocket, a mail, a chat — can pick
// it up without the poller knowing they exist.
const SubjectReminderDue = "reminder.due"

// DueEvent is what is published when a reminder fires.
type DueEvent struct {
	ReminderID string                 `json:"reminder_id"`
	UserID     string                 `json:"user_id"`
	Body       string                 `json:"body"`
	DueAt      time.Time              `json:"due_at"`
	Channels   []domain.ChannelConfig `json:"channels,omitempty"`
}

// PollerConfig parameterises the delivery loop.
type PollerConfig struct {
	Reminders domain.Repository
	Nats      *nats.Conn
	// Interval is how often the queue is checked. Zero means defaultInterval.
	// It is also the worst-case lateness of a reminder.
	Interval time.Duration
	// Batch caps how many are delivered per tick. Zero means defaultBatch.
	Batch int
}

const (
	defaultInterval = 15 * time.Second
	defaultBatch    = 50
)

// Poller delivers reminders that have come due.
type Poller struct {
	cfg PollerConfig
}

func NewPoller(cfg PollerConfig) *Poller {
	if cfg.Interval == 0 {
		cfg.Interval = defaultInterval
	}
	if cfg.Batch == 0 {
		cfg.Batch = defaultBatch
	}
	return &Poller{cfg: cfg}
}

// Start delivers due reminders until ctx is cancelled.
//
// Anything that came due while the service was down is delivered on the first
// tick: the claim asks for what is due, not for what became due since.
func (p *Poller) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(p.cfg.Interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.tick(ctx)
			}
		}
	}()
	slog.Info("reminder: poller started", "interval", p.cfg.Interval)
}

func (p *Poller) tick(ctx context.Context) {
	due, err := p.cfg.Reminders.ClaimDue(ctx, p.cfg.Batch)
	if err != nil {
		slog.Error("reminder: claim due", "error", err)
		return
	}
	for _, r := range due {
		p.publish(DueEvent{
			ReminderID: r.ID, UserID: r.UserID, Body: r.Body, DueAt: r.DueAt,
			Channels: r.Channels,
		})
		slog.Info("reminder: fired", "reminder_id", r.ID, "user_id", r.UserID)
	}
}

// publish announces a due reminder. A reminder that fired but could not be
// announced is still marked fired: claiming and publishing cannot be made
// atomic across two systems, and delivering twice is worse than logging a
// miss.
func (p *Poller) publish(evt DueEvent) {
	if p.cfg.Nats == nil {
		return
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("reminder: encode due event", "error", err)
		return
	}
	if err := p.cfg.Nats.Publish(SubjectReminderDue, payload); err != nil {
		slog.Error("reminder: publish due event", "reminder_id", evt.ReminderID, "error", err)
	}
}
