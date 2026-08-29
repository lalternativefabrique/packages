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
	ErrNotFound           = errors.New("reminder not found")
	ErrBodyMissing        = errors.New("body is required")
	ErrDueInPast          = errors.New("due_at is in the past")
	ErrRunEveryOutOfRange = errors.New("every is out of range")
	ErrNotPending         = errors.New("reminder is not pending")
)

// MinRunEvery bounds how tight a recurrence may be. Anything shorter than the
// poller's worst-case lateness would be silently missed some ticks, so the
// reminder would fire less often than asked for.
const MinRunEvery = time.Minute

// MaxRunEvery keeps a recurrence within a horizon a person still recognises
// as "this repeats" rather than a reminder they forgot they set.
const MaxRunEvery = 365 * 24 * time.Hour

// IsValidRunEvery reports whether d is an acceptable recurrence interval.
func IsValidRunEvery(d time.Duration) bool {
	return d >= MinRunEvery && d <= MaxRunEvery
}

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
	// RunEvery makes the reminder fire again after this interval instead of
	// settling once delivered. Nil is a one-shot reminder.
	RunEvery *time.Duration
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

// Fire marks the reminder as delivered. A recurring one stays pending with
// its next due date instead of settling: the occurrence just delivered is
// done, but the reminder itself is not.
//
// The next date is computed from now rather than from the elapsed DueAt: a
// poller that was down for a while must resume the cadence, not replay every
// occurrence it missed.
func (r *Reminder) Fire() {
	now := time.Now().UTC()
	r.FiredAt = &now
	if r.IsRecurring() {
		r.DueAt = now.Add(*r.RunEvery)
		return
	}
	r.Status = StatusFired
}

// IsRecurring reports whether the reminder fires again after each delivery
// instead of settling once.
func (r *Reminder) IsRecurring() bool {
	return r.RunEvery != nil && *r.RunEvery > 0
}

// SetRunEvery makes the reminder recur at the given interval, or turns it
// back into a one-shot when every is nil. Recurrence can only be set on a
// pending reminder: one already fired, cancelled or done has nothing left to
// repeat.
func (r *Reminder) SetRunEvery(every *time.Duration) error {
	if r.Status != StatusPending {
		return ErrNotPending
	}
	if every != nil && !IsValidRunEvery(*every) {
		return ErrRunEveryOutOfRange
	}
	r.RunEvery = every
	return nil
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

	// ClaimDue takes up to limit reminders whose time has come and settles
	// the occurrence in one statement, so two pollers cannot deliver the
	// same one twice. A one-shot reminder is marked fired; a recurring one
	// is pushed to its next due date and stays pending.
	ClaimDue(ctx context.Context, limit int) ([]*Reminder, error)
}

// Channel delivers a fired reminder through one medium (Slack, email...).
// Target is the ChannelConfig.Target of the reminder's matching entry.
type Channel interface {
	Type() string
	Send(ctx context.Context, title, body, target string) error
}
