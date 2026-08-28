// Package domain holds what a reminder is: something to say back to someone at
// a time they chose, and how to reach them when it comes due.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Status is where a reminder is in its life.
type Status string

const (
	StatusPending   Status = "pending"
	StatusFired     Status = "fired"
	StatusCancelled Status = "cancelled"
	StatusDone      Status = "done"
)

var (
	ErrNotFound    = errors.New("reminder not found")
	ErrBodyMissing = errors.New("body is required")
	ErrDueInPast   = errors.New("due_at is in the past")
)

// ChannelConfig names one way to reach the person when a reminder fires.
// Target's meaning depends on Type: a Slack webhook URL, an email address...
type ChannelConfig struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

// Reminder is one thing to say back, and when.
type Reminder struct {
	ID        string
	UserID    string
	Body      string
	DueAt     time.Time
	Status    Status
	CreatedAt time.Time
	FiredAt   *time.Time
	// Channels says where to deliver the reminder when it fires. Empty means
	// "the API/chat surface alone" — nothing external is contacted.
	Channels []ChannelConfig
}

// New validates a request and returns a pending reminder.
//
// A due time already past is refused rather than fired at once: someone asking
// to be reminded yesterday made a mistake, and firing immediately hides it.
func New(userID, body string, dueAt time.Time, channels ...ChannelConfig) (*Reminder, error) {
	if strings.TrimSpace(body) == "" {
		return nil, ErrBodyMissing
	}
	if dueAt.Before(time.Now().UTC()) {
		return nil, ErrDueInPast
	}
	return &Reminder{
		ID:        uuid.NewString(),
		UserID:    userID,
		Body:      strings.TrimSpace(body),
		DueAt:     dueAt.UTC(),
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		Channels:  channels,
	}, nil
}

// Fire marks the reminder as delivered.
func (r *Reminder) Fire() {
	now := time.Now().UTC()
	r.Status = StatusFired
	r.FiredAt = &now
}

// Cancel drops a reminder nobody wants any more.
func (r *Reminder) Cancel() { r.Status = StatusCancelled }

// Done marks a reminder as taken care of, whether or not it had fired yet.
func (r *Reminder) Done() { r.Status = StatusDone }

// Repository persists reminders.
type Repository interface {
	Save(ctx context.Context, r *Reminder) error
	Update(ctx context.Context, r *Reminder) error
	FindByID(ctx context.Context, id string) (*Reminder, error)
	List(ctx context.Context, userID string, includeSettled bool) ([]*Reminder, error)

	// ClaimDue takes up to limit reminders whose time has come and marks
	// them fired in one statement, so two pollers cannot deliver the same
	// one twice.
	ClaimDue(ctx context.Context, limit int) ([]*Reminder, error)
}

// Channel delivers a fired reminder through one medium (Slack, email...).
// Target is the ChannelConfig.Target of the reminder's matching entry.
type Channel interface {
	Type() string
	Send(ctx context.Context, title, body, target string) error
}
