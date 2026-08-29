package reminder

import (
	"testing"
	"time"
)

func TestADelayIsReadTheWayPeopleWriteIt(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"2h", 2 * time.Hour},
		{"in 2h", 2 * time.Hour},
		{"45m", 45 * time.Minute},
		{"IN 90s", 90 * time.Second},
		{"3d", 72 * time.Hour},
		{"  1h30m  ", 90 * time.Minute},
	} {
		got, err := parseDelay(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("%q = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestANonDelayIsRefusedRatherThanGuessed(t *testing.T) {
	for _, in := range []string{"", "tomorrow", "2", "-5m", "0s", "next week"} {
		if _, err := parseDelay(in); err == nil {
			t.Errorf("%q was accepted as a delay", in)
		}
	}
}

func TestOneOfDueAtOrInIsRequired(t *testing.T) {
	if _, err := resolveDue("", ""); err == nil {
		t.Fatal("a reminder with no time was accepted")
	}
	// Both would mean two answers to one question, and picking either
	// silently discards what the caller asked for.
	if _, err := resolveDue("2030-01-01T00:00:00Z", "2h"); err == nil {
		t.Fatal("due_at and in were accepted together")
	}
}

func TestAnAbsoluteTimeIsKeptAsGiven(t *testing.T) {
	got, err := resolveDue("2030-01-01T09:30:00Z", "")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2030, 1, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("due = %v, want the instant that was given", got)
	}
}

func TestFormatDelayRoundTripsThroughParseDelay(t *testing.T) {
	for _, d := range []time.Duration{time.Hour, 45 * time.Minute, 24 * time.Hour, 3 * 24 * time.Hour} {
		got, err := parseDelay(formatDelay(d))
		if err != nil {
			t.Fatalf("formatDelay(%v) = %q, which parseDelay rejected: %v", d, formatDelay(d), err)
		}
		if got != d {
			t.Errorf("round trip of %v produced %v", d, got)
		}
	}
}

func TestADelayIsCountedFromNow(t *testing.T) {
	before := time.Now().UTC()
	got, err := resolveDue("", "2h")
	if err != nil {
		t.Fatal(err)
	}
	if got.Before(before.Add(2*time.Hour)) || got.After(before.Add(2*time.Hour+time.Minute)) {
		t.Fatalf("due = %v, want about two hours from now", got)
	}
}
