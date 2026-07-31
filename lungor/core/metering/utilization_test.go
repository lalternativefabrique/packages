package metering_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lalternative/packages/lungor/core/metering"
	"github.com/lalternative/packages/lungor/core/metering/domain"
)

// fakeLedger answers ConsumedBetween from a fixed table, keyed by customer.
// Enough to exercise the aggregation without a database: what is under test is
// the arithmetic and the shape of the report, not the SQL.
type fakeLedger struct {
	consumed map[uuid.UUID]int64
}

func (f *fakeLedger) Append(context.Context, *domain.LedgerEntry) (bool, error) {
	return true, nil
}
func (f *fakeLedger) AppendIfBalance(context.Context, *domain.LedgerEntry) (bool, int64, error) {
	return true, 0, nil
}
func (f *fakeLedger) AppendIfPeriodUnder(context.Context, *domain.LedgerEntry, time.Time, time.Time, int64) (bool, int64, error) {
	return true, 0, nil
}
func (f *fakeLedger) Balance(context.Context, uuid.UUID, uuid.UUID, string) (int64, error) {
	return 0, nil
}
func (f *fakeLedger) ConsumedBetween(_ context.Context, _, customerID uuid.UUID, _ string, _, _ time.Time) (int64, error) {
	return f.consumed[customerID], nil
}

func utilMeter(t *testing.T, consumed map[string]int64) *metering.Meter {
	t.Helper()
	appID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	resolver := domain.NewStaticResolver(tenantID)

	byCustomer := make(map[uuid.UUID]int64, len(consumed))
	for user, n := range consumed {
		r, err := resolver.Resolve(context.Background(), appID, user)
		if err != nil {
			t.Fatalf("resolve %q: %v", user, err)
		}
		byCustomer[r.CustomerID] = n
	}

	return metering.New(metering.Config{
		AppID:    appID,
		TenantID: tenantID,
		Ledger:   &fakeLedger{consumed: byCustomer},
	})
}

// The whole point of the report: a mean alone would say "40% used, priced
// fine" for a population that is actually two — dormant subscribers and
// saturated ones. The median and the buckets are what make that visible.
func TestUtilizationReport_MedianExposesTwoPopulations(t *testing.T) {
	m := utilMeter(t, map[string]int64{
		"dormant-1": 5, "dormant-2": 5, "dormant-3": 10,
		"heavy-1": 95, "heavy-2": 100,
	})
	subs := []metering.Subscriber{
		{ExternalUserID: "dormant-1", Plan: "pro", Granted: 100},
		{ExternalUserID: "dormant-2", Plan: "pro", Granted: 100},
		{ExternalUserID: "dormant-3", Plan: "pro", Granted: 100},
		{ExternalUserID: "heavy-1", Plan: "pro", Granted: 100},
		{ExternalUserID: "heavy-2", Plan: "pro", Granted: 100},
	}

	rep, err := m.UtilizationReport(context.Background(), "credit", subs)
	if err != nil {
		t.Fatal(err)
	}

	// (5+5+10+95+100)/5 = 43%, which on its own reads as comfortable headroom.
	if got := rep.Overall.AveragePercent; math.Abs(got-43) > 1e-9 {
		t.Errorf("average = %v, want 43", got)
	}
	// The median is 10: most subscribers barely touch their allocation.
	if got := rep.Overall.MedianPercent; math.Abs(got-10) > 1e-9 {
		t.Errorf("median = %v, want 10 — the gap with the mean is the signal", got)
	}
	// And the buckets say it plainly: three at the bottom, two at the top,
	// nobody in between.
	want := map[string]int{"0-25%": 3, "25-50%": 0, "50-75%": 0, "75-100%": 1, "100%+": 1}
	for _, b := range rep.Buckets {
		if b.Subscribers != want[b.Label] {
			t.Errorf("bucket %s = %d subscribers, want %d", b.Label, b.Subscribers, want[b.Label])
		}
	}
}

// A subscriber past their allocation must land somewhere. Dropping them would
// understate saturation in exactly the population that proves a tier is
// underpriced.
func TestUtilizationReport_OverAllocationLandsInTheOpenBand(t *testing.T) {
	m := utilMeter(t, map[string]int64{"over": 250})
	rep, err := m.UtilizationReport(context.Background(), "credit",
		[]metering.Subscriber{{ExternalUserID: "over", Plan: "max", Granted: 100}})
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.Overall.AveragePercent; math.Abs(got-250) > 1e-9 {
		t.Errorf("average = %v, want 250 — consumption is not clamped", got)
	}
	last := rep.Buckets[len(rep.Buckets)-1]
	if last.Label != "100%+" || last.Subscribers != 1 {
		t.Errorf("open band = %+v, want one subscriber in 100%%+", last)
	}
}

// Plans are compared, not summed. A single tier drifting toward saturation
// while the others idle is the finding a pricing review is looking for.
func TestUtilizationReport_SplitsByPlan(t *testing.T) {
	m := utilMeter(t, map[string]int64{"a": 10, "b": 90, "c": 95})
	rep, err := m.UtilizationReport(context.Background(), "credit", []metering.Subscriber{
		{ExternalUserID: "a", Plan: "solo", Granted: 100},
		{ExternalUserID: "b", Plan: "max", Granted: 100},
		{ExternalUserID: "c", Plan: "max", Granted: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.ByPlan) != 2 {
		t.Fatalf("plans = %d, want 2", len(rep.ByPlan))
	}
	// Sorted by name, so a refresh does not reshuffle the rows.
	if rep.ByPlan[0].Plan != "max" || rep.ByPlan[1].Plan != "solo" {
		t.Errorf("plan order = %q, %q, want max, solo", rep.ByPlan[0].Plan, rep.ByPlan[1].Plan)
	}
	if got := rep.ByPlan[0].AveragePercent; math.Abs(got-92.5) > 1e-9 {
		t.Errorf("max average = %v, want 92.5", got)
	}
}

// A plan granting none of this unit cannot be measured against it. Counting
// such a subscriber as 0% would drag every average toward a floor that means
// nothing — the free tier would make paid tiers look under-consumed.
func TestUtilizationReport_SkipsSubscribersWithNoAllocation(t *testing.T) {
	m := utilMeter(t, map[string]int64{"paid": 50})
	rep, err := m.UtilizationReport(context.Background(), "credit", []metering.Subscriber{
		{ExternalUserID: "paid", Plan: "pro", Granted: 100},
		{ExternalUserID: "ungranted", Plan: "free", Granted: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Overall.Subscribers != 1 {
		t.Errorf("subscribers = %d, want 1 — a zero allocation is not measurable",
			rep.Overall.Subscribers)
	}
	if got := rep.Overall.AveragePercent; math.Abs(got-50) > 1e-9 {
		t.Errorf("average = %v, want 50", got)
	}
}

// No subscribers is a real answer, not an error. The buckets still come back
// so a dashboard renders its axes instead of collapsing.
func TestUtilizationReport_EmptyIsZeroNotNaN(t *testing.T) {
	m := utilMeter(t, nil)
	rep, err := m.UtilizationReport(context.Background(), "credit", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Overall.Subscribers != 0 || rep.Overall.AveragePercent != 0 {
		t.Errorf("empty report = %+v, want zeroed", rep.Overall)
	}
	if math.IsNaN(rep.Overall.AveragePercent) || math.IsNaN(rep.Overall.MedianPercent) {
		t.Error("empty report produced NaN")
	}
	if len(rep.Buckets) != 5 {
		t.Errorf("buckets = %d, want 5 even when empty", len(rep.Buckets))
	}
}
