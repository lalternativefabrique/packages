package sdk

import (
	"errors"
	"net/http"
	"testing"
)

func validUsage() Usage {
	return Usage{
		ExternalUserID: "user-1",
		Unit:           "credit",
		Quantity:       5,
		IdempotencyKey: "job-42",
	}
}

func TestConsume_RecordsAndReportsTheBalance(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"allowed": true, "balance": 95, "recorded": true,
	})
	defer srv.Close()

	d, err := New(srv.URL, "k").Consume(ctx(), validUsage())
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !d.Allowed || !d.Recorded || d.Balance != 95 {
		t.Fatalf("decision = %+v", d)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/metering/usage" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
}

// A user at their cap is a STATE, not a failure. Lungor answers 402, and the
// SDK must hand that back as a decision — an error would be retried, and a
// retry cannot lift a cap.
func TestConsume_RefusalIsADecisionNotAnError(t *testing.T) {
	srv, _ := server(t, http.StatusPaymentRequired, map[string]any{
		"allowed": false, "reason": "cap reached", "balance": 0, "recorded": false,
	})
	defer srv.Close()

	d, err := New(srv.URL, "k").Consume(ctx(), validUsage())
	if err != nil {
		t.Fatalf("a refusal must not be an error, got %v", err)
	}
	if d.Allowed {
		t.Fatal("decision says allowed on a 402")
	}
	if d.Reason != "cap reached" {
		t.Fatalf("reason = %q, want the refusal to be explainable", d.Reason)
	}
	if d.Recorded {
		t.Fatal("a refused consumption must not be recorded")
	}
}

// An outage stays an error, so a caller can retry it — the opposite of a cap.
func TestConsume_OutageIsAnError(t *testing.T) {
	srv, _ := server(t, http.StatusInternalServerError, map[string]any{})
	defer srv.Close()

	if _, err := New(srv.URL, "k").Consume(ctx(), validUsage()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

// Metering is the one path where a retry that double-counts costs the customer
// money, so the key is required rather than left to each caller's judgement.
func TestConsume_RequiresAnIdempotencyKey(t *testing.T) {
	u := validUsage()
	u.IdempotencyKey = ""

	_, err := New("https://lungor.test", "k").Consume(ctx(), u)
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("got %v, want ErrBadRequest", err)
	}
}

func TestConsume_RequiresAUserAndAUnit(t *testing.T) {
	for name, mutate := range map[string]func(*Usage){
		"no user": func(u *Usage) { u.ExternalUserID = "" },
		"no unit": func(u *Usage) { u.Unit = "" },
	} {
		u := validUsage()
		mutate(&u)
		if _, err := New("https://lungor.test", "k").Consume(ctx(), u); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%s: got %v, want ErrBadRequest", name, err)
		}
	}
}

func TestTopup_CreditsTheBalance(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"allowed": true, "balance": 105, "recorded": true,
	})
	defer srv.Close()

	d, err := New(srv.URL, "k").Topup(ctx(), validUsage())
	if err != nil {
		t.Fatalf("topup: %v", err)
	}
	if d.Balance != 105 {
		t.Fatalf("balance = %d, want 105", d.Balance)
	}
	if rec.path != "/api/v1/metering/usage/topup" {
		t.Fatalf("path = %s", rec.path)
	}
}

func TestBalance_ReadsTheRemainingAllowance(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"unit": "credit", "balance": 95, "limit": 3000, "consumed": 2905, "periodic": true,
	})
	defer srv.Close()

	got, err := New(srv.URL, "k").Balance(ctx(), "user-1", "credit")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if got.Remaining != 95 {
		t.Fatalf("remaining = %d, want 95", got.Remaining)
	}
	// The period travels with the number, so a caller renders "95 of 3000"
	// without a second call or its own arithmetic.
	if got.Limit != 3000 || got.Consumed != 2905 {
		t.Fatalf("limit/consumed = %d/%d, want 3000/2905", got.Limit, got.Consumed)
	}
	if !got.Periodic {
		t.Fatal("periodic = false for a period-capped balance")
	}
	if rec.method != http.MethodGet {
		t.Fatalf("method = %s", rec.method)
	}
}

// Under the prepaid regime there is no ceiling and no window. Periodic is what
// tells the two apart: "0 left, back at renewal" and "0 in the wallet, top it
// up" are the same number meaning opposite things.
func TestBalance_PrepaidCarriesNoPeriod(t *testing.T) {
	srv, _ := server(t, http.StatusOK, map[string]any{
		"unit": "credit", "balance": 250, "limit": 0, "consumed": 0, "periodic": false,
	})
	defer srv.Close()

	got, err := New(srv.URL, "k").Balance(ctx(), "user-1", "credit")
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if got.Periodic {
		t.Fatal("a prepaid wallet was reported as period-capped")
	}
	if got.Remaining != 250 || got.Limit != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestBalance_RequiresAUserAndAUnit(t *testing.T) {
	c := New("https://lungor.test", "k")
	if _, err := c.Balance(ctx(), "", "credit"); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("no user: got %v, want ErrBadRequest", err)
	}
	if _, err := c.Balance(ctx(), "user-1", ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("no unit: got %v, want ErrBadRequest", err)
	}
}

func TestMetering_UnconfiguredClientIsRefused(t *testing.T) {
	c := New("", "")
	if _, err := c.Consume(ctx(), validUsage()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("consume: got %v, want ErrNotConfigured", err)
	}
	if _, err := c.Balance(ctx(), "user-1", "credit"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("balance: got %v, want ErrNotConfigured", err)
	}
}

func TestMyLLMCost_ReadsTheReportedTotal(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"cost_micros": 42500, "currency": "EUR",
		"period_start": "2026-08-01T00:00:00Z", "period_end": "2026-08-29T00:00:00Z",
	})
	defer srv.Close()

	got, err := New(srv.URL, "k").MyLLMCost(ctx(), "user-42", nil, nil)
	if err != nil {
		t.Fatalf("my llm cost: %v", err)
	}
	if got.CostMicros != 42_500 || got.Currency != "EUR" {
		t.Fatalf("got %+v", got)
	}
	if rec.method != http.MethodGet || rec.path != "/api/v1/metering/llm-cost/me?external_user_id=user-42" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if got.PeriodStart.IsZero() || got.PeriodEnd.IsZero() {
		t.Fatalf("period travels with the total: got %+v", got)
	}
}

// A user with no customer yet is not a zero-cost customer — the two must not
// collapse into the same answer, exactly like Balance vs an unknown unit.
func TestMyLLMCost_UnresolvedUserIsNotFound(t *testing.T) {
	srv, _ := server(t, http.StatusNotFound, map[string]any{"message": "no customer resolved yet"})
	defer srv.Close()

	if _, err := New(srv.URL, "k").MyLLMCost(ctx(), "ghost", nil, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestMyLLMCost_RequiresAUser(t *testing.T) {
	c := New("https://lungor.test", "k")
	if _, err := c.MyLLMCost(ctx(), "", nil, nil); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("got %v, want ErrBadRequest", err)
	}
}

func TestMyLLMCost_UnconfiguredClientIsRefused(t *testing.T) {
	c := New("", "")
	if _, err := c.MyLLMCost(ctx(), "user-1", nil, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

func TestAppLLMCostSummary_BreaksDownByProviderAndModel(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"cost_micros": 42500, "currency": "EUR",
		"period_start": "2026-08-01T00:00:00Z", "period_end": "2026-08-29T00:00:00Z",
		"by_model": []map[string]any{
			{"provider": "scaleway", "model": "deepseek-v4-flash-0731", "cost_micros": 30000, "tokens": 100000, "calls": 5},
			{"provider": "anthropic", "model": "claude-opus-5", "cost_micros": 12500, "tokens": 4000, "calls": 2},
		},
	})
	defer srv.Close()

	got, err := New(srv.URL, "k").AppLLMCostSummary(ctx(), nil, nil)
	if err != nil {
		t.Fatalf("app llm cost summary: %v", err)
	}
	if got.CostMicros != 42_500 || got.Currency != "EUR" {
		t.Fatalf("got %+v", got)
	}
	if rec.method != http.MethodGet || rec.path != "/api/v1/metering/llm-cost/summary" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if len(got.ByModel) != 2 || got.ByModel[0].Provider != "scaleway" || got.ByModel[0].Model != "deepseek-v4-flash-0731" {
		t.Fatalf("by_model = %+v", got.ByModel)
	}
}

func TestAppLLMCostSummary_UnconfiguredClientIsRefused(t *testing.T) {
	c := New("", "")
	if _, err := c.AppLLMCostSummary(ctx(), nil, nil); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}
