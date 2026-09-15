package fetch

import (
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func markdownOf(t *testing.T, fragment, page string) string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader("<html><body><article>" + fragment + "</article></body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := url.Parse(page)
	if err != nil {
		t.Fatal(err)
	}
	return renderMarkdown(doc, base)
}

func TestMarkdownResolvesLinksAgainstThePage(t *testing.T) {
	md := markdownOf(t, `<p>See <a href="#history">history</a>, <a href="/wiki/UTC">UTC</a> and <a href="https://iso.org">ISO</a>.</p>`,
		"https://en.wikipedia.org/wiki/ISO_8601")
	for _, want := range []string{
		"[history](https://en.wikipedia.org/wiki/ISO_8601#history)",
		"[UTC](https://en.wikipedia.org/wiki/UTC)",
		"[ISO](https://iso.org)",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

func TestMarkdownDropsLinkTitles(t *testing.T) {
	md := markdownOf(t, `<p><a href="/wiki/UTC" title="UTC">UTC</a></p>`, "https://en.wikipedia.org/wiki/ISO_8601")
	if strings.Contains(md, `"UTC"`) {
		t.Errorf("title attribute leaked into markdown: %s", md)
	}
}

func TestMarkdownDropsFootnoteMarkers(t *testing.T) {
	md := markdownOf(t, `<p>Published in 1988.<sup class="reference"><a href="#cite_note-1">[1]</a></sup> Revised<a href="#cite_note-2">[2]</a> later.</p>`,
		"https://en.wikipedia.org/wiki/ISO_8601")
	if strings.Contains(md, "cite_note") || strings.Contains(md, "[1]") || strings.Contains(md, "[2]") {
		t.Errorf("footnote markers leaked into markdown: %s", md)
	}
	if !strings.Contains(md, "Published in 1988.") || !strings.Contains(md, "Revised later.") {
		t.Errorf("prose around the markers was damaged: %s", md)
	}
}
