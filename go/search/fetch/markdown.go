package fetch

import (
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

var markdownConverter = converter.NewConverter(converter.WithPlugins(
	base.NewBasePlugin(),
	commonmark.NewCommonmarkPlugin(),
	table.NewTablePlugin(),
))

func renderMarkdown(node *html.Node, domain string) string {
	out, err := markdownConverter.ConvertNode(node, converter.WithDomain(domain))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// truncateMarkdown cuts at the last line break inside max, so a table row or
// a heading is dropped whole rather than left half-written.
func truncateMarkdown(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	kept := string(runes[:max])
	if i := strings.LastIndex(kept, "\n"); i > 0 {
		kept = kept[:i]
	}
	return strings.TrimSpace(kept) + "\n…"
}
