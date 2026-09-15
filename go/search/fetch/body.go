package fetch

import (
	"strings"

	"golang.org/x/net/html"
)

// keepShare is the share of the page's text readability must keep for its
// article to be trusted. Measured on Wikipedia: ordinary articles keep
// 0.90-0.95, and a list page whose content is one large table keeps 0.16
// because the table is scored as a sibling, not as the article.
const keepShare = 0.5

var boilerplateTags = map[string]bool{
	"script": true, "style": true, "noscript": true, "template": true, "svg": true, "iframe": true,
	"nav": true, "header": true, "footer": true, "aside": true, "form": true, "button": true,
}

var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "main": true, "blockquote": true, "pre": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true, "dl": true, "dt": true, "dd": true,
	"table": true, "thead": true, "tbody": true, "tr": true, "caption": true, "br": true, "hr": true, "figure": true, "figcaption": true,
}

// pruneBoilerplate drops what no reader wants from a whole-page fallback:
// scripts, navigation, forms, and anything a site labels as such.
func pruneBoilerplate(n *html.Node) {
	var drop []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (boilerplateTags[c.Data] || isNavigationRole(c)) {
			drop = append(drop, c)
			continue
		}
		pruneBoilerplate(c)
	}
	for _, c := range drop {
		n.RemoveChild(c)
	}
}

func isNavigationRole(n *html.Node) bool {
	switch attr(n, "role") {
	case "navigation", "banner", "contentinfo", "complementary", "search":
		return true
	}
	return false
}

func bodyOf(doc *html.Node) *html.Node {
	var body *html.Node
	var find func(*html.Node)
	find = func(n *html.Node) {
		if body != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "body" {
			body = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	if body == nil {
		return doc
	}
	return body
}

func textRunes(n *html.Node) int {
	if n.Type == html.TextNode {
		return len([]rune(strings.TrimSpace(n.Data)))
	}
	total := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		total += textRunes(c)
	}
	return total
}

// renderText flattens a node the way readability renders an article: a
// line per block, a tab between table cells.
func renderText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(strings.Join(strings.Fields(n.Data), " "))
			b.WriteString(" ")
		case html.ElementNode:
			if blockTags[n.Data] {
				b.WriteString("\n")
			}
			if n.Data == "td" || n.Data == "th" {
				b.WriteString("\t")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockTags[n.Data] {
			b.WriteString("\n")
		}
	}
	walk(n)
	return tidyText(b.String())
}

func tidyText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(strings.ReplaceAll(line, " \t", "\t"))
		if line == "" {
			if !blank && len(out) > 0 {
				out = append(out, "")
			}
			blank = true
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func documentTitle(doc *html.Node) string {
	var title string
	var find func(*html.Node)
	find = func(n *html.Node) {
		if title != "" {
			return
		}
		if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
			title = strings.TrimSpace(n.FirstChild.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			find(c)
		}
	}
	find(doc)
	return title
}
