package searxng

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/search"
)

func TestSearchSendsResolvedLanguage(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("language")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	p := New(srv.URL, srv.Client())
	if _, err := p.Search(context.Background(), search.Query{Text: "q", Category: search.CategoryGeneral, Limit: 10, Language: "fr"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "fr" {
		t.Errorf("language = %q, want %q", got, "fr")
	}
}

func TestSearchOmitsEmptyLanguage(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.URL.Query()["language"]
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	p := New(srv.URL, srv.Client())
	if _, err := p.Search(context.Background(), search.Query{Text: "q", Category: search.CategoryGeneral, Limit: 10}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if present {
		t.Error("language sent despite being empty; SearxNG default_lang should apply")
	}
}

// SearxNG does not reject an unknown category: it silently falls back to
// "general" and returns web results, so an underscore here would degrade the
// academic route with no error anywhere.
func TestCategoryAcademicIsSpelledWithASpace(t *testing.T) {
	if search.CategoryAcademic != "scientific publications" {
		t.Errorf("CategoryAcademic = %q, want %q", search.CategoryAcademic, "scientific publications")
	}
}

// SearxNG returns results grouped by engine, not ordered by the score it
// computed, so truncating to limit without sorting first drops the best hits.
func TestSearchSortsByScoreBeforeTruncating(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[
			{"url":"https://low.example","title":"low","score":0.2},
			{"url":"https://mid.example","title":"mid","score":0.9},
			{"url":"https://top.example","title":"top","score":4.0}
		]}`))
	}))
	defer srv.Close()

	p := New(srv.URL, srv.Client())
	res, err := p.Search(context.Background(), search.Query{Text: "q", Category: search.CategoryGeneral, Limit: 2, Language: "fr"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(res.Results))
	}
	if res.Results[0].URL != "https://top.example" || res.Results[1].URL != "https://mid.example" {
		t.Errorf("got %q then %q, want top then mid", res.Results[0].URL, res.Results[1].URL)
	}
}

func TestSearchSendsAcademicCategory(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("categories")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()

	p := New(srv.URL, srv.Client())
	if _, err := p.Search(context.Background(), search.Query{Text: "q", Category: search.CategoryAcademic, Limit: 10, Language: "en"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got != "scientific publications" {
		t.Errorf("categories = %q, want %q", got, "scientific publications")
	}
}
