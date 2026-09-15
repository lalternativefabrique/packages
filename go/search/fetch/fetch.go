// Package fetch downloads one page and extracts its main content, as plain
// text and as markdown.
//
// Forked from Synthiz's apps/core/cerveau/infrastructure/fetch_url_tool.go.
package fetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"

	"github.com/lalternative/packages/go/search"
)

const (
	// fetchUserAgent identifies requests as an automated reader rather than a
	// browser, since some sites otherwise serve different (often lighter,
	// JS-only) markup.
	fetchUserAgent = "Mozilla/5.0 (compatible; SearchLib/1.0; +https://github.com/lalternative/packages)"

	// fetchTimeout bounds one page fetch. It is generous because a proxied
	// request adds the proxy's own hop to whatever the origin takes.
	fetchTimeout = 20 * time.Second
)

// Page is the extracted content of one fetched URL. Text is the main
// content flattened for reading aloud or matching; Markdown is the same
// content with its headings, lists, tables and links kept, for a model.
//
// URL is the address actually read, which a redirect can make differ from
// the one asked for. Links are every http(s) link of the whole document,
// absolute, for a caller that walks a site.
type Page struct {
	URL      string
	Title    string
	Text     string
	Markdown string
	Links    []string

	OpenGraph *search.OpenGraph
	Favicon   string
}

// FetchStatic downloads url and extracts its main text via readability. It
// does not execute JavaScript: a page whose content is rendered client-side
// yields an empty Page.Text, not an error — callers that need JS rendering
// should use FetchWithFallback with a Renderer.
//
// maxRunes caps how much text Page.Text holds; use Page.Paginate to walk the
// rest instead of raising this without bound.
//
// Pass a non-nil cache to skip the network round trip and the readability
// parse on a URL fetched before. The cache holds the full, untruncated page,
// so a later call with a different maxRunes still gets the truncation it
// asked for.
func FetchStatic(ctx context.Context, rawURL string, maxRunes int, cache Cache) (*Page, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("url must be http or https")
	}

	full, err := fetchFull(ctx, rawURL, parsed, cache)
	if err != nil {
		return nil, err
	}
	return full.truncated(maxRunes), nil
}

// truncated applies maxRunes to both renderings of the page.
func (p *Page) truncated(maxRunes int) *Page {
	out := *p
	out.Text = truncateRunes(p.Text, maxRunes)
	out.Markdown = truncateMarkdown(p.Markdown, maxRunes)
	return &out
}

// fetchFull returns the page's full, untruncated content, consulting and
// populating cache around the network fetch.
func fetchFull(ctx context.Context, rawURL string, parsed *url.URL, cache Cache) (*Page, error) {
	if cache != nil {
		if page, ok := cache.Get(rawURL); ok {
			return page, nil
		}
	}

	body, final, contentType, err := httpGet(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var page *Page
	if isPDF(contentType, final) {
		page = extractPDF(ctx, body, final)
	} else {
		page = extract(body, final)
	}
	if cache != nil {
		cache.Set(rawURL, page)
	}
	return page, nil
}

func httpGet(ctx context.Context, rawURL string) (body io.ReadCloser, final *url.URL, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/pdf;q=0.9,*/*;q=0.8")

	resp, err := httpClient(fetchTimeout).Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("fetch page: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, nil, "", fmt.Errorf("fetch page: status %d", resp.StatusCode)
	}
	return resp.Body, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}

// extract isolates the readability call: the library panics on malformed
// DOMs and would otherwise take the whole caller down with it. Links are
// read off the document before readability prunes it.
func extract(body io.Reader, page *url.URL) (out *Page) {
	out = &Page{URL: page.String()}
	defer func() {
		if recover() != nil {
			out = &Page{URL: page.String()}
		}
	}()

	doc, err := html.Parse(body)
	if err != nil {
		return out
	}
	out.Links = collectLinks(doc, page)

	article, err := readability.FromDocument(doc, page)
	if err != nil {
		return out
	}
	var buf strings.Builder
	if err := article.RenderText(&buf); err != nil {
		return out
	}
	out.Title = strings.TrimSpace(article.Title())
	out.Text = strings.TrimSpace(buf.String())
	out.Markdown = renderMarkdown(article.Node, page)
	return out
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}
