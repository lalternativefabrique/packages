package domain

import (
	"errors"
	"time"
)

// Zoom is how much time the frise shows at once.
type Zoom string

const (
	ZoomDay   Zoom = "day"
	ZoomWeek  Zoom = "week"
	ZoomMonth Zoom = "month"
)

var ErrUnknownZoom = errors.New("zoom is day, week or month")

// Window is the span a zoom shows around anchor, in the person's zone: the
// day, the week from Monday, or the calendar month.
func Window(zoom Zoom, anchor time.Time, loc *time.Location) (time.Time, time.Time, error) {
	a := anchor.In(loc)
	day := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, loc)
	switch zoom {
	case ZoomDay:
		return day, day.AddDate(0, 0, 1), nil
	case ZoomWeek:
		monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
		return monday, monday.AddDate(0, 0, 7), nil
	case ZoomMonth:
		first := time.Date(a.Year(), a.Month(), 1, 0, 0, 0, 0, loc)
		return first, first.AddDate(0, 1, 0), nil
	}
	return time.Time{}, time.Time{}, ErrUnknownZoom
}

// Interval is a span already taken.
type Interval struct {
	Start time.Time
	End   time.Time
}

// SlotStep is the grid free slots are looked for on.
const SlotStep = 30 * time.Minute

// slotSearchDays bounds how far a full agenda pushes a moment with no time.
const slotSearchDays = 14

// FreeSlot is the nearest start, from day on and never before now, where a
// moment of length fits between DayStartHour and DayEndHour without
// overlapping anything busy.
func FreeSlot(busy []Interval, day, now time.Time, length time.Duration) (time.Time, bool) {
	loc := day.Location()
	for i := 0; i < slotSearchDays; i++ {
		d := time.Date(day.Year(), day.Month(), day.Day()+i, 0, 0, 0, 0, loc)
		open := d.Add(DayStartHour * time.Hour)
		closed := d.Add(DayEndHour * time.Hour)
		start := open
		if now.After(start) {
			start = now.Truncate(SlotStep)
			if start.Before(now) {
				start = start.Add(SlotStep)
			}
		}
		for ; !start.Add(length).After(closed); start = start.Add(SlotStep) {
			if fits(busy, start, start.Add(length)) {
				return start, true
			}
		}
	}
	return time.Time{}, false
}

func fits(busy []Interval, start, end time.Time) bool {
	for _, b := range busy {
		if start.Before(b.End) && b.Start.Before(end) {
			return false
		}
	}
	return true
}

// Length is how long a moment of kind holds the agenda.
func Length(kind Kind) time.Duration {
	if kind == KindAppointment {
		return DefaultAppointmentLength
	}
	return SlotStep
}

// Placement says when a parsed moment happens and why.
type Placement struct {
	Start time.Time
	// Timed is true when the line gave the time; false when it was chosen.
	Timed bool
	// Free is true when the time is the nearest free slot.
	Free bool
}

var ErrNoFreeSlot = errors.New("no free time in the coming days")

// Place decides when p happens. A note is written now. A time given is
// kept, pushed to tomorrow when it already passed today and no day was named.
// A mail with no time leaves now; anything else takes the nearest free slot
// of its day.
func Place(p Parsed, kind Kind, now time.Time, busy []Interval) (Placement, error) {
	if kind == KindNote {
		return Placement{Start: now}, nil
	}
	if p.HasTime {
		start := time.Date(p.Day.Year(), p.Day.Month(), p.Day.Day(), p.Hour, p.Minute, 0, 0, p.Day.Location())
		if !p.DayFound && start.Before(now) {
			start = start.AddDate(0, 0, 1)
		}
		return Placement{Start: start, Timed: true}, nil
	}
	if kind == KindMail && !p.DayFound {
		return Placement{Start: now}, nil
	}
	start, ok := FreeSlot(busy, p.Day, now, Length(kind))
	if !ok {
		return Placement{}, ErrNoFreeSlot
	}
	return Placement{Start: start, Free: true}, nil
}
