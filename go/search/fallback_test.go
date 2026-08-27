package search

import (
	"context"
	"errors"
	"testing"
)

func TestWithFallbackUsesPrimaryWhenItHasResults(t *testing.T) {
	primary := &fakeProvider{results: []Result{{URL: "https://primary.example"}}}
	secondary := &fakeProvider{results: []Result{{URL: "https://secondary.example"}}}

	res, err := WithFallback(primary, secondary).Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].URL != "https://primary.example" {
		t.Errorf("got %v, want primary's result", res.Results)
	}
}

func TestWithFallbackFallsBackOnError(t *testing.T) {
	primary := &fakeProvider{err: errors.New("boom")}
	secondary := &fakeProvider{results: []Result{{URL: "https://secondary.example"}}}

	res, err := WithFallback(primary, secondary).Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].URL != "https://secondary.example" {
		t.Errorf("got %v, want secondary's result", res.Results)
	}
}

func TestWithFallbackFallsBackOnEmptyResults(t *testing.T) {
	primary := &fakeProvider{results: nil}
	secondary := &fakeProvider{results: []Result{{URL: "https://secondary.example"}}}

	res, err := WithFallback(primary, secondary).Search(context.Background(), Query{Text: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].URL != "https://secondary.example" {
		t.Errorf("got %v, want secondary's result", res.Results)
	}
}
