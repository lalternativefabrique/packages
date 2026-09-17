package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNotConfiguredWithoutBaseURL(t *testing.T) {
	c := New("", "key")
	if _, err := c.Search(context.Background(), SearchQuery{Query: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Search = %v, want ErrNotConfigured", err)
	}
	if _, err := c.Fetch(context.Background(), FetchRequest{URL: "https://example.com"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Fetch = %v, want ErrNotConfigured", err)
	}
}

func TestSearchSendsTheQueryAndReadsResults(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %s, want /search", r.URL.Path)
		}
		if k := r.Header.Get("X-Tornad-Key"); k != "app-key" {
			t.Errorf("key header = %q, want app-key", k)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"query":"gramsci","partial":true,"results":[
			{"title":"T","url":"https://e.com","score":1.5,"markdown":"# T",
			 "open_graph":{"site_name":"E"}}]}`))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "app-key").Search(context.Background(), SearchQuery{
		Query: "gramsci", Categories: []string{CategoryGeneral, CategoryAcademic},
		Content: 3, Format: FormatMarkdown,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got["q"] != "gramsci" {
		t.Errorf("q = %v", got["q"])
	}
	if got["format"] != "markdown" {
		t.Errorf("format = %v, want markdown", got["format"])
	}
	if !res.Partial {
		t.Error("Partial not carried through")
	}
	if len(res.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(res.Results))
	}
	r := res.Results[0]
	if r.Title != "T" || r.Markdown != "# T" || r.Score != 1.5 {
		t.Errorf("result = %+v", r)
	}
	if r.OpenGraph == nil || r.OpenGraph.SiteName != "E" {
		t.Errorf("open graph = %+v", r.OpenGraph)
	}
}

// An unset field must not be sent: tornad defaults them, and a zero on the wire
// would override the default with something the caller never asked for.
func TestUnsetFieldsAreOmitted(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"query":"x"}`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "").Search(context.Background(), SearchQuery{Query: "x"}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, k := range []string{"limit", "content", "content_runes", "format", "deadline_ms", "categories"} {
		if _, sent := got[k]; sent {
			t.Errorf("%s was sent although it was never set", k)
		}
	}
}

// A page tornad could not read is routine on the open web, not an outage: a
// caller with a static fallback must be able to tell the two apart.
func TestUpstreamFailureIsItsOwnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"refused by publisher"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "").Fetch(context.Background(), FetchRequest{URL: "https://e.com"})
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("Fetch = %v, want ErrUpstream", err)
	}
	if errors.Is(err, ErrUnavailable) {
		t.Error("a refused page must not read as tornad being down")
	}
	if got := err.Error(); !contains(got, "refused by publisher") {
		t.Errorf("error = %q, want it to carry what tornad reported", got)
	}
}

func TestStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		code int
		want error
	}{
		{http.StatusBadRequest, ErrBadRequest},
		{http.StatusUnauthorized, ErrUnauthorized},
		{http.StatusForbidden, ErrUnauthorized},
		{http.StatusServiceUnavailable, ErrUnavailable},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(tc.code)
			w.Write([]byte(`{"error":"no"}`))
		}))
		_, err := New(srv.URL, "").Search(context.Background(), SearchQuery{Query: "x"})
		if !errors.Is(err, tc.want) {
			t.Errorf("status %d = %v, want %v", tc.code, err, tc.want)
		}
		srv.Close()
	}
}

func TestFetchReadsThePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"title":"T","text":"body","pages":["a","b"]}`))
	}))
	defer srv.Close()

	p, err := New(srv.URL, "").Fetch(context.Background(), FetchRequest{URL: "https://e.com", Paginate: 100})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if p.Title != "T" || p.Text != "body" || len(p.Pages) != 2 {
		t.Errorf("page = %+v", p)
	}
}

func TestCrawlStatusCarriesPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "20" {
			t.Errorf("offset = %q, want 20", r.URL.Query().Get("offset"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"j1","status":"running","pages":21,"next":40,
			"results":[{"url":"https://e.com/a","depth":1}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "").CrawlStatus(context.Background(), CrawlStatusRequest{ID: "j1", Offset: 20})
	if err != nil {
		t.Fatalf("CrawlStatus: %v", err)
	}
	if got.ID != "j1" || got.Pages != 21 {
		t.Errorf("job = %+v", got.CrawlJob)
	}
	if got.Next == nil || *got.Next != 40 {
		t.Errorf("next = %v, want 40", got.Next)
	}
	if len(got.Results) != 1 || got.Results[0].URL != "https://e.com/a" {
		t.Errorf("results = %+v", got.Results)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
