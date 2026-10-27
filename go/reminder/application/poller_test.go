package application

import (
	"context"
	"testing"
	"time"

	"github.com/lalternative/packages/go/reminder/domain"
)

type dueOnce struct{ due []*domain.Reminder }

func (d *dueOnce) Save(context.Context, *domain.Reminder) error                   { return nil }
func (d *dueOnce) Update(context.Context, *domain.Reminder) error                 { return nil }
func (d *dueOnce) FindByID(context.Context, string) (*domain.Reminder, error)     { return nil, nil }
func (d *dueOnce) List(context.Context, string, bool) ([]*domain.Reminder, error) { return nil, nil }
func (d *dueOnce) ClaimDue(context.Context, int) ([]*domain.Reminder, error) {
	due := d.due
	d.due = nil
	return due, nil
}

func TestAFiredReminderReachesOnDueWithoutNats(t *testing.T) {
	r, err := domain.New("u1", "call Marc", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var got []DueEvent
	p := NewPoller(PollerConfig{Reminders: &dueOnce{due: []*domain.Reminder{r}}, OnDue: func(e DueEvent) { got = append(got, e) }})
	p.tick(context.Background())
	p.tick(context.Background())
	if len(got) != 1 || got[0].ReminderID != r.ID || got[0].UserID != "u1" || got[0].Body != "call Marc" {
		t.Fatalf("got %+v", got)
	}
}
