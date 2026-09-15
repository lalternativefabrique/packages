package fetch

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// collectLinks lists every http(s) link in doc, absolute and without its
// fragment, in document order and without repeats. It reads the whole
// document rather than readability's article: navigation is exactly what
// readability strips and exactly what a crawler needs.
func collectLinks(doc *html.Node, page *url.URL) []string {
	seen := make(map[string]bool)
	var out []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if link, ok := resolveLink(attr(n, "href"), page); ok && !seen[link] {
				seen[link] = true
				out = append(out, link)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

func resolveLink(href string, page *url.URL) (string, bool) {
	href = strings.TrimSpace(href)
	if href == "" || page == nil {
		return "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	abs := page.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return "", false
	}
	abs.Fragment = ""
	return abs.String(), true
}
