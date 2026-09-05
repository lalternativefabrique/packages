package tornade

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lalternative/packages/go/search"
)

func TestNewWithoutBaseURLIsNil(t *testing.T) {
	if got := New("  ", nil); got != nil {
		t.Fatalf("New(empty) = %v, want nil", got)
	}
}

func TestSearchSendsTornadeShapedRequest(t *testing.T) {
	var got request
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		json.NewEncoder(w).Encode(response{Query: "gramsci"})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", nil)
	_, err := c.Search(
		context.Background(),
		search.Query{Text: "gramsci", Limit: 8, Language: "fr"},
		[]search.Category{search.CategoryGeneral, search.CategoryAcademic},
		4*time.Second,
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if path != "/search" {
		t.Errorf("path = %q, want /search", path)
	}
	if got.Q != "gramsci" || got.Limit != 8 || got.Language != "fr" {
		t.Errorf("request = %+v, want the query echoed through", got)
	}
	if got.DeadlineMS != 4000 {
		t.Errorf("deadline_ms = %d, want 4000", got.DeadlineMS)
	}
	// CategoryAcademic's value is SearXNG's engine name; tornade's API takes
	// the short alias, and sending the raw value earns a 400.
	want := []string{"general", "academic"}
	if len(got.Categories) != len(want) {
		t.Fatalf("categories = %v, want %v", got.Categories, want)
	}
	for i, name := range want {
		if got.Categories[i] != name {
			t.Errorf("categories[%d] = %q, want %q", i, got.Categories[i], name)
		}
	}
}

func TestSearchDecodesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response{
			Query:   "gramsci",
			Partial: true,
			Results: []result{{
				Title:       "Quaderni",
				URL:         "https://example.org/q",
				Description: "notes",
				Source:      "duckduckgo",
				SourceType:  "web",
				Score:       0.42,
				OpenGraph:   &openGraph{Image: "https://example.org/i.png", SiteName: "Example"},
			}},
		})
	}))
	defer srv.Close()

	res, err := New(srv.URL, nil).Search(context.Background(), search.Query{Text: "gramsci"}, nil, 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !res.Partial {
		t.Error("Partial = false, want true — a dropped category must stay visible to the caller")
	}
	if len(res.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(res.Results))
	}
	r := res.Results[0]
	if r.Title != "Quaderni" || r.URL != "https://example.org/q" || r.Source != "duckduckgo" {
		t.Errorf("result = %+v, want the hit decoded", r)
	}
	if r.OpenGraph == nil || r.OpenGraph.SiteName != "Example" {
		t.Errorf("OpenGraph = %+v, want it carried over", r.OpenGraph)
	}
}

func TestSearchReportsUpstreamStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":"search is not configured"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, nil).Search(context.Background(), search.Query{Text: "x"}, nil, 0)
	if err == nil {
		t.Fatal("Search returned nil error on a 503")
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an empty query must not reach tornade")
	}))
	defer srv.Close()

	if _, err := New(srv.URL, nil).Search(context.Background(), search.Query{Text: " "}, nil, 0); err == nil {
		t.Fatal("Search(empty) returned nil error")
	}
}
