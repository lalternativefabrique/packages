package sdk

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

var costAt = time.Date(2026, 9, 30, 14, 3, 0, 0, time.UTC)

func TestRecordCost_SendsTheLines(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"accepted": 1, "inserted": 1,
		"lines": []map[string]any{{
			"code": "kwh", "idempotency_key": "k1", "quantity": 1500, "price_id": "p-1",
			"price_micros_per_unit": 250000, "unit_size": 1000, "amount_micros": 375000, "currency": "EUR",
		}},
	})
	defer srv.Close()

	res, err := New(srv.URL, "k").RecordCost(ctx(), []CostLine{{
		Code: "kwh", Quantity: 1500, OccurredAt: costAt, IdempotencyKey: "k1", ExternalUserID: "user-1", RunKey: "run-1",
	}})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/metering/costs" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	lines, _ := rec.body["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("body = %v", rec.body)
	}
	got := lines[0].(map[string]any)
	want := map[string]any{
		"code": "kwh", "quantity": float64(1500), "occurred_at": "2026-09-30T14:03:00Z",
		"idempotency_key": "k1", "external_user_id": "user-1", "run_key": "run-1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %v, want %v", k, got[k], v)
		}
	}
	if res.Accepted != 1 || res.Inserted != 1 || len(res.Lines) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if l := res.Lines[0]; l.AmountMicros != 375000 || l.UnitSize != 1000 || l.PriceID != "p-1" || l.Currency != "EUR" {
		t.Fatalf("line = %+v", l)
	}
}

func TestRecordCost_OmitsEmptyOptionalFields(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{"accepted": 1, "inserted": 1})
	defer srv.Close()

	if _, err := New(srv.URL, "k").RecordCost(ctx(), []CostLine{{Code: "kwh", Quantity: 1, OccurredAt: costAt, IdempotencyKey: "k"}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	line := rec.body["lines"].([]any)[0].(map[string]any)
	if _, ok := line["external_user_id"]; ok {
		t.Fatalf("external_user_id sent empty: %v", line)
	}
	if _, ok := line["run_key"]; ok {
		t.Fatalf("run_key sent empty: %v", line)
	}
}

func TestRecordCost_RejectsInvalidLinesBeforeCalling(t *testing.T) {
	cases := map[string][]CostLine{
		"no lines":   nil,
		"no code":    {{Quantity: 1, OccurredAt: costAt, IdempotencyKey: "k"}},
		"no key":     {{Code: "c", Quantity: 1, OccurredAt: costAt}},
		"no date":    {{Code: "c", Quantity: 1, IdempotencyKey: "k"}},
		"negative q": {{Code: "c", Quantity: -1, OccurredAt: costAt, IdempotencyKey: "k"}},
	}
	for name, lines := range cases {
		if _, err := New("https://lungor.test", "k").RecordCost(ctx(), lines); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("%s: got %v, want ErrBadRequest", name, err)
		}
	}
}

func TestRecordCost_MapsServerRefusals(t *testing.T) {
	cases := []struct {
		status  int
		message string
		want    error
	}{
		{http.StatusNotFound, "line 1 (gpt-x.input): cost item not found", ErrUnknownCostItem},
		{http.StatusUnprocessableEntity, "line 0 (kwh): cost item is not attached to this app", ErrCostItemNotUsable},
		{http.StatusUnprocessableEntity, "line 0 (kwh): cost item is inactive", ErrCostItemNotUsable},
		{http.StatusUnprocessableEntity, "line 2 (kwh): cost item has no price in force at occurred_at", ErrNoCostPrice},
		{http.StatusBadRequest, "lines required", ErrBadRequest},
	}
	for _, tc := range cases {
		srv, _ := server(t, tc.status, map[string]any{"message": tc.message})
		_, err := New(srv.URL, "k").RecordCost(ctx(), []CostLine{{Code: "kwh", Quantity: 1, OccurredAt: costAt, IdempotencyKey: "k"}})
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Fatalf("%d %q: got %v, want %v", tc.status, tc.message, err, tc.want)
		}
		if !strings.Contains(err.Error(), tc.message) {
			t.Fatalf("server message lost: %v", err)
		}
	}
}

func TestLLMUsage_ExpandsToOneLinePerNonZeroKind(t *testing.T) {
	lines, err := LLMUsage{
		Model: "deepseek-v4", InputTokens: 1200, OutputTokens: 300,
		OccurredAt: costAt, ExternalUserID: "user-1", RunKey: "run-1", IdempotencyKey: "turn-9",
	}.lines(time.Now())
	if err != nil {
		t.Fatalf("lines: %v", err)
	}
	want := []CostLine{
		{Code: "deepseek-v4.input", Quantity: 1200, OccurredAt: costAt, IdempotencyKey: "turn-9.input", ExternalUserID: "user-1", RunKey: "run-1"},
		{Code: "deepseek-v4.output", Quantity: 300, OccurredAt: costAt, IdempotencyKey: "turn-9.output", ExternalUserID: "user-1", RunKey: "run-1"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %+v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %+v, want %+v", i, lines[i], want[i])
		}
	}
}

func TestLLMUsage_DefaultsOccurredAtToNow(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	lines, err := LLMUsage{Model: "m", CachedTokens: 5, IdempotencyKey: "k"}.lines(now)
	if err != nil {
		t.Fatalf("lines: %v", err)
	}
	if len(lines) != 1 || lines[0].Code != "m.cached" || lines[0].IdempotencyKey != "k.cached" || !lines[0].OccurredAt.Equal(now) {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestLLMUsage_RejectsNothingToRecord(t *testing.T) {
	for name, u := range map[string]LLMUsage{
		"no model":  {InputTokens: 1, IdempotencyKey: "k"},
		"no key":    {Model: "m", InputTokens: 1},
		"no tokens": {Model: "m", IdempotencyKey: "k"},
	} {
		if _, err := New("https://lungor.test", "k").RecordLLMUsage(ctx(), u); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("%s: got %v, want ErrBadRequest", name, err)
		}
	}
}

func TestRecordLLMUsage_PostsTheExpandedBatch(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{"accepted": 3, "inserted": 3})
	defer srv.Close()

	_, err := New(srv.URL, "k").RecordLLMUsage(ctx(), LLMUsage{
		Model: "m", InputTokens: 1, CachedTokens: 2, OutputTokens: 3, OccurredAt: costAt, IdempotencyKey: "t",
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if lines := rec.body["lines"].([]any); len(lines) != 3 {
		t.Fatalf("lines = %v", lines)
	}
}

func TestCostReport_ParsesRows(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{
		"group_by": "item", "from": "2026-09-01T00:00:00Z", "to": "2026-09-30T12:00:00.5Z",
		"rows": []map[string]any{{"key": "m.input", "currency": "EUR", "amount_micros": 42500, "quantity": 128000, "records": 12}},
	})
	defer srv.Close()

	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	r, err := New(srv.URL, "k").CostReport(ctx(), CostByItem, &from, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if rec.method != http.MethodGet || !strings.HasPrefix(rec.path, "/api/v1/metering/costs/report?") ||
		!strings.Contains(rec.path, "group_by=item") || !strings.Contains(rec.path, "from=2026-09-01") {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if r.GroupBy != CostByItem || !r.From.Equal(from) || r.To.IsZero() || len(r.Rows) != 1 {
		t.Fatalf("report = %+v", r)
	}
	if row := r.Rows[0]; row.Key != "m.input" || row.AmountMicros != 42500 || row.Quantity != 128000 || row.Records != 12 || row.Currency != "EUR" {
		t.Fatalf("row = %+v", row)
	}
}
