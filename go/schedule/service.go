// Package schedule is « Ton temps »: rendez-vous it keeps itself, laid on one
// frise with the reminders, notes and mails other contexts keep, and the
// one-line add that decides what a typed moment is and when it happens.
package schedule

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	reminderdomain "github.com/lalternative/packages/go/reminder/domain"
	"github.com/lalternative/packages/go/schedule/domain"
)

type (
	Reminders interface {
		Save(ctx context.Context, r *reminderdomain.Reminder) error
		List(ctx context.Context, userID string, includeSettled bool) ([]*reminderdomain.Reminder, error)
	}
	Notes interface {
		Add(ctx context.Context, userID, title, body string) (Note, error)
		List(ctx context.Context, userID string) ([]Note, error)
	}
	// Note is what the frise shows of a note another context keeps.
	Note struct {
		ID        string
		Title     string
		Body      string
		CreatedAt time.Time
	}
	// UserIDFunc names the person a request is made for.
	UserIDFunc func(c echo.Context) string
)

type Config struct {
	Appointments domain.AppointmentRepository
	Mails        domain.MailRepository
	Reminders    Reminders
	Notes        Notes
	// Sender sends mail. Nil refuses to send: nothing is recorded as sent.
	Sender domain.MailSender
	// OnMailSettled is told of every mail sent or failed. Nil tells nobody.
	OnMailSettled func(ctx context.Context, m *domain.ScheduledMail)
	UserID        UserIDFunc
}

type Service struct {
	appointments domain.AppointmentRepository
	mails        domain.MailRepository
	reminders    Reminders
	notes        Notes
	sender       domain.MailSender
	onSettled    func(ctx context.Context, m *domain.ScheduledMail)
	userID       UserIDFunc
}

func NewService(cfg Config) *Service {
	return &Service{appointments: cfg.Appointments, mails: cfg.Mails, reminders: cfg.Reminders, notes: cfg.Notes, sender: cfg.Sender, onSettled: cfg.OnMailSettled, userID: cfg.UserID}
}

var ErrUnavailable = errors.New("this kind of moment cannot be kept here")

// Between is every moment of the person from from to to, soonest first.
func (s *Service) Between(ctx context.Context, userID string, from, to time.Time) ([]domain.Moment, error) {
	var out []domain.Moment
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(to) }
	if s.appointments != nil {
		list, err := s.appointments.Between(ctx, userID, from, to)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			out = append(out, domain.Moment{Kind: domain.KindAppointment, ID: a.ID, Title: a.Title, Start: a.Start, End: a.End, Place: a.Place, People: a.People})
		}
	}
	if s.reminders != nil {
		list, err := s.reminders.List(ctx, userID, true)
		if err != nil {
			return nil, err
		}
		for _, r := range list {
			if r.Status == reminderdomain.StatusCancelled || !in(r.DueAt) {
				continue
			}
			out = append(out, domain.Moment{Kind: domain.KindReminder, ID: r.ID, Title: r.Body, Start: r.DueAt, End: r.DueAt.Add(domain.Length(domain.KindReminder)), Done: r.Status == reminderdomain.StatusDone || r.Status == reminderdomain.StatusFired})
		}
	}
	if s.notes != nil {
		list, err := s.notes.List(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, n := range list {
			if !in(n.CreatedAt) {
				continue
			}
			out = append(out, domain.Moment{Kind: domain.KindNote, ID: n.ID, Title: n.Title, Start: n.CreatedAt, End: n.CreatedAt.Add(domain.Length(domain.KindNote)), Detail: excerpt(n.Body)})
		}
	}
	if s.mails != nil {
		list, err := s.mails.Between(ctx, userID, from, to)
		if err != nil {
			return nil, err
		}
		for _, m := range list {
			out = append(out, domain.Moment{Kind: domain.KindMail, ID: m.ID, Title: m.Title(), Start: m.SendAt, End: m.SendAt.Add(domain.Length(domain.KindMail)), People: domain.Recipients(m.To), Detail: mailDetail(m), Status: m.Status})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, nil
}

func mailDetail(m *domain.ScheduledMail) string {
	if m.Status == domain.MailFailed && m.Failure != "" {
		return m.Failure
	}
	return excerpt(m.Body)
}

func excerpt(body string) string {
	body = strings.TrimSpace(body)
	if line, _, ok := strings.Cut(body, "\n"); ok {
		body = line
	}
	if r := []rune(body); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return body
}

// AddRequest is a moment typed in one line, with what the sheet's fields add.
type AddRequest struct {
	Text    string
	Kind    domain.Kind
	Title   string
	Place   string
	People  []string
	Body    string
	To      string
	Subject string
}

// Draft is what an AddRequest becomes, before it is kept.
type Draft struct {
	Kind      domain.Kind
	Title     string
	Placement domain.Placement
	To        string
}

// Understand reads the line and decides when it happens, keeping nothing.
func (s *Service) Understand(ctx context.Context, userID string, req AddRequest, now time.Time, loc *time.Location) (Draft, error) {
	p := domain.Parse(req.Text, now, loc)
	kind := p.Kind
	if !p.KindFound && req.Kind.Valid() {
		kind = req.Kind
	}
	title := p.Title
	if t := strings.TrimSpace(req.Title); t != "" {
		title = t
	}
	to := strings.TrimSpace(req.To)
	if kind == domain.KindMail && to == "" {
		to = title
	}
	busy, err := s.busy(ctx, userID, p.Day)
	if err != nil {
		return Draft{}, err
	}
	placement, err := domain.Place(p, kind, now.In(loc), busy)
	if err != nil {
		return Draft{}, err
	}
	return Draft{Kind: kind, Title: title, Placement: placement, To: to}, nil
}

func (s *Service) busy(ctx context.Context, userID string, day time.Time) ([]domain.Interval, error) {
	moments, err := s.Between(ctx, userID, day, day.AddDate(0, 0, 15))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Interval, 0, len(moments))
	for _, m := range moments {
		if m.Kind == domain.KindAppointment || m.Kind == domain.KindReminder {
			out = append(out, domain.Interval{Start: m.Start, End: m.End})
		}
	}
	return out, nil
}

// Add keeps the moment where it belongs: a rendez-vous and a mail here, a
// reminder and a note in their own contexts.
func (s *Service) Add(ctx context.Context, userID string, req AddRequest, now time.Time, loc *time.Location) (domain.Moment, error) {
	d, err := s.Understand(ctx, userID, req, now, loc)
	if err != nil {
		return domain.Moment{}, err
	}
	start := d.Placement.Start
	switch d.Kind {
	case domain.KindReminder:
		if s.reminders == nil {
			return domain.Moment{}, ErrUnavailable
		}
		r, err := reminderdomain.New(userID, d.Title, start)
		if err != nil {
			return domain.Moment{}, err
		}
		if err := s.reminders.Save(ctx, r); err != nil {
			return domain.Moment{}, err
		}
		return domain.Moment{Kind: d.Kind, ID: r.ID, Title: r.Body, Start: r.DueAt, End: r.DueAt.Add(domain.Length(d.Kind))}, nil
	case domain.KindNote:
		if s.notes == nil {
			return domain.Moment{}, ErrUnavailable
		}
		if strings.TrimSpace(d.Title) == "" {
			return domain.Moment{}, domain.ErrTitleMissing
		}
		n, err := s.notes.Add(ctx, userID, d.Title, req.Body)
		if err != nil {
			return domain.Moment{}, err
		}
		return domain.Moment{Kind: d.Kind, ID: n.ID, Title: n.Title, Start: n.CreatedAt, End: n.CreatedAt.Add(domain.Length(d.Kind)), Detail: excerpt(n.Body)}, nil
	case domain.KindMail:
		if s.mails == nil {
			return domain.Moment{}, ErrUnavailable
		}
		m, err := domain.NewScheduledMail(userID, d.To, req.Subject, req.Body, start)
		if err != nil {
			return domain.Moment{}, err
		}
		if !d.Placement.Timed {
			if s.sender == nil {
				return domain.Moment{}, domain.ErrMailNotConnected
			}
			if err := s.sender.Send(ctx, m); err != nil {
				return domain.Moment{}, err
			}
			m.Status = domain.MailSent
		}
		if err := s.mails.Save(ctx, m); err != nil {
			return domain.Moment{}, err
		}
		return domain.Moment{Kind: d.Kind, ID: m.ID, Title: m.Title(), Start: m.SendAt, End: m.SendAt.Add(domain.Length(d.Kind)), People: domain.Recipients(m.To), Status: m.Status}, nil
	default:
		if s.appointments == nil {
			return domain.Moment{}, ErrUnavailable
		}
		a, err := domain.NewAppointment(userID, d.Title, start, time.Time{}, req.Place, req.People)
		if err != nil {
			return domain.Moment{}, err
		}
		if err := s.appointments.Save(ctx, a); err != nil {
			return domain.Moment{}, err
		}
		return domain.Moment{Kind: domain.KindAppointment, ID: a.ID, Title: a.Title, Start: a.Start, End: a.End, Place: a.Place, People: a.People}, nil
	}
}
