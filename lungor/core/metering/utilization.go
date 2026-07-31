package metering

import (
	"context"
	"sort"

	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// Subscriber is one tenant to measure: who they are, what plan they hold, and
// how much of the unit that plan grants them.
//
// The caller supplies the allocation rather than the meter looking it up: plan
// catalogues are product decisions that change with pricing, and a metering
// library that knew about them would need releasing every time a tier moved.
type Subscriber struct {
	ExternalUserID string
	Plan           string
	// Granted for the period. Zero means the plan grants none of this unit, and
	// such a subscriber is skipped: dividing by it is undefined, and counting
	// them as 0% would drag every average toward a floor that means nothing.
	Granted int64
}

// bucketBands are the distribution bands, chosen to answer one question: are
// there two populations here? Quartiles up to the allocation, then a band for
// everyone at or beyond it — saturation is the interesting tail, and lumping it
// into "75-100" would hide exactly the subscribers a pricing decision is about.
var bucketBands = []struct {
	label    string
	from, to int
}{
	{"0-25%", 0, 25},
	{"25-50%", 25, 50},
	{"50-75%", 50, 75},
	{"75-100%", 75, 100},
	{"100%+", 100, 0},
}

// UtilizationReport measures how much of their allocation subscribers actually
// consumed, for one unit.
//
// It walks subscribers one at a time rather than aggregating in SQL, because
// each one's billing window is their own: a subscriber who signed up on the
// 17th has a period running the 17th to the 17th. A GROUP BY over a calendar
// month would report the wrong number for everyone whose anchor is not the 1st
// — which is almost everyone — and it would be wrong quietly.
//
// The cost is one query per subscriber. That is the right trade while the
// answer feeds a pricing decision: a fast wrong number is worse than a slow
// right one. If it ever stops being the right trade, the fix is a cache in
// front of this, not a looser calculation inside it.
func (m *Meter) UtilizationReport(ctx context.Context, unit string, subs []Subscriber) (domain.UtilizationReport, error) {
	report := domain.UtilizationReport{Unit: unit}

	type sample struct {
		plan     string
		percent  float64
		consumed int64
		granted  int64
	}
	samples := make([]sample, 0, len(subs))

	for _, s := range subs {
		if s.Granted <= 0 {
			continue
		}
		used, err := m.ConsumedThisPeriod(ctx, s.ExternalUserID, unit)
		if err != nil {
			return domain.UtilizationReport{}, err
		}
		if used < 0 {
			// Defensive: the ledger returns absolute consumption, but a caller
			// with a custom repository could return a signed value. Treating it
			// as zero keeps a bad adapter from producing a negative average.
			used = 0
		}
		samples = append(samples, sample{
			plan:     s.Plan,
			percent:  float64(used) / float64(s.Granted) * 100,
			consumed: used,
			granted:  s.Granted,
		})
	}

	if len(samples) == 0 {
		report.Buckets = emptyBuckets()
		return report, nil
	}

	byPlan := make(map[string][]sample)
	percents := make([]float64, 0, len(samples))
	for _, s := range samples {
		byPlan[s.plan] = append(byPlan[s.plan], s)
		percents = append(percents, s.percent)
		report.Overall.Consumed += s.consumed
		report.Overall.Granted += s.granted
	}

	report.Overall.Subscribers = len(samples)
	report.Overall.AveragePercent = mean(percents)
	report.Overall.MedianPercent = median(percents)

	// Sorted by plan name so the report is stable between calls: an admin
	// comparing two refreshes should not have to re-read the rows.
	planNames := make([]string, 0, len(byPlan))
	for name := range byPlan {
		planNames = append(planNames, name)
	}
	sort.Strings(planNames)

	for _, name := range planNames {
		group := byPlan[name]
		ps := make([]float64, len(group))
		u := domain.Utilization{Subscribers: len(group)}
		for i, s := range group {
			ps[i] = s.percent
			u.Consumed += s.consumed
			u.Granted += s.granted
		}
		u.AveragePercent = mean(ps)
		u.MedianPercent = median(ps)
		report.ByPlan = append(report.ByPlan, domain.PlanUtilization{Plan: name, Utilization: u})
	}

	report.Buckets = bucketize(percents)
	return report, nil
}

func emptyBuckets() []domain.Bucket {
	out := make([]domain.Bucket, len(bucketBands))
	for i, b := range bucketBands {
		out[i] = domain.Bucket{Label: b.label, From: b.from, To: b.to}
	}
	return out
}

// bucketize assigns each percentage to a band. The last band is open-ended, so
// a subscriber over their allocation lands there rather than nowhere.
func bucketize(percents []float64) []domain.Bucket {
	out := emptyBuckets()
	for _, p := range percents {
		idx := len(out) - 1
		for i, b := range bucketBands {
			if b.to > 0 && p < float64(b.to) {
				idx = i
				break
			}
		}
		out[idx].Subscribers++
	}
	return out
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

// median sorts a copy: the caller's slice order carries meaning elsewhere.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}
