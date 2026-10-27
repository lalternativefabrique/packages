package schedule

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	reminderdomain "github.com/lalternative/packages/go/reminder/domain"

	"github.com/lalternative/packages/go/schedule/domain"
)

type appointments struct{ saved []*domain.Appointment }

func (f *appointments) Save(_ context.Context, a *domain.Appointment) error {
	f.saved = append(f.saved, a)
	return nil
}

func (f *appointments) Between(_ context.Context, _ string, from, to time.Time) ([]*domain.Appointment, error) {
	var out []*domain.Appointment
	for _, a := range f.saved {
		if a.Start.Before(to) && a.End.After(from) {
			out = append(out, a)
		}
	}
	return out, nil
}

type mails struct{ saved []*domain.ScheduledMail }

func (f *mails) Save(_ context.Context, m *domain.ScheduledMail) error {
	f.saved = append(f.saved, m)
	return nil
}

func (f *mails) Between(_ context.Context, _ string, from, to time.Time) ([]*domain.ScheduledMail, error) {
	var out []*domain.ScheduledMail
	for _, m := range f.saved {
		if !m.SendAt.Before(from) && m.SendAt.Before(to) {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *mails) ClaimDue(_ context.Context, now time.Time, limit int) ([]*domain.ScheduledMail, error) {
	var out []*domain.ScheduledMail
	for _, m := range f.saved {
		if m.Status == domain.MailWaiting && !m.SendAt.After(now) && len(out) < limit {
			m.Status = domain.MailSending
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *mails) Settle(_ context.Context, id string, status domain.MailStatus, failure string) error {
	for _, m := range f.saved {
		if m.ID == id {
			m.Status, m.Failure = status, failure
		}
	}
	return nil
}

type sender struct {
	err  error
	sent []string
}

func (f *sender) Send(_ context.Context, m *domain.ScheduledMail) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m.ID)
	return nil
}

type reminders struct{ saved []*reminderdomain.Reminder }

func (f *reminders) Save(_ context.Context, r *reminderdomain.Reminder) error {
	f.saved = append(f.saved, r)
	return nil
}

func (f *reminders) List(context.Context, string, bool) ([]*reminderdomain.Reminder, error) {
	return f.saved, nil
}

type notes struct{ saved []Note }

func (f *notes) Add(_ context.Context, _, title, body string) (Note, error) {
	n := Note{ID: "n" + strconv.Itoa(len(f.saved)+1), Title: title, Body: body, CreatedAt: time.Now()}
	f.saved = append(f.saved, n)
	return n, nil
}

func (f *notes) List(context.Context, string) ([]Note, error) { return f.saved, nil }

type fakes struct {
	a *appointments
	m *mails
	r *reminders
	n *notes
}

func newService() (*Service, fakes) {
	f := fakes{&appointments{}, &mails{}, &reminders{}, &notes{}}
	return NewService(Config{Appointments: f.a, Mails: f.m, Reminders: f.r, Notes: f.n}), f
}

var paris, _ = time.LoadLocation("Europe/Paris")

func TestAddKeepsEachKindWhereItBelongs(t *testing.T) {
	svc, f := newService()
	now := time.Now().In(paris)
	ctx := context.Background()

	cases := []struct {
		req  AddRequest
		kind domain.Kind
	}{
		{AddRequest{Text: "Dentiste demain 16h", Place: "Dr Morel"}, domain.KindAppointment},
		{AddRequest{Text: "rappel loyer demain 9h"}, domain.KindReminder},
		{AddRequest{Text: "Réunion équipe", Kind: domain.KindNote, Body: "3 actions"}, domain.KindNote},
		{AddRequest{Text: "Devis demain 8h", Kind: domain.KindMail, To: "marc@studio.fr", Subject: "Devis"}, domain.KindMail},
	}
	for _, c := range cases {
		m, err := svc.Add(ctx, "u", c.req, now, paris)
		if err != nil || m.Kind != c.kind {
			t.Fatalf("Add(%q) = %+v, %v", c.req.Text, m, err)
		}
	}
	if len(f.a.saved) != 1 || f.a.saved[0].Place != "Dr Morel" || f.a.saved[0].Title != "Dentiste" {
		t.Fatalf("appointments = %+v", f.a.saved)
	}
	if len(f.r.saved) != 1 || f.r.saved[0].Body != "Loyer" {
		t.Fatalf("reminders = %+v", f.r.saved)
	}
	if len(f.n.saved) != 1 || f.n.saved[0].Body != "3 actions" {
		t.Fatalf("notes = %+v", f.n.saved)
	}
	if len(f.m.saved) != 1 || f.m.saved[0].SendAt.In(paris).Hour() != 8 || f.m.saved[0].Status != domain.MailWaiting {
		t.Fatalf("mails = %+v", f.m.saved)
	}
}

func TestUnderstandWithoutTimeTakesTheNearestFreeSlot(t *testing.T) {
	svc, _ := newService()
	now := time.Date(2026, 10, 21, 8, 10, 0, 0, paris)
	busy, _ := domain.NewAppointment("u", "Équipe", time.Date(2026, 10, 21, 9, 0, 0, 0, paris), time.Time{}, "", nil)
	svc.appointments.(*appointments).saved = append(svc.appointments.(*appointments).saved, busy)

	d, err := svc.Understand(context.Background(), "u", AddRequest{Text: "Appeler la banque"}, now, paris)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Placement.Free || !d.Placement.Start.Equal(time.Date(2026, 10, 21, 10, 0, 0, 0, paris)) {
		t.Fatalf("got %+v", d)
	}
}

func TestScheduleReturnsTheSpanWithEveryKind(t *testing.T) {
	svc, f := newService()
	now := time.Now().In(paris)
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, paris)
	a, _ := domain.NewAppointment("u", "Banque", tomorrow, time.Time{}, "", []string{"Léa"})
	f.a.saved = append(f.a.saved, a)
	r, _ := reminderdomain.New("u", "Loyer", tomorrow.Add(2*time.Hour))
	f.r.saved = append(f.r.saved, r)
	far, _ := reminderdomain.New("u", "Plus tard", tomorrow.AddDate(0, 2, 0))
	f.r.saved = append(f.r.saved, far)
	f.n.saved = append(f.n.saved, Note{ID: "n1", Title: "Idée", CreatedAt: now})

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/schedule?zoom=day&timezone=Europe/Paris&date="+tomorrow.Format(dateLayout), nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := svc.Schedule(c); err != nil {
		t.Fatal(err)
	}
	var out ScheduleDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Zoom != "day" || len(out.Lanes) != 3 || len(out.Bands) != 3 || len(out.Kinds) != 4 {
		t.Fatalf("frame = %+v", out)
	}
	if len(out.Moments) != 2 || out.Moments[0].Lane != "rdv" || out.Moments[1].Lane != "rappel" || out.Moments[0].People[0] != "Léa" {
		t.Fatalf("moments = %+v", out.Moments)
	}
}

func TestAddMomentRefusesAMailWithNoRecipient(t *testing.T) {
	svc, _ := newService()
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/schedule/moments", strings.NewReader(`{"text":"","kind":"mail"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	if err := svc.AddMoment(e.NewContext(req, rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}
