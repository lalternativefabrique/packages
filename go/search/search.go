// Package search runs queries against a metasearch backend and returns
// unified results.
//
// Forked from Synthiz's apps/core/search, where SearXNG is the only backend.
// Here the backend is a Provider interface so a commercial SERP API can
// stand in or back it up without callers branching on which one answered.
package search

import "context"

// Category selects which class of engines a Provider queries.
type Category string

const (
	CategoryGeneral  Category = "general"
	CategoryAcademic Category = "scientific publications"
)

// Provider runs one query against one search backend.
type Provider interface {
	Search(ctx context.Context, q Query) (*Response, error)
}

// Query describes one search request.
type Query struct {
	Text      string
	Category  Category
	Limit     int
	TimeRange string
	Page      int
	Language  string
}

// OpenGraph holds the subset of a page's OpenGraph metadata worth surfacing
// alongside a search result.
type OpenGraph struct {
	Title       string
	Description string
	Image       string
	SiteName    string
	Type        string
}

// Result is one search hit, in the shape callers render or hand to an LLM.
type Result struct {
	Title       string
	URL         string
	Description string
	Thumbnail   string
	Source      string
	Author      string
	Duration    string
	PublishedAt string
	SourceType  string
	Score       float64

	OpenGraph *OpenGraph
	Favicon   string
}

// Response is the result of one query.
type Response struct {
	Query   string
	Results []Result
	// Partial reports that part of a merged search failed or missed its
	// deadline, so the caller can tell "nothing matched" from "some backends
	// never answered".
	Partial bool
}

// DetectSourceType classifies a result URL into "youtube", "podcast" or
// "web". category is the Provider category the result came from, since a
// videos-category hit is YouTube-shaped even when its host does not say so.
func DetectSourceType(rawURL string, category Category) string {
	return detectSourceType(rawURL, category)
}
