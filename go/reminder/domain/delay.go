package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// EveryHint is how a recurrence is described to whoever sets one, a person
// or a model: the vocabulary and the range the domain accepts.
const EveryHint = "Repeat interval the person chose: 15m, 30m, 1h, 2h, 24h, 7d. Any interval between 1m and 365d is accepted."

// ParseDelay reads a delay the several ways people and models write one:
// "2h", "in 45m", "3d", "2 hours", "15 mins". time.ParseDuration stops at
// hours and refuses words; refusing them would cost a turn every time.
func ParseDelay(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "in "))
	for _, unit := range []struct{ word, short string }{
		{"days", "d"}, {"day", "d"},
		{"hours", "h"}, {"hour", "h"}, {"hrs", "h"}, {"hr", "h"},
		{"minutes", "m"}, {"minute", "m"}, {"mins", "m"}, {"min", "m"},
		{"seconds", "s"}, {"second", "s"}, {"secs", "s"}, {"sec", "s"},
	} {
		if trimmed, ok := strings.CutSuffix(s, unit.word); ok {
			s = strings.TrimSpace(trimmed) + unit.short
			break
		}
	}
	s = strings.ReplaceAll(s, " ", "")
	var d time.Duration
	var err error
	if days, ok := strings.CutSuffix(s, "d"); ok {
		d, err = time.ParseDuration(days + "h")
		d *= 24
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("%q is not a delay like 2h, 45m or 3d", s)
	}
	if d <= 0 {
		return 0, errors.New("the delay must be positive")
	}
	return d, nil
}

// ParseEvery reads a recurrence interval and checks it against the range.
func ParseEvery(s string) (time.Duration, error) {
	d, err := ParseDelay(s)
	if err != nil {
		return 0, fmt.Errorf("every: %w", err)
	}
	if !IsValidRunEvery(d) {
		return 0, fmt.Errorf("every must be between %v and %v: %w", MinRunEvery, FormatDelay(MaxRunEvery), ErrRunEveryOutOfRange)
	}
	return d, nil
}

// FormatDelay renders a duration the way ParseDelay reads it back: whole
// days as "Nd", otherwise Go's form without trailing zero units ("1h30m").
func FormatDelay(d time.Duration) string {
	if d > 0 && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	}
	s := d.String()
	if d%time.Minute == 0 {
		s = strings.TrimSuffix(s, "0s")
		if d%time.Hour == 0 {
			s = strings.TrimSuffix(s, "0m")
		}
	}
	return s
}
