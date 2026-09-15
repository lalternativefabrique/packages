package fetch

import (
	"net/url"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
)

// The table plugin skips a table with a line break in a cell by default;
// Wikipedia's data tables all have one, and a skipped table is the page
// lost, so breaks are kept as spaces inside the cell instead.
var markdownConverter = converter.NewConverter(converter.WithPlugins(
	base.NewBasePlugin(),
	commonmark.NewCommonmarkPlugin(),
	table.NewTablePlugin(
		table.WithNewlineBehavior(table.NewlineBehaviorPreserve),
		table.WithSkipEmptyRows(true),
		table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
	),
))

func renderMarkdown(node *html.Node, page *url.URL) string {
	prepareForMarkdown(node, page)
	out, err := markdownConverter.ConvertNode(node)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// prepareForMarkdown rewrites the DOM for a reader that is a model, not a
// browser: links and images resolve against the page so a fragment keeps
// pointing into it, link titles go since they repeat the link text, and
// footnote markers go since they carry nothing without the notes.
func prepareForMarkdown(node *html.Node, page *url.URL) {
	var drop []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "a":
				if isFootnoteMarker(n) || isEmptyLink(n) {
					drop = append(drop, n)
					return
				}
				resolveAttr(n, "href", page)
				removeAttr(n, "title")
			case "img":
				if strings.TrimSpace(attr(n, "alt")) == "" {
					drop = append(drop, n)
					return
				}
				resolveAttr(n, "src", page)
				removeAttr(n, "title")
			case "sup":
				if hasClass(n, "reference") {
					drop = append(drop, n)
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(node)
	for _, n := range drop {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
}

// isFootnoteMarker recognises a "[1]"-style link into the page's own notes.
func isFootnoteMarker(a *html.Node) bool {
	href := attr(a, "href")
	return strings.Contains(href, "#cite_note") || strings.Contains(href, "#cite_ref")
}

// isEmptyLink is an anchor with nothing to show: no text, no described
// image. Icon links and edit links come out as "[](url)" otherwise, by the
// hundred, and an image with no alt text is nothing a model can read.
func isEmptyLink(a *html.Node) bool {
	var has func(*html.Node) bool
	has = func(n *html.Node) bool {
		if n.Type == html.TextNode && strings.TrimSpace(n.Data) != "" {
			return true
		}
		if n.Type == html.ElementNode && n.Data == "img" && strings.TrimSpace(attr(n, "alt")) != "" {
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if has(c) {
				return true
			}
		}
		return false
	}
	return !has(a)
}

func resolveAttr(n *html.Node, name string, page *url.URL) {
	if page == nil {
		return
	}
	for i, a := range n.Attr {
		if a.Key != name {
			continue
		}
		ref, err := url.Parse(strings.TrimSpace(a.Val))
		if err != nil {
			return
		}
		n.Attr[i].Val = page.ResolveReference(ref).String()
		return
	}
}

func removeAttr(n *html.Node, name string) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != name {
			kept = append(kept, a)
		}
	}
	n.Attr = kept
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
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
