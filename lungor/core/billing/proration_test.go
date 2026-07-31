package billing

import (
	"testing"
	"time"
)

// The worked examples from the PRD (docs/epic/prd-upgrade-consent-proration.md).
// Pro 5 € → Max 12 € over a 30-day period: the delta is 7 €, and what is owed is
// the slice of it covering the days left.
func TestProrateWorkedExamples(t *testing.T) {
	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: start.Add(30 * 24 * time.Hour)}
	const pro, max = int64(500), int64(1200)

	cases := []struct {
		name string
		now  time.Time
		want int64
		why  string
	}{
		{"day 1 of 30", start.Add(1 * 24 * time.Hour), 677,
			"29 days left of a 7 € delta"},
		{"mid-period", start.Add(15 * 24 * time.Hour), 350,
			"exactly half the delta — the PRD's headline example"},
		{"day 28 of 30", start.Add(28 * 24 * time.Hour), 0,
			"0,47 € is under the floor: charge nothing, grant the tier anyway"},
		{"the instant the period opens", start, 700,
			"a full period remains, so the whole delta is owed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Prorate(w, pro, max, tc.now, DefaultProrationFloorCents)
			if got != tc.want {
				t.Fatalf("Prorate(now=%v) = %d cents, want %d — %s",
					tc.now.Format("2006-01-02"), got, tc.want, tc.why)
			}
		})
	}
}

// Everything that must NOT produce a charge. Each of these, if it returned a
// non-zero amount, would take money that is not owed.
func TestProrateRefusesToInventCharges(t *testing.T) {
	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: start.Add(30 * 24 * time.Hour)}
	mid := start.Add(15 * 24 * time.Hour)

	cases := []struct {
		name     string
		w        Window
		from, to int64
		now      time.Time
		why      string
	}{
		{"downgrade", w, 1200, 500, mid,
			"a smaller tier is scheduled for the renewal and refunds nothing — a negative here would credit the customer"},
		{"same tier", w, 500, 500, mid,
			"no move, no charge"},
		{"period already over", w, 500, 1200, w.End.Add(time.Hour),
			"the next renewal charges the new price in full; there is nothing left to top up"},
		{"period ends exactly now", w, 500, 1200, w.End,
			"half-open window: End belongs to the next period"},
		{"zero-width window", Window{Start: start, End: start}, 500, 1200, start,
			"a degenerate window says nothing about what remains"},
		{"inverted window", Window{Start: w.End, End: w.Start}, 500, 1200, mid,
			"corrupt input must not bill a whole delta"},
		{"zero window", Window{}, 500, 1200, mid,
			"an unset window is not a full period"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Prorate(tc.w, tc.from, tc.to, tc.now, DefaultProrationFloorCents); got != 0 {
				t.Fatalf("Prorate = %d cents, want 0 — %s", got, tc.why)
			}
		})
	}
}

// The floor is the whole reason a paid upgrade stays safe near the end of a
// period: under it, no payment is attempted, so no payment can fail and block
// the upgrade.
func TestProrateFloor(t *testing.T) {
	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: start.Add(30 * 24 * time.Hour)}

	// 7 € × 9/30 = 2,10 € — just above the 2 € floor.
	if got := Prorate(w, 500, 1200, start.Add(21*24*time.Hour), DefaultProrationFloorCents); got != 210 {
		t.Fatalf("9 days left = %d cents, want 210 (just above the floor)", got)
	}
	// 7 € × 8/30 = 1,87 € — just below.
	if got := Prorate(w, 500, 1200, start.Add(22*24*time.Hour), DefaultProrationFloorCents); got != 0 {
		t.Fatalf("8 days left = %d cents, want 0 (just below the floor)", got)
	}
	// With no floor, the same instant is charged.
	if got := Prorate(w, 500, 1200, start.Add(22*24*time.Hour), 0); got != 187 {
		t.Fatalf("8 days left with no floor = %d cents, want 187", got)
	}
}

// Money is never rounded down: a cent lost per upgrade is a cent lost forever,
// and to a rule nobody chose.
func TestProrateRoundsUp(t *testing.T) {
	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	// 3 days, and a delta that does not divide evenly by the elapsed fraction.
	w := Window{Start: start, End: start.Add(3 * 24 * time.Hour)}
	// 1000 × 2/3 = 666,67 → 667.
	if got := Prorate(w, 0, 1000, start.Add(24*time.Hour), 0); got != 667 {
		t.Fatalf("Prorate = %d cents, want 667 (rounded up, not 666)", got)
	}
}

// A proration must never exceed one period's difference, whatever the clock
// says. Skew that reads as "more than a full period remains" is clamped rather
// than extrapolated.
func TestProrateClampsFutureClock(t *testing.T) {
	start := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	w := Window{Start: start, End: start.Add(30 * 24 * time.Hour)}
	got := Prorate(w, 500, 1200, start.Add(-10*24*time.Hour), DefaultProrationFloorCents)
	if got != 700 {
		t.Fatalf("Prorate with now before the window = %d cents, want 700 (one full delta, clamped)", got)
	}
}
