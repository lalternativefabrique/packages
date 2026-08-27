// Package fetch downloads one page and extracts its main text.
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

	"github.com/lalternative/packages/go/search"
)

const (
	// fetchUserAgent identifies requests as an automated reader rather than a
	// browser, since some sites otherwise serve different (often lighter,
	// JS-only) markup.
	fetchUserAgent = "Mozilla/5.0 (compatible; SearchLib/1.0; +https://github.com/lalternative/packages)"
)

// Page is the extracted content of one fetched URL.
type Page struct {
	Title string
	Text  string

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
	return &Page{
		Title:     full.Title,
		Text:      truncateRunes(full.Text, maxRunes),
		OpenGraph: full.OpenGraph,
		Favicon:   full.Favicon,
	}, nil
}

// fetchFull returns the page's full, untruncated content, consulting and
// populating cache around the network fetch.
func fetchFull(ctx context.Context, rawURL string, parsed *url.URL, cache Cache) (*Page, error) {
	if cache != nil {
		if page, ok := cache.Get(rawURL); ok {
			return page, nil
		}
	}

	body, err := httpGet(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	title, text := extract(body, parsed)
	page := &Page{Title: title, Text: text}
	if cache != nil {
		cache.Set(rawURL, page)
	}
	return page, nil
}

func httpGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", fetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch page: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// extract isolates the readability call: the library panics on malformed
// DOMs and would otherwise take the whole caller down with it.
func extract(body io.Reader, parsed *url.URL) (title, text string) {
	defer func() {
		if recover() != nil {
			title, text = "", ""
		}
	}()

	article, err := readability.FromReader(body, parsed)
	if err != nil {
		return "", ""
	}
	var buf strings.Builder
	if err := article.RenderText(&buf); err != nil {
		return "", ""
	}
	return strings.TrimSpace(article.Title()), strings.TrimSpace(buf.String())
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
