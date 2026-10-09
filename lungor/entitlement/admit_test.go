package entitlement

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/lalternative/packages/lungor/policy"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

func newGateWith(l *fakeLedger, mutate func(*Config)) *Gate {
	cfg := Config{
		Ledger: l, Store: NewMemoryStore(), Fallback: "free", Units: gatedUnits, Refresh: time.Hour,
		Subject: func(c echo.Context) (string, error) { return c.Request().Header.Get("X-User"), nil },
	}
	if mutate != nil {
		mutate(&cfg)
	}
	return New(cfg)
}

func TestAdmitReadsLiveBalance(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, nil)
	ctx := context.Background()

	if err := g.Admit(ctx, "u1", "ai_write", 10); err != nil {
		t.Fatalf("10 of 10: %v", err)
	}
	var lerr *LimitError
	if err := g.Admit(ctx, "u1", "ai_write", 11); !errors.As(err, &lerr) || lerr.Limit != 10 || lerr.Used != 0 || lerr.Plan != "free" {
		t.Fatalf("11 of 10: %v", err)
	}
	if err := g.Consume(ctx, "u1", "ai_write", 4, "job-1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Admit(ctx, "u1", "ai_write", 7); !errors.As(err, &lerr) || lerr.Used != 4 {
		t.Fatalf("after 4 consumed: %v", err)
	}
	if n := l.balanceReads.Load(); n < 3 {
		t.Fatalf("balance reads = %d, want one per Admit", n)
	}
}

func TestAdmitHonoursUnlimited(t *testing.T) {
	l := newLedger()
	l.set("staff", "beta")
	g := newGateWith(l, nil)
	if err := g.Admit(context.Background(), "staff", "ai_write", 1<<40); err != nil {
		t.Fatal(err)
	}
}

func TestAdmitMissingUnitPolicy(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	ctx := context.Background()

	var lerr *LimitError
	denied := newGateWith(l, nil)
	if err := denied.Admit(ctx, "u1", "unlisted", 1); !errors.As(err, &lerr) || lerr.Limit != 0 {
		t.Fatalf("denied: %v", err)
	}
	if err := denied.Allow(ctx, "u1", "custom_signature", 1); !errors.As(err, &lerr) {
		t.Fatalf("denied snapshot: %v", err)
	}

	open := newGateWith(l, func(c *Config) { c.MissingUnit = MissingUnitUnlimited })
	if err := open.Admit(ctx, "u1", "unlisted", 1); err != nil {
		t.Fatalf("unlimited: %v", err)
	}
	if err := open.Allow(ctx, "u1", "custom_signature", 1); err != nil {
		t.Fatalf("unlimited snapshot: %v", err)
	}
	if rec := serve(open, open.Require("custom_signature"), "u1", ok); rec.Code != http.StatusCreated {
		t.Fatalf("unlimited feature: %d", rec.Code)
	}
}

func TestAdmitTTLCachesTheBalanceUntilConsume(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, func(c *Config) { c.AdmitTTL = time.Minute })
	ctx := context.Background()

	for range 3 {
		if err := g.Admit(ctx, "u1", "ai_write", 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := l.balanceReads.Load(); n != 1 {
		t.Fatalf("balance reads = %d, want 1", n)
	}
	_ = g.Consume(ctx, "u1", "ai_write", 1, "k")
	_ = g.Admit(ctx, "u1", "ai_write", 1)
	if n := l.balanceReads.Load(); n != 2 {
		t.Fatalf("balance reads after consume = %d, want 2", n)
	}
}

func TestAdmitOnOutage(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	ctx := context.Background()

	deny := newGateWith(l, nil)
	allow := newGateWith(l, func(c *Config) { c.AdmitOnOutage = Allow })
	_, _ = deny.Limits(ctx, "u1")
	_, _ = allow.Limits(ctx, "u1")
	l.setDown(true)

	if err := deny.Admit(ctx, "u1", "ai_write", 1); !errors.Is(err, ErrLedgerUnavailable) {
		t.Fatalf("deny: %v", err)
	}
	if err := allow.Admit(ctx, "u1", "ai_write", 1); err != nil {
		t.Fatalf("allow: %v", err)
	}
	if err := allow.Consume(ctx, "u1", "ai_write", 1, "k"); !errors.Is(err, ErrLedgerUnavailable) {
		t.Fatalf("consume on outage must deny: %v", err)
	}
	if err := allow.Record(ctx, "u1", "ai_write", 1, "k"); !errors.Is(err, ErrLedgerUnavailable) {
		t.Fatalf("record on outage must error: %v", err)
	}
}

func TestConsumeAndRecord(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, nil)
	ctx := context.Background()

	if err := g.Consume(ctx, "u1", "ai_write", 2, "msg-1"); err != nil {
		t.Fatal(err)
	}
	if len(l.consumed) != 1 || l.consumed[0].IdempotencyKey != "msg-1" || l.consumed[0].Quantity != 2 {
		t.Fatalf("consumed = %+v", l.consumed)
	}
	l.refuse = true
	var lerr *LimitError
	if err := g.Consume(ctx, "u1", "ai_write", 1, "msg-2"); !errors.As(err, &lerr) || lerr.Plan != "free" || lerr.Limit != 10 {
		t.Fatalf("refused consume: %v", err)
	}
	if err := g.Record(ctx, "u1", "ai_write", 1, "msg-3"); err != nil {
		t.Fatalf("record never refuses: %v", err)
	}
}

func TestBypassPassesEveryCheck(t *testing.T) {
	l := newLedger()
	l.set("admin", "free")
	l.refuse = true
	g := newGateWith(l, func(c *Config) {
		c.Bypass = func(_ context.Context, subject string) (bool, error) {
			if subject == "broken" {
				return false, errors.New("directory down")
			}
			return subject == "admin", nil
		}
	})
	ctx := context.Background()

	for name, check := range map[string]func() error{
		"allow":   func() error { return g.Allow(ctx, "admin", "custom_signature", 1) },
		"admit":   func() error { return g.Admit(ctx, "admin", "custom_signature", 1) },
		"consume": func() error { return g.Consume(ctx, "admin", "ai_write", 1, "k") },
		"record":  func() error { return g.Record(ctx, "admin", "ai_write", 1, "k") },
	} {
		if err := check(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(l.consumed) != 0 {
		t.Fatalf("bypassed subject was metered: %+v", l.consumed)
	}
	if rec := serve(g, g.Spend("ai_write", 1), "admin", ok); rec.Code != http.StatusCreated {
		t.Fatalf("spend: %d", rec.Code)
	}
	if rec := serve(g, g.Require("custom_signature"), "admin", ok); rec.Code != http.StatusCreated {
		t.Fatalf("require: %d", rec.Code)
	}
	if err := g.Allow(ctx, "broken", "custom_signature", 1); err == nil || strings.Contains(err.Error(), "plan") {
		t.Fatalf("bypass error must surface: %v", err)
	}

	e := echo.New()
	e.GET("/", func(c echo.Context) error {
		ok, err := g.Bypassed(c)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, ok)
	})
	for user, want := range map[string]string{"admin": "true", "u1": "false"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-User", user)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if strings.TrimSpace(rec.Body.String()) != want {
			t.Fatalf("Bypassed(%s) = %s", user, rec.Body)
		}
	}
}

func TestWebhookOnDeliveryRunsBeforeInvalidateAndFailsLoudly(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, nil)
	ctx := context.Background()
	_, _ = g.Limits(ctx, "u1")

	var seen []string
	fail := errors.New("db down")
	var hookErr error
	e := echo.New()
	e.POST("/webhooks/lungor", g.Webhook("s3cret", OnDelivery(func(_ context.Context, d sdk.Delivery) error {
		seen = append(seen, d.Type)
		return hookErr
	})))

	l.set("u1", "pro")
	body := []byte(`{"data":{"ExternalUserID":"u1","ProductCode":"pro"}}`)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, signed("s3cret", sdk.EventSubscriptionActivated, body))
	if rec.Code != http.StatusNoContent || len(seen) != 1 || seen[0] != sdk.EventSubscriptionActivated {
		t.Fatalf("code = %d, seen = %v", rec.Code, seen)
	}
	if lim, _ := g.Limits(ctx, "u1"); lim.Plan != "pro" {
		t.Fatalf("Go-cased subject not re-read: %+v", lim)
	}

	hookErr = fail
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, signed("s3cret", sdk.EventSubscriptionCanceled, body))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("hook error: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, signed("wrong", sdk.EventSubscriptionCanceled, body))
	if rec.Code != http.StatusUnauthorized || len(seen) != 2 {
		t.Fatalf("hook ran on a bad signature: %d, %v", rec.Code, seen)
	}
}

func TestGraceKeepsPlanLimitsForListedStatuses(t *testing.T) {
	l := newLedger()
	l.set("late", "pro")
	l.statuses["late"] = "past_due"
	ctx := context.Background()

	strict := newGateWith(l, nil)
	if lim, _ := strict.Limits(ctx, "late"); lim.Plan != "free" {
		t.Fatalf("default: past_due with Entitled=false must fall back, got %+v", lim)
	}
	lenient := newGateWith(l, func(c *Config) { c.EntitledStatuses = []string{"active", "trialing", "past_due"} })
	if lim, _ := lenient.Limits(ctx, "late"); lim.Plan != "pro" || lim.Limit("domain") != 5 {
		t.Fatalf("grace: %+v", lim)
	}
}

func TestDevModeWithoutLedgerPassesEverything(t *testing.T) {
	g := New(Config{
		Subject:  func(c echo.Context) (string, error) { return c.Request().Header.Get("X-User"), nil },
		Counters: map[string]Counter{"domain": func(context.Context, string) (int64, error) { return 1 << 20, nil }},
	})
	ctx := context.Background()
	for name, check := range map[string]func() error{
		"allow":   func() error { return g.Allow(ctx, "u1", "anything", 1<<30) },
		"admit":   func() error { return g.Admit(ctx, "u1", "anything", 1<<30) },
		"consume": func() error { return g.Consume(ctx, "u1", "anything", 1, "k") },
		"record":  func() error { return g.Record(ctx, "u1", "anything", 1, "k") },
	} {
		if err := check(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if rec := serve(g, g.Spend("ai_write", 1), "u1", ok); rec.Code != http.StatusCreated {
		t.Fatalf("spend: %d", rec.Code)
	}
	if rec := serve(g, g.RequireRoom("domain", 1), "u1", ok); rec.Code != http.StatusCreated {
		t.Fatalf("room: %d", rec.Code)
	}
	if lim, err := g.Limits(ctx, "u1"); err != nil || lim.Plan != AllowAllPlan || lim.Limit("domain") != Unlimited {
		t.Fatalf("limits = %+v, %v", lim, err)
	}
}

func TestStaticLedgerMetersALocalPlan(t *testing.T) {
	g := New(Config{
		Ledger: StaticLedger("local", map[string]int64{"ai_write": 2, "drive_gb": Unlimited}),
		Store:  NewMemoryStore(), Fallback: "local", Units: []string{"ai_write", "drive_gb"},
	})
	ctx := context.Background()

	lim, err := g.Limits(ctx, "u1")
	if err != nil || lim.Plan != "local" || lim.Limit("ai_write") != 2 || lim.Limit("drive_gb") != Unlimited {
		t.Fatalf("limits = %+v, %v", lim, err)
	}
	if err := g.Consume(ctx, "u1", "ai_write", 1, "a"); err != nil {
		t.Fatal(err)
	}
	if err := g.Consume(ctx, "u1", "ai_write", 1, "a"); err != nil {
		t.Fatalf("duplicate key must not count: %v", err)
	}
	if err := g.Consume(ctx, "u1", "ai_write", 1, "b"); err != nil {
		t.Fatal(err)
	}
	var lerr *LimitError
	if err := g.Consume(ctx, "u1", "ai_write", 1, "c"); !errors.As(err, &lerr) || lerr.Limit != 2 || lerr.Used != 2 {
		t.Fatalf("third of 2: %v", err)
	}
	if err := g.Admit(ctx, "u2", "ai_write", 2); err != nil {
		t.Fatalf("other subject: %v", err)
	}
	if err := g.Admit(ctx, "u1", "unlisted", 1); !errors.As(err, &lerr) {
		t.Fatalf("unlisted: %v", err)
	}
	if err := g.Admit(ctx, "u1", "drive_gb", 1<<40); err != nil {
		t.Fatalf("unlimited: %v", err)
	}
}

func TestAnonLedgerRoutesAnonymousSubjects(t *testing.T) {
	l := newLedger()
	anon := policy.AnonPrefix + "visitor"
	ctx := context.Background()

	plain := newGateWith(l, nil)
	if lim, _ := plain.Limits(ctx, anon); lim.Plan != "free" || lim.Limit("ai_write") != 10 {
		t.Fatalf("default anon = %+v", lim)
	}
	if err := plain.Admit(ctx, anon, "ai_write", 1); err != nil {
		t.Fatalf("default anon admit reads the fallback plan: %v", err)
	}

	g := newGateWith(l, func(c *Config) {
		c.AnonLedger = StaticLedger("anon", map[string]int64{"ai_write": 1})
	})
	if lim, _ := g.Limits(ctx, anon); lim.Plan != "anon" || lim.Limit("ai_write") != 1 {
		t.Fatalf("anon = %+v", lim)
	}
	if err := g.Consume(ctx, anon, "ai_write", 1, "k1"); err != nil {
		t.Fatal(err)
	}
	var lerr *LimitError
	if err := g.Admit(ctx, anon, "ai_write", 1); !errors.As(err, &lerr) || lerr.Plan != "anon" {
		t.Fatalf("anon over: %v", err)
	}
	if len(l.consumed) != 0 || l.reads.Load() != 0 {
		t.Fatalf("anonymous subject reached Lungor: consumed=%v reads=%d", l.consumed, l.reads.Load())
	}
}

func TestErrorHandlerOptions(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, nil)
	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(e.DefaultHTTPErrorHandler,
		WithLimitStatus(http.StatusUnprocessableEntity),
		WithMessage(func(err *LimitError) string { return "plan " + err.Plan + " allows " + err.Unit }),
	)
	e.POST("/", ok, g.Require("custom_signature"))
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-User", "u1")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "plan_limit" || body["message"] != "plan free allows custom_signature" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestSpendWithKey(t *testing.T) {
	l := newLedger()
	l.set("u1", "free")
	g := newGateWith(l, nil)
	mw := g.SpendWith("ai_write", 1, SpendOpts{Key: func(c echo.Context) string { return c.Request().Header.Get("X-Message") }})

	e := echo.New()
	e.HTTPErrorHandler = ErrorHandler(e.DefaultHTTPErrorHandler)
	e.POST("/", ok, mw)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("X-User", "u1")
	req.Header.Set("X-Message", "msg-9")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || len(l.consumed) != 1 || l.consumed[0].IdempotencyKey != "ai_write:msg-9" {
		t.Fatalf("code = %d, consumed = %+v", rec.Code, l.consumed)
	}

	req.Header.Del("X-Message")
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("empty key: %d", rec.Code)
	}
}
