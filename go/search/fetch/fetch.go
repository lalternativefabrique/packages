// Package fetch downloads one page and extracts its main content, as plain
// text and as markdown.
//
// Forked from Synthiz's apps/core/cerveau/infrastructure/fetch_url_tool.go.
package fetch

import (
	"bytes"
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
// populating cache around the network fetch. An empty page is never
// cached: it is a JS shell or a download that failed, and either deserves
// another try before the TTL runs out.
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
		page, err = extractPDF(ctx, body, final)
		if err != nil {
			return nil, err
		}
	} else {
		page = extract(body, final)
	}
	if cache != nil && page.Text != "" {
		cache.Set(rawURL, page)
	}
	return page, nil
}

// httpGet fetches rawURL direct, and again through the proxy when the host
// refused the direct egress: a status a bot policy answers with, or a
// connection the origin would not hold. The refusal is remembered, so the
// host's next fetch pays no direct try.
func httpGet(ctx context.Context, rawURL string) (body io.ReadCloser, final *url.URL, contentType string, err error) {
	host := ""
	if u, perr := url.Parse(rawURL); perr == nil {
		host = strings.ToLower(u.Host)
	}
	viaProxy := ProxyPreferred(host)
	resp, err := doGet(ctx, rawURL, viaProxy)
	if !viaProxy && proxy.get() != nil && refusedDirect(resp, err) {
		if resp != nil {
			resp.Body.Close()
		}
		PreferProxy(host)
		resp, err = doGet(ctx, rawURL, true)
	}
	if err != nil {
		return nil, nil, "", fmt.Errorf("fetch page: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, nil, "", fmt.Errorf("fetch page: status %d", resp.StatusCode)
	}
	return resp.Body, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}

func doGet(ctx context.Context, rawURL string, viaProxy bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/pdf;q=0.9,*/*;q=0.8")
	return httpClient(fetchTimeout, viaProxy).Do(req)
}

// maxHTMLBytes bounds a page read into memory: it is parsed twice, once
// for its links and once for readability, which prunes what it parses.
const maxHTMLBytes = 16 << 20

// extract isolates the readability call: the library panics on malformed
// DOMs and would otherwise take the whole caller down with it. Links are
// read off the whole document, which readability prunes.
//
// When readability keeps less than keepShare of the page's text, its
// article is not the page: a list page is one large table that scores as
// a sibling of the intro, and comes back without it. The whole body, less
// its boilerplate, stands in for the article then.
func extract(body io.Reader, page *url.URL) (out *Page) {
	out = &Page{URL: page.String()}
	defer func() {
		if recover() != nil {
			out = &Page{URL: page.String()}
		}
	}()

	raw, err := io.ReadAll(io.LimitReader(body, maxHTMLBytes))
	if err != nil {
		return out
	}
	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return out
	}
	out.Links = collectLinks(doc, page)
	out.Title = documentTitle(doc)

	whole := bodyOf(doc)
	pruneBoilerplate(whole)
	wholeRunes := textRunes(whole)

	forReadability, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return out
	}
	article, err := readability.FromDocument(forReadability, page)
	if err == nil && article.Node != nil && float64(textRunes(article.Node)) >= keepShare*float64(wholeRunes) {
		var buf strings.Builder
		if err := article.RenderText(&buf); err == nil {
			if title := strings.TrimSpace(article.Title()); title != "" {
				out.Title = title
			}
			out.Text = strings.TrimSpace(buf.String())
			out.Markdown = renderMarkdown(article.Node, page)
			return out
		}
	}
	if err == nil && article.Node != nil {
		if title := strings.TrimSpace(article.Title()); title != "" {
			out.Title = title
		}
	}
	out.Text = renderText(whole)
	out.Markdown = renderMarkdown(whole, page)
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
