package search

import (
	"context"
	"math/rand/v2"
	"reflect"
	"testing"
	"time"
)

type delayedProvider struct {
	delay   time.Duration
	results []Result
}

func (d delayedProvider) Search(ctx context.Context, q Query) (*Response, error) {
	select {
	case <-time.After(d.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &Response{Query: q.Text, Results: d.results}, nil
}

func TestMergeIgnoresArrivalOrder(t *testing.T) {
	lists := [][]Result{
		{{URL: "https://a", Title: "from first"}, {URL: "https://b"}},
		{{URL: "https://c"}, {URL: "https://a", Title: "from second"}},
		{{URL: "https://d"}, {URL: "https://e"}},
	}
	run := func(delays []time.Duration) []Result {
		providers := make([]Provider, len(lists))
		for i := range lists {
			providers[i] = delayedProvider{delay: delays[i], results: lists[i]}
		}
		res, err := Merge(context.Background(), providers, Query{}, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return res.Results
	}
	want := run([]time.Duration{0, 5 * time.Millisecond, 10 * time.Millisecond})
	for range 20 {
		delays := []time.Duration{0, 5 * time.Millisecond, 10 * time.Millisecond}
		rand.Shuffle(len(delays), func(i, j int) { delays[i], delays[j] = delays[j], delays[i] })
		if got := run(delays); !reflect.DeepEqual(got, want) {
			t.Fatalf("arrival order %v changed the output:\n%+v\nwant\n%+v", delays, got, want)
		}
	}
	if want[0].URL != "https://a" || want[0].Title != "from first" {
		t.Fatalf("duplicate must keep the first provider's fields: %+v", want[0])
	}
	if want[1].URL != "https://c" || want[2].URL != "https://d" {
		t.Fatalf("rank-1 ties must order by provider priority: %+v", want)
	}
}

func TestEqualScoresOrderByProviderPriority(t *testing.T) {
	got := fuseByRank([][]Result{nil, {{URL: "https://z"}}, {{URL: "https://y"}}}, 0)
	if got[0].URL != "https://z" || got[1].URL != "https://y" {
		t.Fatalf("an empty first provider must not shift priority: %+v", got)
	}
}
