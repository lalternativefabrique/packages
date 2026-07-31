package billingperiod

import (
	"testing"
	"time"
)

func date(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, time.UTC)
}

// --- month-end clamping -----------------------------------------------------

// The divergence from Lungor. Every case here returns the WRONG date under a
// bare AddDate, which is what Lungor still does: 31 Jan + 1 month yields 3 March
// there, and the anchor keeps walking forward from that point on.
func TestAddToClampsToLastDayOfShorterMonth(t *testing.T) {
	cases := []struct {
		name  string
		start time.Time
		iv    Interval
		count int
		want  time.Time
	}{
		{
			// The headline bug: AddDate gives 2026-03-03.
			name:  "31 January to a 28-day February",
			start: date(2026, time.January, 31, 9),
			iv:    IntervalMonth,
			count: 1,
			want:  date(2026, time.February, 28, 9),
		},
		{
			// AddDate gives 2026-03-02.
			name:  "30 January to a 28-day February",
			start: date(2026, time.January, 30, 9),
			iv:    IntervalMonth,
			count: 1,
			want:  date(2026, time.February, 28, 9),
		},
		{
			// 2028 is a leap year: February has a 29th, so no clamp is needed.
			name:  "31 January to a leap February",
			start: date(2028, time.January, 31, 9),
			iv:    IntervalMonth,
			count: 1,
			want:  date(2028, time.February, 29, 9),
		},
		{
			// A leap day anchor renewing into a non-leap year clamps to the 28th.
			name:  "29 February of a leap year, one year on",
			start: date(2028, time.February, 29, 9),
			iv:    IntervalYear,
			count: 1,
			want:  date(2029, time.February, 28, 9),
		},
		{
			// Crossing a year boundary; both months have 31 days, so no clamp.
			name:  "31 December to 31 January",
			start: date(2026, time.December, 31, 9),
			iv:    IntervalMonth,
			count: 1,
			want:  date(2027, time.January, 31, 9),
		},
		{
			// 31 → a 30-day month.
			name:  "31 May to a 30-day June",
			start: date(2026, time.May, 31, 9),
			iv:    IntervalMonth,
			count: 1,
			want:  date(2026, time.June, 30, 9),
		},
		{
			// Multi-month counts clamp against the FINAL month, not each step.
			name:  "31 December plus two months",
			start: date(2025, time.December, 31, 9),
			iv:    IntervalMonth,
			count: 2,
			want:  date(2026, time.February, 28, 9),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.iv.AddTo(tc.start, tc.count); !got.Equal(tc.want) {
				t.Errorf("AddTo(%s) = %s, want %s",
					tc.start.Format(time.RFC3339), got.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}

// Clamping must not cost the time of day: only the calendar day is adjusted, so
// a period still ends at the instant it was anchored on.
func TestAddToPreservesTimeOfDay(t *testing.T) {
	start := time.Date(2026, time.January, 31, 14, 37, 5, 123456789, time.UTC)
	got := IntervalMonth.AddTo(start, 1)

	if got.Day() != 28 || got.Month() != time.February {
		t.Fatalf("expected 28 February, got %s", got.Format(time.RFC3339))
	}
	if h, m, s := got.Clock(); h != 14 || m != 37 || s != 5 {
		t.Errorf("clock = %02d:%02d:%02d, want 14:37:05", h, m, s)
	}
	if got.Nanosecond() != 123456789 {
		t.Errorf("nanoseconds = %d, want 123456789", got.Nanosecond())
	}
}

// A mid-month anchor is the common case and must pass through untouched.
func TestAddToKeepsDayOfMonthWhenItExists(t *testing.T) {
	start := date(2026, time.January, 18, 9)
	if got, want := IntervalMonth.AddTo(start, 1), date(2026, time.February, 18, 9); !got.Equal(want) {
		t.Errorf("AddTo = %s, want %s", got, want)
	}
}

func TestAddToYearInterval(t *testing.T) {
	if got, want := IntervalYear.AddTo(date(2026, time.March, 15, 9), 2), date(2028, time.March, 15, 9); !got.Equal(want) {
		t.Errorf("AddTo = %s, want %s", got, want)
	}
}

// Ported from Lungor: a zero/negative count means one, an unknown interval means
// one month. A malformed plan must never yield an unbounded period.
func TestAddToDegenerateInputsFailShort(t *testing.T) {
	start := date(2026, time.March, 15, 9)
	want := date(2026, time.April, 15, 9)

	for _, count := range []int{0, -1, -12} {
		if got := IntervalMonth.AddTo(start, count); !got.Equal(want) {
			t.Errorf("AddTo(count=%d) = %s, want %s (count<1 means 1)", count, got, want)
		}
	}
	if got := Interval("fortnight").AddTo(start, 1); !got.Equal(want) {
		t.Errorf("unknown interval = %s, want a one-month fallback %s", got, want)
	}
}

// --- Activate ---------------------------------------------------------------

// The first period is anchored on the PAYMENT date, which is what stops a late
// webhook from shifting the cycle.
func TestActivateAnchorsOnThePaymentDate(t *testing.T) {
	paidAt := date(2026, time.July, 18, 10)
	w := Activate(paidAt, IntervalMonth, 1)

	if !w.Start.Equal(paidAt) {
		t.Errorf("Start = %s, want the payment date %s", w.Start, paidAt)
	}
	if want := date(2026, time.August, 18, 10); !w.End.Equal(want) {
		t.Errorf("End = %s, want %s", w.End, want)
	}
}

func TestActivateNormalizesToUTC(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*3600)
	w := Activate(time.Date(2026, time.July, 18, 10, 0, 0, 0, zone), IntervalMonth, 1)

	if w.Start.Location() != time.UTC {
		t.Errorf("Start location = %s, want UTC", w.Start.Location())
	}
	// 10:00 at UTC+5 is 05:00 UTC; the window must describe that same instant.
	if want := date(2026, time.July, 18, 5); !w.Start.Equal(want) {
		t.Errorf("Start = %s, want %s", w.Start, want)
	}
}

// --- Advance ----------------------------------------------------------------

// The anti-drift guarantee. Advance must key off the previous window's End and
// nothing else — this is the test that fails if someone reintroduces `now`.
func TestAdvanceStartsWhereThePreviousPeriodEnded(t *testing.T) {
	first := Activate(date(2026, time.July, 18, 10), IntervalMonth, 1)
	second := Advance(first, IntervalMonth, 1)

	if !second.Start.Equal(first.End) {
		t.Errorf("Start = %s, want the previous End %s", second.Start, first.End)
	}
	if want := date(2026, time.September, 18, 10); !second.End.Equal(want) {
		t.Errorf("End = %s, want %s", second.End, want)
	}
}

// Twelve renewals processed at arbitrarily late moments must still land on the
// 18th. Under a `now + 1 month` renewal each late processing would push the
// date out and the error would accumulate; here it cannot.
func TestAdvanceDoesNotDriftOverManyRenewals(t *testing.T) {
	w := Activate(date(2026, time.January, 18, 10), IntervalMonth, 1)
	for i := 0; i < 12; i++ {
		w = Advance(w, IntervalMonth, 1)
	}

	if want := date(2027, time.January, 18, 10); !w.Start.Equal(want) {
		t.Errorf("after 12 renewals Start = %s, want %s", w.Start, want)
	}
	if want := date(2027, time.February, 18, 10); !w.End.Equal(want) {
		t.Errorf("after 12 renewals End = %s, want %s", w.End, want)
	}
}

// Consecutive periods must tile exactly: no gap an event could fall into, no
// overlap where an event would be counted twice.
func TestAdvanceTilesWithoutGapOrOverlap(t *testing.T) {
	first := Activate(date(2026, time.January, 31, 9), IntervalMonth, 1)
	second := Advance(first, IntervalMonth, 1)

	if !second.Start.Equal(first.End) {
		t.Fatalf("gap or overlap: first ends %s, second starts %s", first.End, second.Start)
	}
	// The boundary instant belongs to the second period only.
	if first.Contains(first.End) {
		t.Error("first window must not contain its own End")
	}
	if !second.Contains(second.Start) {
		t.Error("second window must contain its own Start")
	}
}

// --- Contains / IsDue -------------------------------------------------------

func TestWindowContains(t *testing.T) {
	w := Window{Start: date(2026, time.July, 18, 0), End: date(2026, time.August, 18, 0)}

	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"before the start", date(2026, time.July, 17, 23), false},
		{"exactly the start", w.Start, true},
		{"mid period", date(2026, time.August, 1, 12), true},
		{"just before the end", date(2026, time.August, 17, 23), true},
		{"exactly the end", w.End, false},
		{"after the end", date(2026, time.August, 18, 1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.Contains(tc.t); got != tc.want {
				t.Errorf("Contains(%s) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}
}

func TestWindowContainsNothingWhenZeroWidth(t *testing.T) {
	at := date(2026, time.July, 18, 0)
	if (Window{Start: at, End: at}).Contains(at) {
		t.Error("a zero-width window must contain nothing")
	}
}

func TestIsDue(t *testing.T) {
	w := Window{Start: date(2026, time.July, 18, 0), End: date(2026, time.August, 18, 0)}

	if IsDue(w, date(2026, time.August, 17, 23)) {
		t.Error("not due before End")
	}
	if !IsDue(w, w.End) {
		t.Error("due exactly at End")
	}
	if !IsDue(w, date(2026, time.September, 1, 0)) {
		t.Error("due after End")
	}
}

// --- WindowContaining -------------------------------------------------------

func TestWindowContainingWalksToTheCurrentPeriod(t *testing.T) {
	anchor := Activate(date(2026, time.January, 18, 10), IntervalMonth, 1)

	// Three and a bit months on: the 18 April → 18 May period.
	got := WindowContaining(anchor, IntervalMonth, 1, date(2026, time.April, 25, 0))

	if want := date(2026, time.April, 18, 10); !got.Start.Equal(want) {
		t.Errorf("Start = %s, want %s", got.Start, want)
	}
	if want := date(2026, time.May, 18, 10); !got.End.Equal(want) {
		t.Errorf("End = %s, want %s", got.End, want)
	}
	if !got.Contains(date(2026, time.April, 25, 0)) {
		t.Error("the returned window must contain now")
	}
}

func TestWindowContainingReturnsCurrentWindowUnchanged(t *testing.T) {
	anchor := Activate(date(2026, time.July, 18, 10), IntervalMonth, 1)
	got := WindowContaining(anchor, IntervalMonth, 1, date(2026, time.July, 20, 0))

	if got != anchor {
		t.Errorf("a window already containing now must be returned unchanged: got %+v want %+v", got, anchor)
	}
}

// A future anchor (clock skew, or an anchor written ahead) must not walk
// backwards — it is returned as-is.
func TestWindowContainingLeavesAFutureAnchorAlone(t *testing.T) {
	anchor := Activate(date(2027, time.January, 18, 10), IntervalMonth, 1)
	if got := WindowContaining(anchor, IntervalMonth, 1, date(2026, time.July, 20, 0)); got != anchor {
		t.Errorf("future anchor changed: got %+v want %+v", got, anchor)
	}
}

// A corrupt anchor centuries in the past must terminate and yield a bounded
// window. A window that never ends would read as an unlimited quota (NFR4).
func TestWindowContainingTerminatesOnAnAbsurdAnchor(t *testing.T) {
	anchor := Activate(date(1800, time.January, 18, 10), IntervalMonth, 1)
	got := WindowContaining(anchor, IntervalMonth, 1, date(2026, time.July, 20, 0))

	if got.End.Before(got.Start) || got.End.Equal(got.Start) {
		t.Errorf("window must stay well-formed, got %+v", got)
	}
	if got.End.Sub(got.Start) > 32*24*time.Hour {
		t.Errorf("window must stay one period wide, got %s", got.End.Sub(got.Start))
	}
}

// Walking many periods from a month-end anchor must not drift: once clamped to
// 28 February the cycle continues on the 28th, deterministically.
func TestWindowContainingIsStableFromAMonthEndAnchor(t *testing.T) {
	anchor := Activate(date(2026, time.January, 31, 9), IntervalMonth, 1)
	got := WindowContaining(anchor, IntervalMonth, 1, date(2026, time.June, 15, 0))

	if want := date(2026, time.May, 28, 9); !got.Start.Equal(want) {
		t.Errorf("Start = %s, want %s", got.Start, want)
	}
	if want := date(2026, time.June, 28, 9); !got.End.Equal(want) {
		t.Errorf("End = %s, want %s", got.End, want)
	}
}
