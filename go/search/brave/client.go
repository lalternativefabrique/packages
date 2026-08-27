// Package brave adapts the Brave Search API to search.Provider.
//
// Brave is the recommended commercial fallback because it runs its own
// independent index rather than reselling Google or Bing results, so it
// covers the actual gap a self-hosted SearXNG instance has: an upstream
// engine getting rate-limited or blocked, not a scrape of the same source.
package brave

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/lalternative/packages/go/search"
)

const defaultBaseURL = "https://api.search.brave.com/res/v1/web/search"

type webResponse struct {
	Web struct {
		Results []webResult `json:"results"`
	} `json:"web"`
}

type webResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Age         string `json:"age"`
	Profile     struct {
		Name string `json:"name"`
		Img  string `json:"img"`
	} `json:"profile"`
}

// Provider queries the Brave Search API.
type Provider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// New builds a Provider authenticated with apiKey. Pass nil for httpClient
// to use a client with a 10s timeout.
func New(apiKey string, httpClient *http.Client) *Provider {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{apiKey: apiKey, baseURL: defaultBaseURL, httpClient: httpClient}
}

var _ search.Provider = (*Provider)(nil)

// Search implements search.Provider. Brave has no separate academic
// category: a search.CategoryAcademic query still runs, just against
// Brave's general web index.
func (p *Provider) Search(ctx context.Context, q search.Query) (*search.Response, error) {
	limit := q.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	params := url.Values{
		"q":     {q.Text},
		"count": {strconv.Itoa(limit)},
	}
	if q.Language != "" {
		params.Set("search_lang", q.Language)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("brave returned %d: %s", resp.StatusCode, string(body))
	}

	var raw webResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	results := make([]search.Result, 0, len(raw.Web.Results))
	for _, r := range raw.Web.Results {
		results = append(results, search.Result{
			Title:       r.Title,
			URL:         r.URL,
			Description: r.Description,
			Thumbnail:   r.Profile.Img,
			Source:      "brave",
			PublishedAt: r.Age,
			SourceType:  search.DetectSourceType(r.URL, q.Category),
		})
	}

	return &search.Response{Query: q.Text, Results: results}, nil
}
