package sdk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func agentSeen(t *testing.T, answer string) (*httptest.Server, *string) {
	t.Helper()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if lines, ok := body["lines"].([]any); ok {
			body = lines[0].(map[string]any)
		}
		seen, _ = body["agent_id"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestConsumeSendsTheAgent(t *testing.T) {
	srv, seen := agentSeen(t, `{"allowed":true,"balance":1,"recorded":true}`)
	if _, err := New(srv.URL, "k").Consume(ctx(), Usage{ExternalUserID: "u1", Unit: "tok", Quantity: 1, IdempotencyKey: "k1", AgentID: "ag-station"}); err != nil {
		t.Fatal(err)
	}
	if *seen != "ag-station" {
		t.Fatalf("agent_id = %q", *seen)
	}
}

func TestRecordCostSendsTheAgent(t *testing.T) {
	srv, seen := agentSeen(t, `{"accepted":1,"inserted":1,"lines":[]}`)
	line := CostLine{Code: "kwh", Quantity: 1, OccurredAt: time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC), IdempotencyKey: "k", AgentID: "ag-station"}
	if _, err := New(srv.URL, "k").RecordCost(ctx(), []CostLine{line}); err != nil {
		t.Fatal(err)
	}
	if *seen != "ag-station" {
		t.Fatalf("agent_id = %q", *seen)
	}
}

func TestUsageRequestCarriesTheFeature(t *testing.T) {
	req, err := Usage{ExternalUserID: "u1", Unit: "tok", Quantity: 1, IdempotencyKey: "k1", Feature: "chat"}.request()
	if err != nil {
		t.Fatal(err)
	}
	if req.Feature == nil || *req.Feature != "chat" {
		t.Fatalf("feature = %v", req.Feature)
	}
}
