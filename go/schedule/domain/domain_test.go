package domain

import (
	"testing"
	"time"
)

var paris, _ = time.LoadLocation("Europe/Paris")

// Wednesday 21 October 2026, 16:40 in Paris.
var now = time.Date(2026, 10, 21, 16, 40, 0, 0, paris)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, paris) }

func TestParse(t *testing.T) {
	cases := []struct {
		line    string
		kind    Kind
		day     time.Time
		hasTime bool
		h, m    int
		title   string
	}{
		{"Dentiste jeudi 16h", KindAppointment, day(10, 22), true, 16, 0, "Dentiste"},
		{"rappel loyer demain 9h30", KindReminder, day(10, 22), true, 9, 30, "Loyer"},
		{"Rappelle-moi d'appeler Marc", KindReminder, day(10, 21), false, 0, 0, "Appeler Marc"},
		{"Déjeuner Léa à midi", KindAppointment, day(10, 21), true, 12, 0, "Déjeuner Léa"},
		{"note sur la réunion équipe", KindNote, day(10, 21), false, 0, 0, "Réunion équipe"},
		{"mail à Marc après-demain 8:15", KindMail, day(10, 23), true, 8, 15, "Marc"},
		{"Banque le 7", KindAppointment, day(11, 7), false, 0, 0, "Banque"},
		{"Anniversaire Léa 31 octobre", KindAppointment, day(10, 31), false, 0, 0, "Anniversaire Léa"},
		{"Impôts 15/05", KindAppointment, time.Date(2027, 5, 15, 0, 0, 0, 0, paris), false, 0, 0, "Impôts"},
		{"Équipe mercredi", KindAppointment, day(10, 28), false, 0, 0, "Équipe"},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			p := Parse(c.line, now, paris)
			if p.Kind != c.kind || !p.Day.Equal(c.day) || p.HasTime != c.hasTime || p.Title != c.title {
				t.Fatalf("Parse(%q) = %+v", c.line, p)
			}
			if c.hasTime && (p.Hour != c.h || p.Minute != c.m) {
				t.Fatalf("time = %d:%d, want %d:%d", p.Hour, p.Minute, c.h, c.m)
			}
		})
	}
}

func TestParseKeepsAnImpossibleHourInTheTitle(t *testing.T) {
	p := Parse("Salle 31h", now, paris)
	if p.HasTime || p.Title != "Salle 31h" {
		t.Fatalf("got %+v", p)
	}
}

func TestWindow(t *testing.T) {
	cases := []struct {
		zoom     Zoom
		from, to time.Time
	}{
		{ZoomDay, day(10, 21), day(10, 22)},
		{ZoomWeek, day(10, 19), day(10, 26)},
		{ZoomMonth, day(10, 1), day(11, 1)},
	}
	for _, c := range cases {
		from, to, err := Window(c.zoom, now, paris)
		if err != nil || !from.Equal(c.from) || !to.Equal(c.to) {
			t.Fatalf("%s: %v → %v (%v)", c.zoom, from, to, err)
		}
	}
	if _, _, err := Window("year", now, paris); err != ErrUnknownZoom {
		t.Fatalf("err = %v", err)
	}
}

func at(d time.Time, h, m int) time.Time {
	return d.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute)
}

func TestFreeSlotStartsAfterNowOnTheGrid(t *testing.T) {
	got, ok := FreeSlot(nil, day(10, 21), now, time.Hour)
	if !ok || !got.Equal(at(day(10, 21), 17, 0)) {
		t.Fatalf("got %v", got)
	}
}

func TestFreeSlotSkipsWhatIsBusy(t *testing.T) {
	busy := []Interval{{at(day(10, 21), 17, 0), at(day(10, 21), 18, 0)}, {at(day(10, 21), 18, 30), at(day(10, 21), 19, 0)}}
	got, _ := FreeSlot(busy, day(10, 21), now, time.Hour)
	if !got.Equal(at(day(10, 21), 19, 0)) {
		t.Fatalf("got %v", got)
	}
}

func TestFreeSlotOnAFutureDayStartsInTheMorning(t *testing.T) {
	got, _ := FreeSlot(nil, day(10, 23), now, time.Hour)
	if !got.Equal(at(day(10, 23), DayStartHour, 0)) {
		t.Fatalf("got %v", got)
	}
}

func TestFreeSlotMovesToTheNextDayOnceTheDayIsOver(t *testing.T) {
	late := at(day(10, 21), 21, 30)
	got, _ := FreeSlot(nil, day(10, 21), late, time.Hour)
	if !got.Equal(at(day(10, 22), DayStartHour, 0)) {
		t.Fatalf("got %v", got)
	}
}

func TestPlace(t *testing.T) {
	timed, _ := Place(Parse("Dentiste 9h", now, paris), KindAppointment, now, nil)
	if !timed.Timed || !timed.Start.Equal(at(day(10, 22), 9, 0)) {
		t.Fatalf("a passed hour with no day is tomorrow, got %+v", timed)
	}
	mail, _ := Place(Parse("mail à Marc", now, paris), KindMail, now, nil)
	if mail.Timed || !mail.Start.Equal(now) {
		t.Fatalf("a mail with no time leaves now, got %+v", mail)
	}
	free, _ := Place(Parse("Coiffeur vendredi", now, paris), KindAppointment, now, nil)
	if !free.Free || !free.Start.Equal(at(day(10, 23), DayStartHour, 0)) {
		t.Fatalf("got %+v", free)
	}
}

func TestNewAppointment(t *testing.T) {
	start := at(day(10, 22), 9, 0)
	a, err := NewAppointment("u", " Équipe ", start, time.Time{}, " Salle 2 ", []string{"Léa", " "})
	if err != nil || a.Title != "Équipe" || !a.End.Equal(start.Add(time.Hour)) || a.Place != "Salle 2" || len(a.People) != 1 {
		t.Fatalf("got %+v, %v", a, err)
	}
	if _, err := NewAppointment("u", "x", start, start, "", nil); err != ErrEndBeforeStart {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewAppointment("u", " ", start, time.Time{}, "", nil); err != ErrTitleMissing {
		t.Fatalf("err = %v", err)
	}
}
