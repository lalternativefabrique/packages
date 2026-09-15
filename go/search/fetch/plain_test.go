package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const llmsTxt = "# Firecrawl Docs\n\n- [Introduction](https://docs.firecrawl.dev/introduction.md): Search the web.\n- [Crawl](https://docs.firecrawl.dev/features/crawl.md): Recursively crawl a site.\n"

func servePlain(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchStaticPassesMarkdownThroughUntouched(t *testing.T) {
	srv := servePlain(t, "text/markdown; charset=utf-8", llmsTxt)

	page, err := FetchStatic(context.Background(), srv.URL+"/llms.txt", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	want := "# Firecrawl Docs\n\n- [Introduction](https://docs.firecrawl.dev/introduction.md): Search the web.\n- [Crawl](https://docs.firecrawl.dev/features/crawl.md): Recursively crawl a site."
	if page.Markdown != want {
		t.Errorf("Markdown = %q, want the file as served", page.Markdown)
	}
	if page.Text != want || page.Title != "llms" {
		t.Errorf("Text = %q, Title = %q", page.Text, page.Title)
	}
}

func TestFetchStaticReadsATxtByNameWhenTheTypeIsMissing(t *testing.T) {
	srv := servePlain(t, "", "plain words\nsecond line")

	page, err := FetchStatic(context.Background(), srv.URL+"/notes.txt", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if page.Text != "plain words\nsecond line" || page.Title != "notes" {
		t.Errorf("page = %+v", page)
	}
}

func TestFetchStaticTitlesAnUntitledPageByItsFile(t *testing.T) {
	srv := servePlain(t, "text/html", `<html><body><article><p>Un paragraphe assez long pour que readability le garde comme contenu principal de la page servie.</p></article></body></html>`)

	page, err := FetchStatic(context.Background(), srv.URL+"/docs/guide.html", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if page.Title != "guide" {
		t.Errorf("Title = %q, want the file name", page.Title)
	}
}
