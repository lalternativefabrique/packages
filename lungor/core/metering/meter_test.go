package metering_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/lungor/core/metering"
	"github.com/lalternative/packages/lungor/core/metering/domain"
)

var (
	testApp    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testTenant = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// clock is a mutable time source for exercising monthly resets.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newMeter(t *testing.T, clk *clock) *metering.Meter {
	t.Helper()
	m := metering.New(metering.Config{
		AppID: testApp, TenantID: testTenant,
		Units: newMemUnits(), Ledger: newMemLedger(),
		Now: clk.now,
	})
	if err := m.DeclareUnit(context.Background(), "whisper_min", "Whisper minutes"); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestListUnits_ReflectsDeclarations(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk) // declares whisper_min

	got, err := m.ListUnits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Code != "whisper_min" {
		t.Fatalf("ListUnits = %+v, want [whisper_min]", got)
	}
}

// ReprovisionUnits re-declares a full set idempotently: it adds a missing unit
// and re-declaring an existing one is a no-op (no duplicate, no error) — this is
// what heals a wiped units table without a restart.
func TestReprovisionUnits_IdempotentAndHeals(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk) // whisper_min already declared

	decls := []metering.UnitDecl{
		{Code: "whisper_min", Name: "Whisper minutes"},
		{Code: "video", Name: "Caption videos"},
	}
	if err := m.ReprovisionUnits(ctx, decls); err != nil {
		t.Fatal(err)
	}
	// Second run must be a no-op, not an error or a duplicate.
	if err := m.ReprovisionUnits(ctx, decls); err != nil {
		t.Fatalf("second reprovision should be idempotent: %v", err)
	}

	got, err := m.ListUnits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ListUnits after reprovision = %d units, want 2", len(got))
	}
	// The newly added unit must now accept consumption (was undeclared before).
	if _, err := m.ConsumeQuota(ctx, "owner1", domain.Allocation{Unit: "video", Amount: 5}, 1, "j:1"); err != nil {
		t.Fatalf("consume on reprovisioned unit failed: %v", err)
	}
}

// The ceiling is exact at both edges: a debit that fits to the unit is allowed,
// one unit more is refused, and the remainder is reported after each movement.
func TestConsumeQuota_HardCapIsExact(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 120}

	// Consume 100 of 120 → allowed, 20 left in the period.
	dec, err := m.ConsumeQuota(ctx, "owner1", alloc, 100, "job:1")
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed || dec.Balance != 20 {
		t.Fatalf("consume 100/120 → allowed=%v remaining=%d, want true/20", dec.Allowed, dec.Balance)
	}

	// Consume 30 more → exceeds the 20 left → refused, nothing recorded.
	dec, err = m.ConsumeQuota(ctx, "owner1", alloc, 30, "job:2")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatalf("consume 30 with 20 left → want refused, got allowed (remaining=%d)", dec.Balance)
	}

	// 20 exactly still fits.
	dec, _ = m.ConsumeQuota(ctx, "owner1", alloc, 20, "job:3")
	if !dec.Allowed || dec.Balance != 0 {
		t.Fatalf("consume final 20 → allowed=%v remaining=%d, want true/0", dec.Allowed, dec.Balance)
	}
}

// The counter returns to zero on the calendar-month boundary under the default
// resolver: a fully spent July leaves August's allowance untouched.
func TestConsumeQuota_MonthlyReset(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 120}

	// Use it all in July.
	if _, err := m.ConsumeQuota(ctx, "owner1", alloc, 120, "july:1"); err != nil {
		t.Fatal(err)
	}
	if rem, _ := m.RemainingThisPeriod(ctx, "owner1", alloc); rem != 0 {
		t.Fatalf("July remaining = %d, want 0", rem)
	}

	// Roll to August → the July debits fall out of the window, back to 120.
	clk.t = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if rem, _ := m.RemainingThisPeriod(ctx, "owner1", alloc); rem != 120 {
		t.Fatalf("August remaining = %d, want 120 (monthly reset)", rem)
	}
}

// Reading the remainder is a pure query: repeated reads inside one period must
// not move it. Under the retired grant machinery this guarded against grants
// stacking; it now guards against a read writing to the ledger at all.
func TestRemainingThisPeriod_ReadIsPure(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 120}

	for i := 0; i < 5; i++ {
		if rem, _ := m.RemainingThisPeriod(ctx, "owner1", alloc); rem != 120 {
			t.Fatalf("read %d remaining = %d, want 120 (a read must not move the counter)", i, rem)
		}
	}
}

func TestConsumeQuota_UndeclaredUnitRefused(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk) // only whisper_min declared
	_, err := m.ConsumeQuota(ctx, "owner1", domain.Allocation{Unit: "video", Amount: 20}, 1, "v:1")
	if err == nil {
		t.Fatal("consume of undeclared unit must error")
	}
}

func TestUnlimited_NeverCaps(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)}
	m := metering.New(metering.Config{
		AppID: testApp, TenantID: testTenant,
		Units: newMemUnits(), Ledger: newMemLedger(), Now: clk.now,
	})
	_ = m.DeclareUnit(ctx, "video", "Videos")
	alloc := domain.Allocation{Unit: "video", Amount: domain.Unlimited}

	// A large number of debits all pass under an unlimited allocation.
	for i := 0; i < 1000; i++ {
		dec, err := m.ConsumeQuota(ctx, "owner1", alloc, 1, "v:"+time.Duration(i).String())
		if err != nil || !dec.Allowed {
			t.Fatalf("unlimited consume %d refused: allowed=%v err=%v", i, dec.Allowed, err)
		}
	}
}

func TestStaticResolver_Deterministic(t *testing.T) {
	r := domain.NewStaticResolver(testTenant)
	a, _ := r.Resolve(context.Background(), testApp, "owner1")
	b, _ := r.Resolve(context.Background(), testApp, "owner1")
	if a.CustomerID != b.CustomerID {
		t.Fatal("resolver must be deterministic for the same external id")
	}
	c, _ := r.Resolve(context.Background(), testApp, "owner2")
	if a.CustomerID == c.CustomerID {
		t.Fatal("different external ids must map to different customers")
	}
	if a.TenantID != testTenant {
		t.Fatalf("tenant = %v, want fixed %v", a.TenantID, testTenant)
	}
}

// --- period-bounded quota (ConsumeQuota) ------------------------------------

// The production bug FR2 fixes: under the prepaid model a tenant who exhausted
// a past period had a zero lifetime balance forever, so every later debit was
// refused however untouched the new period was. ConsumeQuota counts only the
// current window, so the renewal alone restores service.
func TestConsumeQuota_ExhaustedPeriodDoesNotBlockTheNext(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 100}

	// Spend the whole July allowance.
	if dec, err := m.ConsumeQuota(ctx, "owner1", alloc, 100, "july:1"); err != nil || !dec.Allowed {
		t.Fatalf("July full spend: allowed=%v err=%v", dec.Allowed, err)
	}
	// July is now closed.
	if dec, _ := m.ConsumeQuota(ctx, "owner1", alloc, 1, "july:2"); dec.Allowed {
		t.Fatal("a debit past July's allowance was allowed, want refused")
	}

	// August: the ledger still holds July's debit, but it is out of window.
	clk.t = time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	dec, err := m.ConsumeQuota(ctx, "owner1", alloc, 100, "august:1")
	if err != nil {
		t.Fatal(err)
	}
	if !dec.Allowed {
		t.Fatal("August debit refused although the period is fresh — " +
			"an exhausted past period must never block a new one")
	}
	consumed, err := m.ConsumedThisPeriod(ctx, "owner1", alloc.Unit)
	if err != nil {
		t.Fatal(err)
	}
	if consumed != 100 {
		t.Fatalf("August consumption = %d, want 100 (July's debit is out of period)", consumed)
	}
}

// All-or-nothing within the period: a debit that does not fit is refused whole,
// never recorded partially, so a served job is never under-billed.
func TestConsumeQuota_RefusesRatherThanUnderBills(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 100}

	if _, err := m.ConsumeQuota(ctx, "owner1", alloc, 60, "j:1"); err != nil {
		t.Fatal(err)
	}
	dec, err := m.ConsumeQuota(ctx, "owner1", alloc, 80, "j:2") // 60+80 > 100
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed {
		t.Fatal("an 80-credit debit with 40 left was allowed, want refused")
	}
	consumed, _ := m.ConsumedThisPeriod(ctx, "owner1", alloc.Unit)
	if consumed != 60 {
		t.Fatalf("consumption = %d, want 60 — the refused debit must leave no row", consumed)
	}
}

// NFR2: a retry of the SAME logical debit must not be charged twice.
func TestConsumeQuota_Idempotent(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 100}

	d1, _ := m.ConsumeQuota(ctx, "owner1", alloc, 40, "job:X")
	d2, _ := m.ConsumeQuota(ctx, "owner1", alloc, 40, "job:X") // replay
	if !d1.Allowed || !d2.Allowed {
		t.Fatalf("both calls must be allowed: %v %v", d1.Allowed, d2.Allowed)
	}
	if d1.Recorded == d2.Recorded {
		t.Fatal("exactly one of the two calls must have recorded an entry")
	}
	consumed, _ := m.ConsumedThisPeriod(ctx, "owner1", alloc.Unit)
	if consumed != 40 {
		t.Fatalf("consumption = %d, want 40 (single debit, not double)", consumed)
	}
}

// Use it or lose it: an untouched period grants the plan's allowance, never the
// plan's plus what went unspent.
func TestConsumeQuota_UnspentCreditsDoNotCarryOver(t *testing.T) {
	ctx := context.Background()
	clk := &clock{t: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
	m := newMeter(t, clk)
	alloc := domain.Allocation{Unit: "whisper_min", Amount: 100}

	// Spend nothing in July, roll to August.
	clk.t = time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)

	rem, err := m.RemainingThisPeriod(ctx, "owner1", alloc)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 100 {
		t.Fatalf("August allowance = %d, want 100 — July's unspent credits must not carry over", rem)
	}
	if dec, _ := m.ConsumeQuota(ctx, "owner1", alloc, 101, "aug:over"); dec.Allowed {
		t.Fatal("a 101-credit debit was allowed on a 100-credit plan")
	}
}
