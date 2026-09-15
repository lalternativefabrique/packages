package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

const tableHTML = `<html><head><title>Exemple — offres</title></head><body><article>
<h2>Tarifs</h2>
<p>Nos offres sont pensées pour accompagner chaque équipe, de la première expérimentation
au déploiement en production sur des volumes importants, avec un support adapté.</p>
<table>
<tr><th>Plan</th><th>Prix</th><th>Requêtes</th></tr>
<tr><td>Free</td><td>0 €</td><td>500</td></tr>
<tr><td>Pro</td><td>49 €</td><td>50 000</td></tr>
</table>
<p>Voir la <a href="/pricing">grille complète</a> pour le détail des options et des
engagements de disponibilité qui accompagnent chacun des plans proposés ici.</p>
</article></body></html>`

func TestFetchStaticRendersMarkdownWithTablesAndLinks(t *testing.T) {
	srv := serveHTML(t, tableHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	for _, want := range []string{"## Tarifs", "| Plan | Prix | Requêtes |", "| Pro  | 49 € | 50 000   |", "[grille complète](" + srv.URL + "/pricing)"} {
		if !strings.Contains(page.Markdown, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, page.Markdown)
		}
	}
	if strings.Contains(page.Text, "|") {
		t.Errorf("Text should stay plain, got %q", page.Text)
	}
}

func TestFetchStaticTruncatesMarkdownOnALineBreak(t *testing.T) {
	srv := serveHTML(t, tableHTML)
	defer srv.Close()

	page, err := FetchStatic(context.Background(), srv.URL, 120, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if len([]rune(page.Markdown)) > 122 {
		t.Errorf("Markdown not truncated: %d runes", len([]rune(page.Markdown)))
	}
	if !strings.HasSuffix(page.Markdown, "\n…") {
		t.Errorf("Markdown should end on a whole line then an ellipsis, got %q", page.Markdown)
	}
}
