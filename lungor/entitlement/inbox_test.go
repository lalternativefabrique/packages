package entitlement

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

type failingInbox struct{ *MemoryInbox }

func (failingInbox) Insert(context.Context, InboxEntry) (bool, error) {
	return false, errors.New("db down")
}

func newInboxGate(l *fakeLedger, inbox Inbox) *Gate {
	g := newGate(l, NewMemoryStore(), nil)
	g.cfg.Inbox = inbox
	return g
}

func postEvent(e *echo.Echo, eventID, event string, body []byte) int {
	req := signed("s3cret", event, body)
	req.Header.Set(sdk.HeaderSourceEventID, eventID)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

func TestWebhookWithInboxStoresOnceAndAcknowledges(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	inbox := NewMemoryInbox()
	g := newInboxGate(l, inbox)
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret"))

	if lim, _ := g.Limits(context.Background(), "u1"); lim.Plan != "free" {
		t.Fatalf("before = %+v", lim)
	}
	l.set("u1", "pro")
	for range 2 {
		if code := postEvent(e, "evt-1", sdk.EventSubscriptionActivated, []byte(`{"external_user_id":"u1"}`)); code != http.StatusNoContent {
			t.Fatalf("webhook: %d", code)
		}
	}
	if lim, _ := g.Limits(context.Background(), "u1"); lim.Plan != "free" {
		t.Fatalf("applied synchronously: %+v", lim)
	}
	entry, ok := inbox.Entry("evt-1")
	if !ok || entry.Status != InboxPending || entry.Subject != "u1" {
		t.Fatalf("entry = %+v ok=%v", entry, ok)
	}

	g.drainInbox(context.Background(), InboxConfig{}.withDefaults())
	if lim, _ := g.Limits(context.Background(), "u1"); lim.Plan != "pro" {
		t.Fatalf("after drain = %+v", lim)
	}
	if entry, _ := inbox.Entry("evt-1"); entry.Status != InboxDone {
		t.Fatalf("entry after = %+v", entry)
	}
}

func TestWebhookInboxInsertFailureAnswers500(t *testing.T) {
	g := newInboxGate(newLedger(), failingInbox{NewMemoryInbox()})
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret"))
	if code := postEvent(e, "evt-1", sdk.EventSubscriptionActivated, []byte(`{"external_user_id":"u1"}`)); code != http.StatusInternalServerError {
		t.Fatalf("got %d", code)
	}
}

func TestInboxRetriesWithBackoffThenFailsAndReplays(t *testing.T) {
	inbox := NewMemoryInbox()
	g := newInboxGate(newLedger(), inbox)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }
	var calls atomic.Int64
	broken := true
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret", OnDelivery(func(context.Context, sdk.Delivery) error {
		calls.Add(1)
		if broken {
			return errors.New("hook down")
		}
		return nil
	})))
	if code := postEvent(e, "evt-1", "invoice.paid", []byte(`{}`)); code != http.StatusNoContent {
		t.Fatalf("webhook: %d", code)
	}
	cfg := InboxConfig{MaxAttempts: 3, Backoff: time.Second, MaxBackoff: 90 * time.Second}.withDefaults()

	g.drainInbox(context.Background(), cfg)
	entry, _ := inbox.Entry("evt-1")
	if entry.Status != InboxPending || entry.Attempts != 1 || !entry.NextAttemptAt.Equal(now.Add(time.Second)) || entry.LastError == "" {
		t.Fatalf("after 1st = %+v", entry)
	}
	g.drainInbox(context.Background(), cfg)
	if calls.Load() != 1 {
		t.Fatalf("retried before backoff: %d calls", calls.Load())
	}
	now = now.Add(time.Second)
	g.drainInbox(context.Background(), cfg)
	if entry, _ = inbox.Entry("evt-1"); !entry.NextAttemptAt.Equal(now.Add(2 * time.Second)) {
		t.Fatalf("after 2nd = %+v", entry)
	}
	now = now.Add(2 * time.Second)
	g.drainInbox(context.Background(), cfg)
	if entry, _ = inbox.Entry("evt-1"); entry.Status != InboxFailed || entry.Attempts != 3 {
		t.Fatalf("after 3rd = %+v", entry)
	}

	broken = false
	n, err := g.ReplayInbox(context.Background(), now.Add(-time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("replay = %d, %v", n, err)
	}
	g.drainInbox(context.Background(), cfg)
	if entry, _ = inbox.Entry("evt-1"); entry.Status != InboxDone {
		t.Fatalf("after replay = %+v", entry)
	}
}

func TestInboxBackoffIsCapped(t *testing.T) {
	cfg := InboxConfig{Backoff: time.Second, MaxBackoff: 10 * time.Second}.withDefaults()
	for attempts, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 4: 8 * time.Second, 5: 10 * time.Second, 30: 10 * time.Second} {
		if got := cfg.backoff(attempts); got != want {
			t.Errorf("backoff(%d) = %s, want %s", attempts, got, want)
		}
	}
}

func TestRunInboxNeedsInbox(t *testing.T) {
	if err := newGate(newLedger(), NewMemoryStore(), nil).RunInbox(context.Background(), InboxConfig{}); err == nil {
		t.Fatal("want error")
	}
}

func TestObserveInvalidatesOnlyOnPlanChange(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGate(l, NewMemoryStore(), nil)
	ctx := context.Background()

	if err := g.Observe(ctx, "u1", "pro"); err != nil {
		t.Fatal(err)
	}
	if l.reads.Load() != 0 {
		t.Fatalf("subject without snapshot was read: %d", l.reads.Load())
	}
	if _, err := g.Limits(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	before := l.reads.Load()
	if err := g.Observe(ctx, "u1", "free"); err != nil {
		t.Fatal(err)
	}
	if err := g.Observe(ctx, "u1", ""); err != nil {
		t.Fatal(err)
	}
	if l.reads.Load() != before {
		t.Fatalf("same plan re-read Lungor")
	}
	l.set("u1", "max")
	if err := g.Observe(ctx, "u1", "max"); err != nil {
		t.Fatal(err)
	}
	if lim, _ := g.Limits(ctx, "u1"); lim.Plan != "max" {
		t.Fatalf("after = %+v", lim)
	}
}

func TestDefaultRefreshIsFifteenMinutes(t *testing.T) {
	if DefaultRefresh != 15*time.Minute {
		t.Fatalf("DefaultRefresh = %s", DefaultRefresh)
	}
}

func TestRefusalOnOldSnapshotRechecksLungorOnce(t *testing.T) {
	l := newLedger()
	l.set("u1", "beta")
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "free", Limits: map[string]int64{"domain": 1, "drive_gb": 0}, RefreshedAt: time.Now().Add(-2 * time.Minute)})
	g := newGate(l, store, map[string]int64{"domain": 1})

	if err := g.Allow(context.Background(), "u1", "domain", 1); err != nil {
		t.Fatalf("allow after recheck: %v", err)
	}
	if l.reads.Load() != 1 {
		t.Fatalf("lungor reads = %d", l.reads.Load())
	}
	if err := g.Allow(context.Background(), "u1", "drive_gb", 1); err == nil {
		t.Fatal("beta grants no drive_gb, want refusal")
	}
	if l.reads.Load() != 1 {
		t.Fatalf("fresh refusal re-read lungor: %d", l.reads.Load())
	}
}

func TestRefusalOnFreshSnapshotMakesNoLedgerCall(t *testing.T) {
	l := newLedger()
	l.set("u1", "beta")
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "free", Limits: map[string]int64{"custom_signature": 0}, RefreshedAt: time.Now()})
	g := newGate(l, store, nil)

	var lerr *LimitError
	if err := g.allowFeature(context.Background(), "u1", "custom_signature"); !errors.As(err, &lerr) {
		t.Fatalf("got %v", err)
	}
	if l.reads.Load() != 0 {
		t.Fatalf("lungor reads = %d", l.reads.Load())
	}
}

func TestRecheckFailureKeepsOriginalRefusal(t *testing.T) {
	l := newLedger()
	l.setDown(true)
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "free", Limits: map[string]int64{"custom_signature": 0}, RefreshedAt: time.Now().Add(-time.Hour)})
	g := newGate(l, store, nil)

	var lerr *LimitError
	if err := g.allowFeature(context.Background(), "u1", "custom_signature"); !errors.As(err, &lerr) || lerr.Plan != "free" {
		t.Fatalf("got %v", err)
	}
}

func TestRecheckRereadsOnlyOldSnapshots(t *testing.T) {
	l := newLedger()
	l.set("u1", "beta")
	store := NewMemoryStore()
	_ = store.Put(context.Background(), "u1", Snapshot{Plan: "free", RefreshedAt: time.Now()})
	g := newGate(l, store, nil)
	ctx := context.Background()

	if lim, err := g.Recheck(ctx, "u1"); err != nil || lim.Plan != "free" || l.reads.Load() != 0 {
		t.Fatalf("fresh: %+v %v reads=%d", lim, err, l.reads.Load())
	}
	g.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if lim, err := g.Recheck(ctx, "u1"); err != nil || lim.Plan != "beta" || l.reads.Load() != 1 {
		t.Fatalf("old: %+v %v reads=%d", lim, err, l.reads.Load())
	}
}
