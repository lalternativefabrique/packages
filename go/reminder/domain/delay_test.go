package domain

import (
	"errors"
	"testing"
	"time"
)

func TestParseEveryAcceptsTheIntervalThePersonChose(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"1m": time.Minute, "15 mins": 15 * time.Minute, "1h": time.Hour, "every 2 hours": 0,
		"2 hours": 2 * time.Hour, "in 3d": 72 * time.Hour, "365d": MaxRunEvery,
	} {
		got, err := ParseEvery(in)
		if want == 0 {
			if err == nil {
				t.Errorf("%q accepted", in)
			}
			continue
		}
		if err != nil || got != want {
			t.Errorf("ParseEvery(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestParseEveryRefusesOutOfRange(t *testing.T) {
	for _, in := range []string{"30s", "366d"} {
		if _, err := ParseEvery(in); !errors.Is(err, ErrRunEveryOutOfRange) {
			t.Errorf("ParseEvery(%q) err = %v", in, err)
		}
	}
}

func TestFormatDelayRoundTrips(t *testing.T) {
	for _, d := range []time.Duration{time.Minute, 90 * time.Minute, time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		back, err := ParseDelay(FormatDelay(d))
		if err != nil || back != d {
			t.Errorf("%v → %q → %v, %v", d, FormatDelay(d), back, err)
		}
	}
}
