package fetch

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// listPageHTML is a page whose content is one large table after a short
// intro. Readability keeps this synthetic one; the real page it imitates
// is in testdata, gzipped, for the fallback itself.
func listPageHTML() string {
	var b strings.Builder
	b.WriteString(`<html><head><title>Countries by population</title></head><body>
<nav><a href="/">Home</a> <a href="/about">About</a></nav>
<div id="content"><p>This is a list of countries by population, based on estimates published in 2024.</p>
<table><tr><th>Rank</th><th>Country</th><th>Population</th></tr>`)
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&b, `<tr><td>%d</td><td><a href="/wiki/Country_%d">Country %d</a></td><td>%d,000,000</td></tr>`, i, i, i, 200-i)
	}
	b.WriteString(`</table></div><footer>Text is available under CC BY-SA.</footer></body></html>`)
	return b.String()
}

func TestFetchStaticRendersALargeTableInBothForms(t *testing.T) {
	srv := serveHTML(t, listPageHTML())
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL+"/list", 200000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if !strings.Contains(page.Markdown, "| Rank") || !strings.Contains(page.Markdown, "[Country 42](") {
		t.Errorf("Markdown lost the table:\n%.400s", page.Markdown)
	}
	if !strings.Contains(page.Text, "Country 42") {
		t.Errorf("Text lost the table rows:\n%.400s", page.Text)
	}
}

func serveGzippedFixture(t *testing.T, name string) *httptest.Server {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The Wikipedia list of countries by population: readability keeps its
// intro and drops the table that is the page, which the fallback restores.
func TestFetchStaticFallsBackToTheBodyOnAListPage(t *testing.T) {
	srv := serveGzippedFixture(t, "wikipedia-list-page.html.gz")

	page, err := FetchStatic(context.Background(), srv.URL+"/wiki/List_of_countries_by_population_(United_Nations)", 400000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	rows := 0
	for _, line := range strings.Split(page.Markdown, "\n") {
		if strings.HasPrefix(line, "|") {
			rows++
		}
	}
	if rows < 200 {
		t.Errorf("Markdown has %d table lines, want the country table", rows)
	}
	for _, want := range []string{"India", "China", "Monaco"} {
		if !strings.Contains(page.Text, want) {
			t.Errorf("Text lacks %q", want)
		}
	}
	if !strings.Contains(page.Title, "population") {
		t.Errorf("Title = %q", page.Title)
	}
	for _, noise := range []string{"Create account", "Log in", "Privacy policy"} {
		if strings.Contains(page.Text, noise) {
			t.Errorf("boilerplate %q leaked into the fallback text", noise)
		}
	}
}

func TestFetchStaticStillTrustsReadabilityOnAnArticle(t *testing.T) {
	srv := serveHTML(t, articleHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if page.Text == "" || strings.Contains(page.Text, "\t") {
		t.Errorf("an article should still come from readability: %q", page.Text)
	}
}
