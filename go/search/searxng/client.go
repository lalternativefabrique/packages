// Package searxng adapts a SearXNG metasearch instance to search.Provider.
package searxng

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/lalternative/packages/go/search"
)

type response struct {
	Results []result `json:"results"`
}

type result struct {
	Title         string          `json:"title"`
	URL           string          `json:"url"`
	Content       string          `json:"content"`
	Engine        string          `json:"engine"`
	Thumbnail     string          `json:"thumbnail"`
	ImgSrc        string          `json:"img_src"`
	PublishedDate string          `json:"publishedDate"`
	Length        json.RawMessage `json:"length"`
	Author        string          `json:"author"`
	Score         float64         `json:"score"`
}

// Provider queries a SearXNG instance's JSON API.
type Provider struct {
	baseURL    string
	httpClient *http.Client
}

// New builds a Provider against a SearXNG instance at baseURL. Pass nil for
// httpClient to use a client with a 20s timeout — SearXNG's "general"
// category fans out to bing, google cse and duckduckgo web and lands around
// 10s.
func New(baseURL string, httpClient *http.Client) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return &Provider{baseURL: baseURL, httpClient: httpClient}
}

var _ search.Provider = (*Provider)(nil)

// Search implements search.Provider.
func (p *Provider) Search(ctx context.Context, q search.Query) (*search.Response, error) {
	limit := q.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}

	params := url.Values{
		"q":      {q.Text},
		"format": {"json"},
		"pageno": {fmt.Sprintf("%d", page)},
	}
	if q.Language != "" {
		params.Set("language", q.Language)
	}
	if q.Category != "" {
		params.Set("categories", string(q.Category))
	}
	if q.TimeRange != "" {
		params.Set("time_range", q.TimeRange)
	}

	reqURL := fmt.Sprintf("%s/search?%s", p.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("searxng request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("searxng returned %d: %s", resp.StatusCode, string(body))
	}

	var raw response
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]search.Result, 0, len(raw.Results))
	for _, r := range raw.Results {
		results = append(results, toResult(r, q.Category))
	}
	sortByScore(results)
	if len(results) > limit {
		results = results[:limit]
	}

	return &search.Response{Query: q.Text, Results: results}, nil
}

// sortByScore orders results by SearxNG's own aggregation score, highest
// first.
//
// SearxNG computes the score (a result found by several engines, each
// ranking it high, scores higher) but returns the list grouped by engine
// rather than ordered by it, so truncating to limit without sorting first
// can drop the best sources. The sort is stable so equal scores keep
// SearxNG's relative order.
func sortByScore(results []search.Result) {
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
}

func toResult(r result, category search.Category) search.Result {
	thumbnail := r.Thumbnail
	if thumbnail == "" {
		thumbnail = r.ImgSrc
	}
	return search.Result{
		Title:       r.Title,
		URL:         r.URL,
		Description: r.Content,
		Thumbnail:   thumbnail,
		Source:      r.Engine,
		Author:      r.Author,
		Duration:    parseLength(r.Length),
		PublishedAt: r.PublishedDate,
		SourceType:  search.DetectSourceType(r.URL, category),
		Score:       r.Score,
	}
}

// parseLength handles SearxNG's length field which can be a string
// ("10:45") or a number (645 seconds).
func parseLength(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var n float64
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		secs := int(math.Round(n))
		if secs >= 3600 {
			return fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
		}
		return fmt.Sprintf("%d:%02d", secs/60, secs%60)
	}
	return ""
}
