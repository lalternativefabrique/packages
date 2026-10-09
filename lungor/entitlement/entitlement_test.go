package entitlement

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/lungor/policy"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

type fakeLedger struct {
	mu           sync.Mutex
	plans        map[string]string
	down         bool
	block        chan struct{}
	reads        atomic.Int64
	balanceReads atomic.Int64
	consumed     []sdk.Usage
	released     []sdk.Usage
	refuse       bool
}

var catalogue = sdk.Plans{
	{Code: "free", Allocations: []sdk.Allocation{{Unit: "domain", Amount: 1}, {Unit: "ai_write", Amount: 10}}},
	{Code: "pro", Allocations: []sdk.Allocation{
		{Unit: "domain", Amount: 5}, {Unit: "custom_signature", Amount: 1}, {Unit: "drive_gb", Amount: Unlimited},
	}},
}

var staffPlans = sdk.Plans{
	{Code: "beta", Allocations: []sdk.Allocation{
		{Unit: "domain", Amount: 50}, {Unit: "custom_signature", Amount: 1}, {Unit: "ai_write", Amount: Unlimited},
	}},
}

var gatedUnits = []string{"domain", "ai_write", "custom_signature", "drive_gb"}

func newLedger() *fakeLedger { return &fakeLedger{plans: map[string]string{}} }

func (f *fakeLedger) Entitlement(_ context.Context, id string, _ ...string) (sdk.Entitlement, error) {
	f.reads.Add(1)
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return sdk.Entitlement{}, sdk.ErrUnavailable
	}
	plan, ok := f.plans[id]
	if !ok {
		return sdk.Entitlement{Status: sdk.StatusNoSubscription}, nil
	}
	return sdk.Entitlement{Entitled: true, Status: "active", PlanCode: plan}, nil
}

func (f *fakeLedger) ListPlans(context.Context) (sdk.Plans, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return nil, sdk.ErrUnavailable
	}
	return catalogue, nil
}

func (f *fakeLedger) Balance(_ context.Context, id, unit string) (sdk.Balance, error) {
	f.balanceReads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return sdk.Balance{}, sdk.ErrUnavailable
	}
	p, ok := append(append(sdk.Plans{}, catalogue...), staffPlans...).ByCode(f.plans[id])
	if !ok {
		return sdk.Balance{Unit: unit}, nil
	}
	for _, a := range p.Allocations {
		if a.Unit == unit {
			return sdk.Balance{Unit: unit, Limit: a.Amount, Unlimited: a.Amount >= Unlimited}, nil
		}
	}
	return sdk.Balance{Unit: unit}, nil
}

func (f *fakeLedger) Consume(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.down {
		return sdk.Decision{}, sdk.ErrUnavailable
	}
	if f.refuse {
		return sdk.Decision{Allowed: false, Balance: 0}, nil
	}
	f.consumed = append(f.consumed, u)
	return sdk.Decision{Allowed: true, Recorded: true}, nil
}

func (f *fakeLedger) Release(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, u)
	return sdk.Decision{Allowed: true}, nil
}

func (f *fakeLedger) set(subject, plan string) {
	f.mu.Lock()
	f.plans[subject] = plan
	f.mu.Unlock()
}

func (f *fakeLedger) setDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

type provisioner struct {
	ledger *fakeLedger
	calls  int
}

func (p *provisioner) Grant(_ context.Context, id, _ string) error {
	p.calls++
	p.ledger.set(id, "free")
	return nil
}

func newGate(l *fakeLedger, store Store, counts map[string]int64) *Gate {
	counters := map[string]Counter{}
	for unit := range counts {
		counters[unit] = func(context.Context, string) (int64, error) { return counts[unit], nil }
	}
	return New(Config{
		Ledger:   l,
		Store:    store,
		Fallback: "free",
		Subject: func(c echo.Context) (string, error) {
			return c.Request().Header.Get("X-User"), nil
		},
		Counters: counters,
		Units:    gatedUnits,
		Refresh:  time.Hour,
	})
}

func TestMemoryCacheHitMakesNoLedgerCall(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	g := newGate(l, NewMemoryStore(), nil)
	ctx := context.Background()

	for range 3 {
		lim, err := g.Limits(ctx, "u1")
		if err != nil || lim.Plan != "pro" || lim.Limit("domain") != 5 {
			t.Fatalf("limits = %+v, %v", lim, err)
		}
	}
	if n := l.reads.Load(); n != 1 {
		t.Fatalf("ledger reads = %d, want 1", n)
	}
}

func TestStoreHitMakesNoLedgerCall(t *testing.T) {
	l := newLedger()
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "pro", Limits: map[string]int64{"domain": 5}, RefreshedAt: time.Now()})
	g := newGate(l, store, nil)

	lim, err := g.Limits(context.Background(), "u1")
	if err != nil || lim.Plan != "pro" {
		t.Fatalf("limits = %+v, %v", lim, err)
	}
	if n := l.reads.Load(); n != 0 {
		t.Fatalf("ledger reads = %d, want 0", n)
	}
}

func TestLedgerReadIsWrittenThroughToStore(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	store := NewMemoryStore()
	g := newGate(l, store, nil)

	if _, err := g.Limits(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	s, ok, _ := store.Get(context.Background(), "u1")
	if !ok || s.Plan != "pro" || s.Limits["domain"] != 5 {
		t.Fatalf("stored = %+v, %v", s, ok)
	}
}

func TestConcurrentMissesShareOneLedgerRead(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	l.block = make(chan struct{})
	g := newGate(l, NewMemoryStore(), nil)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Limits(context.Background(), "u1"); err != nil {
				t.Error(err)
			}
		}()
	}
	for l.reads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(l.block)
	wg.Wait()
	if n := l.reads.Load(); n != 1 {
		t.Fatalf("ledger reads = %d, want 1", n)
	}
}

func TestUnknownSubjectIsProvisionedThenReread(t *testing.T) {
	l := newLedger()
	p := &provisioner{ledger: l}
	g := New(Config{
		Ledger: l, Store: NewMemoryStore(), Fallback: "none", Units: gatedUnits,
		Grant: &policy.Grant{Provisioner: p, Emails: emails{}},
	})

	lim, err := g.Limits(context.Background(), "new")
	if err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || lim.Plan != "free" || lim.Limit("domain") != 1 {
		t.Fatalf("grants = %d, limits = %+v", p.calls, lim)
	}
	if n := l.reads.Load(); n != 2 {
		t.Fatalf("ledger reads = %d, want 2", n)
	}
}

type emails struct{}

func (emails) Email(context.Context, string) (string, bool, error) { return "a@b.c", true, nil }

func TestOutageServesLastSnapshot(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	store := NewMemoryStore()
	g := newGate(l, store, nil)
	_, _ = g.Limits(context.Background(), "u1")

	l.setDown(true)
	g2 := newGate(l, store, nil)
	lim, err := g2.Limits(context.Background(), "u1")
	if err != nil || lim.Plan != "pro" {
		t.Fatalf("limits = %+v, %v", lim, err)
	}
}

func TestOutageWithoutSnapshotAppliesFallbackPlan(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	g := newGate(l, NewMemoryStore(), nil)
	_, _ = g.Limits(context.Background(), "u1")

	l.setDown(true)
	lim, err := g.Limits(context.Background(), "u2")
	if err != nil || lim.Plan != "free" || lim.Limit("domain") != 1 || lim.Limit("custom_signature") != 0 {
		t.Fatalf("limits = %+v, %v", lim, err)
	}
}

func TestOutageWithoutSnapshotOrCatalogueDeniesEverything(t *testing.T) {
	l := newLedger()
	l.setDown(true)
	g := newGate(l, NewMemoryStore(), map[string]int64{"domain": 0})

	var lerr *LimitError
	if err := g.Allow(context.Background(), "u1", "domain", 1); !errors.As(err, &lerr) {
		t.Fatalf("err = %v, want LimitError", err)
	}
}

func TestStaleSnapshotIsServedAndRefreshedInBackground(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "free", Limits: map[string]int64{"domain": 1}, RefreshedAt: time.Now().Add(-2 * time.Hour)})
	g := newGate(l, store, nil)

	lim, _ := g.Limits(context.Background(), "u1")
	if lim.Plan != "free" {
		t.Fatalf("stale read = %+v, want the stored snapshot", lim)
	}
	g.bg.Wait()
	lim, _ = g.Limits(context.Background(), "u1")
	if lim.Plan != "pro" {
		t.Fatalf("after refresh = %+v", lim)
	}
	if s, _, _ := store.Get(context.Background(), "u1"); s.Plan != "pro" {
		t.Fatalf("store = %+v", s)
	}
}

func TestAllowCountsHoldingsAndHonoursUnlimited(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	g := newGate(l, NewMemoryStore(), map[string]int64{"domain": 4, "drive_gb": 1 << 40})
	ctx := context.Background()

	if err := g.Allow(ctx, "u1", "domain", 1); err != nil {
		t.Fatalf("4+1 of 5: %v", err)
	}
	var lerr *LimitError
	if err := g.Allow(ctx, "u1", "domain", 2); !errors.As(err, &lerr) || lerr.Used != 4 || lerr.Limit != 5 || lerr.Plan != "pro" {
		t.Fatalf("4+2 of 5: %v", err)
	}
	if err := g.Allow(ctx, "u1", "drive_gb", 100); err != nil {
		t.Fatalf("unlimited: %v", err)
	}
	if err := g.Allow(ctx, "u1", "unlisted", 1); !errors.As(err, &lerr) {
		t.Fatalf("unlisted unit: %v", err)
	}
}

func serve(g *Gate, mw echo.MiddlewareFunc, user string, handler echo.HandlerFunc) *httptest.ResponseRecorder {
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(e.DefaultHTTPErrorHandler)
	e.POST("/", handler, mw)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-User", user)
	req.Header.Set(echo.HeaderXRequestID, "req-1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func ok(c echo.Context) error { return c.NoContent(http.StatusCreated) }

func TestRequire(t *testing.T) {
	l := newLedger()
	l.set("pro", "pro")
	l.set("free", "free")
	g := newGate(l, NewMemoryStore(), nil)

	if rec := serve(g, g.Require("custom_signature"), "pro", ok); rec.Code != http.StatusCreated {
		t.Fatalf("pro: %d", rec.Code)
	}
	rec := serve(g, g.Require("custom_signature"), "free", ok)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("free: %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["code"] != "plan_limit" || body["unit"] != "custom_signature" || body["plan"] != "free" || body["limit"] != float64(0) || body["used"] != float64(0) {
		t.Fatalf("body = %v", body)
	}
	if rec := serve(g, g.Require("custom_signature"), "", ok); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous request: %d", rec.Code)
	}
}

func TestRequireRoom(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGate(l, NewMemoryStore(), map[string]int64{"domain": 1})

	if rec := serve(g, g.RequireRoom("domain", 1), "u1", ok); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("at limit: %d", rec.Code)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("RequireRoom without a Counter must panic")
		}
	}()
	g.RequireRoom("seats", 1)
}

func TestRequireRoomFrom(t *testing.T) {
	l := newLedger()
	l.set("u1", "pro")
	g := newGate(l, NewMemoryStore(), map[string]int64{"domain": 2})
	amount := func(n int64) func(echo.Context) (int64, error) {
		return func(echo.Context) (int64, error) { return n, nil }
	}

	if rec := serve(g, g.RequireRoomFrom("domain", amount(3)), "u1", ok); rec.Code != http.StatusCreated {
		t.Fatalf("2+3 of 5: %d", rec.Code)
	}
	if rec := serve(g, g.RequireRoomFrom("domain", amount(4)), "u1", ok); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("2+4 of 5: %d", rec.Code)
	}
}

func TestSpend(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGate(l, NewMemoryStore(), nil)

	if rec := serve(g, g.Spend("ai_write", 1), "u1", ok); rec.Code != http.StatusCreated {
		t.Fatalf("spend: %d", rec.Code)
	}
	if len(l.consumed) != 1 || l.consumed[0].IdempotencyKey != "ai_write:req-1" || len(l.released) != 0 {
		t.Fatalf("consumed = %+v, released = %+v", l.consumed, l.released)
	}

	failing := func(echo.Context) error { return errors.New("boom") }
	if rec := serve(g, g.Spend("ai_write", 1), "u1", failing); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failing handler: %d", rec.Code)
	}
	if len(l.released) != 1 || l.released[0].IdempotencyKey != "ai_write:req-1:release" {
		t.Fatalf("released = %+v", l.released)
	}

	l.refuse = true
	if rec := serve(g, g.Spend("ai_write", 1), "u1", ok); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("refused: %d", rec.Code)
	}
}

func TestSpendDeniesWhenLedgerDown(t *testing.T) {
	l := newLedger()
	l.setDown(true)
	g := newGate(l, NewMemoryStore(), nil)

	rec := serve(g, g.Spend("ai_write", 1), "u1", ok)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"ledger_unavailable"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func signed(secret, event string, body []byte) *http.Request {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/lungor", strings.NewReader(string(body)))
	req.Header.Set(sdk.HeaderTimestamp, ts)
	req.Header.Set(sdk.HeaderSignature, "v1="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set(sdk.HeaderEvent, event)
	return req
}

func TestWebhookUpdatesStoreAndCache(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	store := NewMemoryStore()
	g := newGate(l, store, nil)
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret"))

	if lim, _ := g.Limits(context.Background(), "u1"); lim.Plan != "free" {
		t.Fatalf("before = %+v", lim)
	}
	l.set("u1", "pro")
	body := []byte(`{"external_user_id":"u1"}`)
	for range 2 {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, signed("s3cret", sdk.EventSubscriptionActivated, body))
		if rec.Code != http.StatusNoContent {
			t.Fatalf("webhook: %d", rec.Code)
		}
	}
	if lim, _ := g.Limits(context.Background(), "u1"); lim.Plan != "pro" {
		t.Fatalf("cache after = %+v", lim)
	}
	if s, _, _ := store.Get(context.Background(), "u1"); s.Plan != "pro" {
		t.Fatalf("store after = %+v", s)
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	g := newGate(newLedger(), NewMemoryStore(), nil)
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret"))

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, signed("wrong", sdk.EventEntitlementChanged, []byte(`{"external_user_id":"u1"}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestErrorHandlerPassesOtherErrorsThrough(t *testing.T) {
	var passed error
	h := ErrorHandler(func(err error, _ echo.Context) { passed = err })
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())
	other := errors.New("other")
	h(other, c)
	if passed != other {
		t.Fatalf("passed = %v", passed)
	}
}

func TestStaffPlanAbsentFromCatalogueGetsItsOwnLimits(t *testing.T) {
	l := newLedger()
	l.set("staff", "beta")
	g := newGate(l, NewMemoryStore(), nil)

	lim, err := g.Limits(context.Background(), "staff")
	if err != nil {
		t.Fatal(err)
	}
	if lim.Plan != "beta" || lim.Limit("domain") != 50 || lim.Limit("custom_signature") != 1 ||
		lim.Limit("ai_write") != Unlimited || lim.Limit("drive_gb") != 0 {
		t.Fatalf("limits = %+v", lim)
	}
}

func TestPublicPlansReadTheSameLimits(t *testing.T) {
	l := newLedger()
	l.set("f", "free")
	l.set("p", "pro")
	g := newGate(l, NewMemoryStore(), nil)
	ctx := context.Background()

	free, _ := g.Limits(ctx, "f")
	pro, _ := g.Limits(ctx, "p")
	if free.Limit("domain") != 1 || free.Limit("ai_write") != 10 || free.Limit("custom_signature") != 0 {
		t.Fatalf("free = %+v", free)
	}
	if pro.Limit("domain") != 5 || pro.Limit("custom_signature") != 1 || pro.Limit("drive_gb") != Unlimited {
		t.Fatalf("pro = %+v", pro)
	}
}

func TestRoutesRegisterTheUnitsTheyGate(t *testing.T) {
	l := newLedger()
	l.set("staff", "beta")
	g := New(Config{Ledger: l, Store: NewMemoryStore(), Fallback: "free"})
	g.Require("custom_signature")
	g.Spend("ai_write", 1)

	lim, err := g.Limits(context.Background(), "staff")
	if err != nil {
		t.Fatal(err)
	}
	if len(lim.Limits) != 2 || lim.Limit("custom_signature") != 1 || lim.Limit("ai_write") != Unlimited {
		t.Fatalf("limits = %+v", lim)
	}
}

func TestInvalidateRereadsAndUpdatesStoreAndCache(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	store := NewMemoryStore()
	g := newGate(l, store, nil)
	ctx := context.Background()
	if _, err := g.Limits(ctx, "u1"); err != nil {
		t.Fatal(err)
	}

	l.set("u1", "beta")
	if err := g.Invalidate(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	lim, _ := g.Limits(ctx, "u1")
	stored, ok, _ := store.Get(ctx, "u1")
	if lim.Plan != "beta" || lim.Limit("domain") != 50 || !ok || stored.Plan != "beta" {
		t.Fatalf("cache = %+v, store = %+v", lim, stored)
	}

	l.setDown(true)
	if err := g.Invalidate(ctx, "u1"); err == nil {
		t.Fatal("invalidate succeeded with Lungor down")
	}
	if lim, _ := g.Limits(ctx, "u1"); lim.Plan != "beta" {
		t.Fatalf("snapshot lost on failed invalidate: %+v", lim)
	}
}
