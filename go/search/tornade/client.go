// Package tornade queries a tornade instance's /search endpoint.
//
// Tornade is the HTTP facade over SearXNG and Brave: it owns the category
// fusion, the per-category deadline and the fallback to Brave when a
// self-hosted SearXNG's engines are rate-limited or blocked. A caller that
// reaches SearXNG directly reimplements the first two and loses the third.
//
// Unlike searxng.Provider, which answers for one category at a time and is
// fused client-side by search.Merge, this client hands tornade the whole
// category list and lets it do the fusion it already implements.
package tornade

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/go/search"
)

// Client queries a tornade instance.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New builds a Client against a tornade at baseURL, or nil when baseURL is
// empty — the same absent-rather-than-half-present contract the other
// clients in this module follow.
//
// Pass nil for httpClient to use one with a 30s timeout: tornade's own
// SEARCH_DEADLINE_MS bounds the search itself, and this only has to outlast
// it rather than cut it short.
func New(baseURL string, httpClient *http.Client) *Client {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

type request struct {
	Q          string   `json:"q"`
	Categories []string `json:"categories,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Language   string   `json:"language,omitempty"`
	TimeRange  string   `json:"time_range,omitempty"`
	Page       int      `json:"page,omitempty"`
	DeadlineMS int64    `json:"deadline_ms,omitempty"`
}

type response struct {
	Query   string   `json:"query"`
	Results []result `json:"results"`
	Partial bool     `json:"partial"`
}

type result struct {
	Title       string     `json:"title"`
	URL         string     `json:"url"`
	Description string     `json:"description"`
	Thumbnail   string     `json:"thumbnail"`
	Source      string     `json:"source"`
	Author      string     `json:"author"`
	Duration    string     `json:"duration"`
	PublishedAt string     `json:"published_at"`
	SourceType  string     `json:"source_type"`
	Score       float64    `json:"score"`
	Favicon     string     `json:"favicon"`
	OpenGraph   *openGraph `json:"open_graph"`
}

type openGraph struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
	SiteName    string `json:"site_name"`
	Type        string `json:"type"`
}

// Search runs q against the given categories, which tornade fuses by
// reciprocal rank when there is more than one. An empty categories list
// leaves the choice to tornade, which answers on its general category.
//
// deadline bounds the wait on the slowest category; tornade clamps it to its
// own SEARCH_DEADLINE_MS and drops whichever category misses it, setting
// Partial rather than holding the whole response back.
func (c *Client) Search(ctx context.Context, q search.Query, categories []search.Category, deadline time.Duration) (*search.Response, error) {
	if strings.TrimSpace(q.Text) == "" {
		return nil, fmt.Errorf("tornade: query is empty")
	}

	names := make([]string, 0, len(categories))
	for _, cat := range categories {
		names = append(names, categoryName(cat))
	}

	body, err := json.Marshal(request{
		Q:          q.Text,
		Categories: names,
		Limit:      q.Limit,
		Language:   q.Language,
		TimeRange:  q.TimeRange,
		Page:       q.Page,
		DeadlineMS: deadline.Milliseconds(),
	})
	if err != nil {
		return nil, fmt.Errorf("tornade: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tornade: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tornade: call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("tornade: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}

	var out response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("tornade: decode response: %w", err)
	}

	results := make([]search.Result, 0, len(out.Results))
	for _, r := range out.Results {
		converted := search.Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
			Thumbnail:   r.Thumbnail,
			Source:      r.Source,
			Author:      r.Author,
			Duration:    r.Duration,
			PublishedAt: r.PublishedAt,
			SourceType:  r.SourceType,
			Score:       r.Score,
			Favicon:     r.Favicon,
		}
		if r.OpenGraph != nil {
			converted.OpenGraph = &search.OpenGraph{
				Title:       r.OpenGraph.Title,
				Description: r.OpenGraph.Description,
				Image:       r.OpenGraph.Image,
				SiteName:    r.OpenGraph.SiteName,
				Type:        r.OpenGraph.Type,
			}
		}
		results = append(results, converted)
	}

	return &search.Response{Query: out.Query, Results: results, Partial: out.Partial}, nil
}

// categoryName maps a Category onto the name tornade's API accepts.
// CategoryAcademic's own value is SearXNG's engine name ("scientific
// publications"), which tornade takes as the short "academic" alias instead.
func categoryName(c search.Category) string {
	if c == search.CategoryAcademic {
		return "academic"
	}
	return string(c)
}
