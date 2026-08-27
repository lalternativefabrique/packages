package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const articleHTML = `<!doctype html>
<html><head><title>A real article</title></head>
<body>
<article>
<h1>A real article</h1>
<p>This is the first paragraph of a normal article with enough text for
readability to consider it the main content of the page, rather than
boilerplate navigation or a footer.</p>
<p>A second paragraph adds more substance so the extracted text comfortably
clears any minimum-length heuristic a caller might apply downstream.</p>
</article>
</body></html>`

const jsOnlyShellHTML = `<!doctype html>
<html><head><title>App</title></head>
<body><div id="root"></div><script src="/app.js"></script></body></html>`

const malformedHTML = `<html><body><p>unclosed paragraph <div>broken nesting</body>`

func serveHTML(t *testing.T, html string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(html))
	}))
}

func TestFetchStaticExtractsArticleText(t *testing.T) {
	srv := serveHTML(t, articleHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if page.Text == "" {
		t.Error("Text is empty, want extracted article text")
	}
}

func TestFetchStaticYieldsEmptyTextOnJSOnlyShell(t *testing.T) {
	srv := serveHTML(t, jsOnlyShellHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if len([]rune(page.Text)) >= minUsableRunes {
		t.Errorf("Text = %q, want little or no text from a JS-only shell", page.Text)
	}
}

func TestFetchStaticRecoversFromMalformedHTML(t *testing.T) {
	srv := serveHTML(t, malformedHTML)
	defer srv.Close()

	if _, err := FetchStatic(context.Background(), srv.URL, 6000, nil); err != nil {
		t.Fatalf("FetchStatic: %v, want no error even on malformed markup", err)
	}
}

func TestFetchStaticRejectsNonHTTPScheme(t *testing.T) {
	if _, err := FetchStatic(context.Background(), "ftp://example.com", 6000, nil); err == nil {
		t.Error("want an error for a non-http(s) scheme")
	}
}

func TestFetchStaticTruncatesToMaxRunes(t *testing.T) {
	srv := serveHTML(t, articleHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 10, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if len([]rune(page.Text)) > 11 { // 10 runes + the ellipsis
		t.Errorf("Text has %d runes, want at most 11 (10 + ellipsis)", len([]rune(page.Text)))
	}
}
