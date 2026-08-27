package search

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvider struct {
	results []Result
	err     error
	delay   time.Duration
}

func (f *fakeProvider) Search(ctx context.Context, q Query) (*Response, error) {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &Response{Query: q.Text, Results: f.results}, nil
}

func TestMergeDeduplicatesAndOrders(t *testing.T) {
	a := &fakeProvider{results: []Result{
		{URL: "https://shared.example", Title: "shared", Score: 3.0},
		{URL: "https://web.example", Title: "web", Score: 1.0},
	}}
	b := &fakeProvider{results: []Result{
		{URL: "https://shared.example", Title: "shared", Score: 0.1},
		{URL: "https://cairn.example", Title: "cairn", Score: 2.0},
	}}

	res, err := Merge(context.Background(), []Provider{a, b}, Query{Text: "q", Limit: 10}, time.Second)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(res.Results) != 3 {
		t.Fatalf("got %d results, want 3 (one URL is in both providers)", len(res.Results))
	}
	if res.Results[0].URL != "https://shared.example" {
		t.Errorf("first = %q, want the URL both providers returned", res.Results[0].URL)
	}
	if res.Partial {
		t.Error("Partial set although both providers answered")
	}
}

// Raw scores are not comparable across providers: one list peaks near 4.0
// where the other caps at 1.0. Sorting the concatenation by score buries
// every result from whichever provider scores lowest — RRF prevents that.
func TestMergeKeepsBothProvidersInTheHead(t *testing.T) {
	a := &fakeProvider{results: []Result{
		{URL: "https://web1.example", Score: 0.2},
		{URL: "https://web2.example", Score: 0.1},
	}}
	b := &fakeProvider{results: []Result{
		{URL: "https://acad1.example", Score: 1.0},
		{URL: "https://acad2.example", Score: 1.0},
	}}

	res, err := Merge(context.Background(), []Provider{a, b}, Query{Text: "q", Limit: 2}, time.Second)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	var fromA, fromB int
	for _, r := range res.Results {
		switch r.URL {
		case "https://web1.example", "https://web2.example":
			fromA++
		default:
			fromB++
		}
	}
	if fromA == 0 || fromB == 0 {
		t.Errorf("top 2 = %v, want one result from each provider", res.Results)
	}
}

func TestMergeReturnsBeforeTheSlowProvider(t *testing.T) {
	deadline := 100 * time.Millisecond
	fast := &fakeProvider{results: []Result{{URL: "https://web.example"}}}
	slow := &fakeProvider{results: []Result{{URL: "https://slow.example"}}, delay: deadline + 2*time.Second}

	start := time.Now()
	res, err := Merge(context.Background(), []Provider{fast, slow}, Query{Text: "q", Limit: 10}, deadline)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if elapsed >= deadline+2*time.Second {
		t.Errorf("waited %s, want a return near the %s deadline", elapsed, deadline)
	}
	if len(res.Results) == 0 {
		t.Error("no results although the fast provider answered in time")
	}
	if !res.Partial {
		t.Error("Partial not set although the slow provider missed the deadline")
	}
}

func TestMergeFlagsPartialOnError(t *testing.T) {
	ok := &fakeProvider{results: []Result{{URL: "https://web.example"}}}
	bad := &fakeProvider{err: errors.New("boom")}

	res, err := Merge(context.Background(), []Provider{ok, bad}, Query{Text: "q", Limit: 10}, time.Second)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !res.Partial {
		t.Error("Partial not set although one provider failed")
	}
	if len(res.Results) != 1 || res.Results[0].URL != "https://web.example" {
		t.Errorf("got %v, want the surviving result", res.Results)
	}
}
