package schedule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/lalternative/packages/go/schedule/domain"
)

func withSender(s *sender) (*Service, fakes) {
	svc, f := newService()
	svc.sender = s
	return svc, f
}

func TestAMailWithNoTimeIsSentAtOnceAndKeptAsSent(t *testing.T) {
	s := &sender{}
	svc, f := withSender(s)
	m, err := svc.Add(context.Background(), "u", AddRequest{Kind: domain.KindMail, To: "lea@studio.fr", Subject: "Devis", Body: "Ci-joint"}, time.Now(), paris)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != domain.MailSent || len(s.sent) != 1 || len(f.m.saved) != 1 || f.m.saved[0].Status != domain.MailSent {
		t.Fatalf("moment = %+v, sent = %v, saved = %+v", m, s.sent, f.m.saved)
	}
}

func TestAMailWithATimeWaitsWithoutSending(t *testing.T) {
	s := &sender{}
	svc, f := withSender(s)
	if _, err := svc.Add(context.Background(), "u", AddRequest{Text: "demain 9h", Kind: domain.KindMail, To: "lea@studio.fr"}, time.Now(), paris); err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 0 || f.m.saved[0].Status != domain.MailWaiting {
		t.Fatalf("sent = %v, saved = %+v", s.sent, f.m.saved[0])
	}
}

func addMail(t *testing.T, svc *Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/schedule/moments", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	if err := svc.AddMoment(c); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestASendRefusedIsNeitherKeptNorShownAsSent(t *testing.T) {
	cases := []struct {
		name   string
		sender *sender
		body   string
		status int
	}{
		{"not connected", &sender{err: domain.ErrMailNotConnected}, `{"kind":"mail","to":"lea@studio.fr"}`, http.StatusConflict},
		{"no send scope", &sender{err: domain.ErrMailCannotSend}, `{"kind":"mail","to":"lea@studio.fr"}`, http.StatusConflict},
		{"daily limit", &sender{err: domain.ErrMailLimit}, `{"kind":"mail","to":"lea@studio.fr"}`, http.StatusTooManyRequests},
		{"a name, not an address", &sender{}, `{"kind":"mail","to":"Marc"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, f := withSender(c.sender)
			rec := addMail(t, svc, c.body)
			if rec.Code != c.status || len(f.m.saved) != 0 {
				t.Fatalf("status = %d (%s), saved = %d", rec.Code, rec.Body.String(), len(f.m.saved))
			}
		})
	}
	svc, _ := newService()
	if rec := addMail(t, svc, `{"kind":"mail","to":"lea@studio.fr"}`); rec.Code != http.StatusConflict {
		t.Fatalf("no sender: status = %d", rec.Code)
	}
}

func TestSendDueSendsWhatIsDueOnceAndRecordsFailures(t *testing.T) {
	s := &sender{}
	svc, f := withSender(s)
	now := time.Now()
	due, _ := domain.NewScheduledMail("u", "a@b.fr", "due", "", now.Add(-time.Minute))
	later, _ := domain.NewScheduledMail("u", "a@b.fr", "later", "", now.Add(time.Hour))
	f.m.saved = append(f.m.saved, due, later)

	if n, err := svc.SendDue(context.Background(), now); err != nil || n != 1 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if due.Status != domain.MailSent || later.Status != domain.MailWaiting {
		t.Fatalf("due = %s, later = %s", due.Status, later.Status)
	}
	if n, _ := svc.SendDue(context.Background(), now); n != 0 || len(s.sent) != 1 {
		t.Fatalf("a sent mail was claimed again: n = %d, sent = %v", n, s.sent)
	}

	s.err = domain.ErrMailLimit
	if _, err := svc.SendDue(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if later.Status != domain.MailFailed || later.Failure != domain.ErrMailLimit.Error() {
		t.Fatalf("later = %+v", later)
	}
}
