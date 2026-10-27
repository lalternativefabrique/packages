// Package domain holds what "Ton temps" lays on a frise: rendez-vous kept by
// this context, reminders and writing kept by others, and the rules deciding
// when a moment typed in one line happens.
package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Lane is a row of the frise.
type Lane string

const (
	LaneAppointment Lane = "rdv"
	LaneReminder    Lane = "rappel"
	LaneWriting     Lane = "ecrit"
)

// Kind is what a moment is, whichever lane it rides.
type Kind string

const (
	KindAppointment Kind = "appointment"
	KindReminder    Kind = "reminder"
	KindNote        Kind = "note"
	KindMail        Kind = "mail"
)

func (k Kind) Lane() Lane {
	switch k {
	case KindReminder:
		return LaneReminder
	case KindNote, KindMail:
		return LaneWriting
	default:
		return LaneAppointment
	}
}

func (k Kind) Valid() bool {
	switch k {
	case KindAppointment, KindReminder, KindNote, KindMail:
		return true
	}
	return false
}

// LaneSpec is how a lane is named and coloured.
type LaneSpec struct {
	ID    Lane
	Label string
	Color string
}

func Lanes() []LaneSpec {
	return []LaneSpec{
		{ID: LaneAppointment, Label: "rendez-vous", Color: "#5fb48a"},
		{ID: LaneReminder, Label: "rappels", Color: "#e8553d"},
		{ID: LaneWriting, Label: "écrit", Color: "#9b86e0"},
	}
}

// KindSpec is a type the add sheet offers.
type KindSpec struct {
	ID    Kind
	Label string
	Lane  Lane
}

func Kinds() []KindSpec {
	return []KindSpec{
		{ID: KindAppointment, Label: "Rendez-vous", Lane: LaneAppointment},
		{ID: KindReminder, Label: "Rappel", Lane: LaneReminder},
		{ID: KindNote, Label: "Note", Lane: LaneWriting},
		{ID: KindMail, Label: "Mail", Lane: LaneWriting},
	}
}

// Band is a part of the day a week or a month is split into.
type Band struct {
	ID       string
	Label    string
	FromHour int
	ToHour   int
}

func Bands() []Band {
	return []Band{
		{ID: "morning", Label: "matin", FromHour: 0, ToHour: 12},
		{ID: "afternoon", Label: "après-midi", FromHour: 12, ToHour: 18},
		{ID: "evening", Label: "soir", FromHour: 18, ToHour: 24},
	}
}

// DayStartHour and DayEndHour bound the day view and where a free slot is looked for.
const (
	DayStartHour = 7
	DayEndHour   = 22
)

// Moment is one thing on the frise.
type Moment struct {
	Kind   Kind
	ID     string
	Title  string
	Start  time.Time
	End    time.Time
	Place  string
	People []string
	Detail string
	Done   bool
	// Status is a mail's MailStatus, empty for any other kind.
	Status MailStatus
}

var (
	ErrTitleMissing     = errors.New("title is required")
	ErrEndBeforeStart   = errors.New("end must be after start")
	ErrRecipientMissing = errors.New("a mail needs a recipient")
)

// Appointment is a rendez-vous: somewhere to be, with someone, for a while.
type Appointment struct {
	ID        string
	UserID    string
	Title     string
	Start     time.Time
	End       time.Time
	Place     string
	People    []string
	CreatedAt time.Time
}

// DefaultAppointmentLength is how long a rendez-vous given only a start lasts.
const DefaultAppointmentLength = time.Hour

func NewAppointment(userID, title string, start, end time.Time, place string, people []string) (*Appointment, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrTitleMissing
	}
	if end.IsZero() {
		end = start.Add(DefaultAppointmentLength)
	}
	if !end.After(start) {
		return nil, ErrEndBeforeStart
	}
	kept := make([]string, 0, len(people))
	for _, p := range people {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return &Appointment{
		ID: uuid.NewString(), UserID: userID, Title: title,
		Start: start.UTC(), End: end.UTC(), Place: strings.TrimSpace(place), People: kept,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// MailStatus says where a mail stands: waiting for its time, being sent by
// one worker, sent, or failed with a reason.
type MailStatus string

const (
	MailWaiting MailStatus = "waiting"
	MailSending MailStatus = "sending"
	MailSent    MailStatus = "sent"
	MailFailed  MailStatus = "failed"
)

// Label is how the frise names the status.
func (s MailStatus) Label() string {
	switch s {
	case MailWaiting:
		return "programmé"
	case MailSending:
		return "envoi en cours"
	case MailSent:
		return "envoyé"
	case MailFailed:
		return "échec"
	}
	return ""
}

// ScheduledMail is a mail written to leave at SendAt.
type ScheduledMail struct {
	ID        string
	UserID    string
	To        string
	Subject   string
	Body      string
	SendAt    time.Time
	Status    MailStatus
	Failure   string
	CreatedAt time.Time
}

var (
	ErrRecipientInvalid = errors.New("a recipient is a mail address")
	ErrMailNotConnected = errors.New("messag is not connected: connect it to send mail")
	ErrMailCannotSend   = errors.New("L'Alter may not send mail from this mailbox: reconnect messag and allow sending")
	ErrMailLimit        = errors.New("the daily limit of sent mails is reached: try again tomorrow")
)

// MailSender sends one mail now.
type MailSender interface {
	Send(ctx context.Context, m *ScheduledMail) error
}

// Recipients are the addresses To lists, separated by commas or semicolons.
func Recipients(to string) []string {
	var out []string
	for _, r := range strings.FieldsFunc(to, func(c rune) bool { return c == ',' || c == ';' }) {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

func NewScheduledMail(userID, to, subject, body string, sendAt time.Time) (*ScheduledMail, error) {
	to = strings.TrimSpace(to)
	recipients := Recipients(to)
	if len(recipients) == 0 {
		return nil, ErrRecipientMissing
	}
	for _, r := range recipients {
		if at := strings.Index(r, "@"); at < 1 || at == len(r)-1 || strings.ContainsAny(r, " <>") {
			return nil, ErrRecipientInvalid
		}
	}
	return &ScheduledMail{
		ID: uuid.NewString(), UserID: userID, To: to, Subject: strings.TrimSpace(subject), Body: body,
		SendAt: sendAt.UTC(), Status: MailWaiting, CreatedAt: time.Now().UTC(),
	}, nil
}

func (m *ScheduledMail) Title() string {
	if m.Subject != "" {
		return m.Subject
	}
	return "Mail à " + m.To
}

type AppointmentRepository interface {
	Save(ctx context.Context, a *Appointment) error
	Between(ctx context.Context, userID string, from, to time.Time) ([]*Appointment, error)
}

type MailRepository interface {
	Save(ctx context.Context, m *ScheduledMail) error
	Between(ctx context.Context, userID string, from, to time.Time) ([]*ScheduledMail, error)
	// ClaimDue moves up to limit waiting mails due by now to sending in one
	// statement, so two workers never send the same mail.
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*ScheduledMail, error)
	Settle(ctx context.Context, id string, status MailStatus, failure string) error
}
