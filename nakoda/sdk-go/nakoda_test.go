package nakoda

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lalternative/packages/go/busevents"
)

type received struct {
	method, path, auth string
	body               map[string]any
}

func server(t *testing.T, answers ...int) (*Client, *[]received) {
	t.Helper()
	var mu sync.Mutex
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		rec := received{method: r.Method, path: r.URL.EscapedPath(), auth: r.Header.Get("Authorization")}
		_ = json.NewDecoder(r.Body).Decode(&rec.body)
		got = append(got, rec)
		status := http.StatusAccepted
		if len(got) <= len(answers) {
			status = answers[len(got)-1]
		}
		w.WriteHeader(status)
		if status == http.StatusAccepted {
			_, _ = w.Write([]byte(`{"status":"recorded"}`))
		} else {
			_, _ = w.Write([]byte(`{"error":"why"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Key: "nakoda_key_x", URL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC) }
	c.wait = func(context.Context, time.Duration) error { return nil }
	return c, &got
}

func TestASignUpIsSentWithTheKeyAndAnIDDerivedFromThePerson(t *testing.T) {
	c, got := server(t)
	status, err := c.SignedUp(context.Background(), busevents.SignedUp{PersonID: "p1", Source: "newsletter"})
	if err != nil || status != "recorded" {
		t.Fatalf("status %q, err %v", status, err)
	}
	r := (*got)[0]
	if r.method != http.MethodPost || r.path != "/v1/events/account.signed_up" || r.auth != "Bearer nakoda_key_x" {
		t.Fatalf("request = %+v", r)
	}
	if r.body["event_id"] != "signed-up-p1" || r.body["person_id"] != "p1" || r.body["occurred_at"] != "2026-09-27T10:00:00Z" {
		t.Fatalf("body = %v", r.body)
	}
}

func TestWhatNakodaCannotAnswerIsSentAgainWithTheSameEvent(t *testing.T) {
	c, got := server(t, http.StatusServiceUnavailable, http.StatusBadGateway)
	status, err := c.SubscriptionRenewed(context.Background(), busevents.Subscription{PersonID: "p1", SubscriptionID: "s1", Currency: "EUR", Interval: busevents.Monthly})
	if err != nil || status != "recorded" {
		t.Fatalf("status %q, err %v", status, err)
	}
	if len(*got) != 3 {
		t.Fatalf("%d calls; want 3", len(*got))
	}
	if (*got)[0].body["event_id"] == "" || (*got)[0].body["event_id"] != (*got)[2].body["event_id"] {
		t.Fatal("a retry must carry the event id of the first try")
	}
	if (*got)[0].path != "/v1/events/subscription.renewed" {
		t.Fatalf("path %q", (*got)[0].path)
	}
}

func TestARefusalIsNotSentAgain(t *testing.T) {
	c, got := server(t, http.StatusUnprocessableEntity)
	_, err := c.Activated(context.Background(), busevents.Activated{PersonID: "p1"})
	var api *APIError
	if !errors.As(err, &api) || api.Status != http.StatusUnprocessableEntity || api.Reason != "why" || api.Temporary() {
		t.Fatalf("err = %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("%d calls; a 422 is final", len(*got))
	}
}

func TestRetriesStopAfterTheBudget(t *testing.T) {
	c, got := server(t, 503, 503, 503, 503, 503)
	if _, err := c.Forget(context.Background(), "p 1/2"); err == nil {
		t.Fatal("five 503s must fail")
	}
	if len(*got) != 4 || (*got)[0].method != http.MethodDelete || (*got)[0].path != "/v1/people/p%201%2F2" {
		t.Fatalf("calls = %+v", *got)
	}
}

func TestTheKeyComesFromTheEnvironment(t *testing.T) {
	t.Setenv("NAKODA_KEY", "")
	if _, err := New(Config{}); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("NAKODA_KEY", "from-env")
	c, err := New(Config{})
	if err != nil || c.key != "from-env" || c.base != DefaultURL {
		t.Fatalf("client %+v, err %v", c, err)
	}
}

func TestACancelledContextStopsTheRetries(t *testing.T) {
	c, _ := server(t, 503, 503, 503)
	ctx, cancel := context.WithCancel(context.Background())
	c.wait = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	if _, err := c.DailyVisitors(ctx, busevents.DailyVisitors{Date: "2026-09-26", Visitors: 3}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
