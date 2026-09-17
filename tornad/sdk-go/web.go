package sdk

import (
	"context"
	"fmt"
	"strings"

	"github.com/lalternative/packages/tornad/sdk-go/internal/wire"
)

// Categories tornad answers for. The wire value of the academic one is
// "scientific publications"; tornad accepts "academic" as an alias, and this is
// the alias — a caller that hardcodes the long form against a deployment that
// renames it has no compiler to tell it.
//
// They are not an enum: which categories exist is decided by the providers a
// deployment configured, and one it does not have answers 400. A caller may
// pass any string.
const (
	CategoryGeneral  = "general"
	CategoryAcademic = "academic"
)

// SearchQuery is what to search for.
//
// A struct rather than parameters because tornad's own request grows: Content
// and Format arrived after the first callers were written, and each would have
// been a new signature.
type SearchQuery struct {
	Query string
	// Categories to search, empty meaning general alone. Several are fused by
	// reciprocal rank, which is the reason to ask tornad rather than a search
	// backend directly.
	Categories []string
	Limit      int
	Language   string
	TimeRange  string
	Page       int
	// DeadlineMS caps the wait on the slowest category. It can only ask for
	// LESS than the deployment's own deadline: a larger value is ignored, so
	// this is a ceiling, never an extension.
	DeadlineMS int
	// Content reads the first N results' pages into the answer, which is one
	// call instead of a search followed by N fetches. Clamped to 10 by tornad.
	Content int
	// ContentRunes bounds each page read by Content, defaulting to 4000.
	// tornad applies no ceiling of its own, so a large value here is a large
	// answer.
	ContentRunes int
	// Format picks which rendering of a page Content returns: FormatText,
	// FormatMarkdown, or empty for BOTH — which is twice the bytes, and the
	// wrong default for anything counting tokens.
	Format string
}

// Renderings a page can come back as.
const (
	FormatText     = "text"
	FormatMarkdown = "markdown"
)

// Result is one search result.
type Result struct {
	Title       string
	URL         string
	Description string
	Source      string
	Author      string
	PublishedAt string
	SourceType  string
	Duration    string
	Thumbnail   string
	Favicon     string
	Score       float32
	OpenGraph   *OpenGraph

	// Text and Markdown are filled only for the first Content results.
	Text     string
	Markdown string
	// ContentError says why this result's page could not be read. The search
	// itself succeeded: a page that fails reports here rather than failing
	// every other result with it.
	ContentError string
}

// OpenGraph is a result's Open Graph metadata, when the page carried any.
type OpenGraph struct {
	Title       string
	Description string
	Image       string
	SiteName    string
	Type        string
}

// SearchResults is a ranked answer.
type SearchResults struct {
	Query   string
	Results []Result
	// Partial is set when a category missed the deadline and was dropped. The
	// results are usable; they are simply not everything that was asked for.
	Partial bool
}

// Search returns ranked web results, optionally with the first pages' text.
func (c *Client) Search(ctx context.Context, q SearchQuery) (SearchResults, error) {
	if c.wire == nil {
		return SearchResults{}, ErrNotConfigured
	}
	body := wire.HttpapiSearchRequest{Q: ptr(q.Query)}
	if len(q.Categories) > 0 {
		body.Categories = ptr(q.Categories)
	}
	if q.Limit > 0 {
		body.Limit = ptr(q.Limit)
	}
	if q.Language != "" {
		body.Language = ptr(q.Language)
	}
	if q.TimeRange != "" {
		body.TimeRange = ptr(q.TimeRange)
	}
	if q.Page > 0 {
		body.Page = ptr(q.Page)
	}
	if q.DeadlineMS > 0 {
		body.DeadlineMs = ptr(q.DeadlineMS)
	}
	if q.Content > 0 {
		body.Content = ptr(q.Content)
	}
	if q.ContentRunes > 0 {
		body.ContentRunes = ptr(q.ContentRunes)
	}
	if q.Format != "" {
		body.Format = ptr(q.Format)
	}

	res, err := c.wire.SearchWithResponse(ctx, body)
	if err != nil {
		return SearchResults{}, wrap(err)
	}
	if res.JSON200 == nil {
		return SearchResults{}, statusFrom(res.StatusCode(), res.Body)
	}
	out := SearchResults{Query: deref(res.JSON200.Query), Partial: deref(res.JSON200.Partial)}
	for _, r := range deref(res.JSON200.Results) {
		out.Results = append(out.Results, resultFrom(r))
	}
	return out, nil
}

func resultFrom(r wire.HttpapiResult) Result {
	out := Result{
		Title:        deref(r.Title),
		URL:          deref(r.Url),
		Description:  deref(r.Description),
		Source:       deref(r.Source),
		Author:       deref(r.Author),
		PublishedAt:  deref(r.PublishedAt),
		SourceType:   deref(r.SourceType),
		Duration:     deref(r.Duration),
		Thumbnail:    deref(r.Thumbnail),
		Favicon:      deref(r.Favicon),
		Score:        deref(r.Score),
		Text:         deref(r.Text),
		Markdown:     deref(r.Markdown),
		ContentError: deref(r.ContentError),
	}
	if r.OpenGraph != nil {
		out.OpenGraph = &OpenGraph{
			Title:       deref(r.OpenGraph.Title),
			Description: deref(r.OpenGraph.Description),
			Image:       deref(r.OpenGraph.Image),
			SiteName:    deref(r.OpenGraph.SiteName),
			Type:        deref(r.OpenGraph.Type),
		}
	}
	return out
}

// FetchRequest asks for one page's main text.
type FetchRequest struct {
	URL string
	// MaxRunes bounds the text returned, defaulting to 6000. The cache holds
	// the whole page whatever this says, so a later call asking for more still
	// sees all of it.
	MaxRunes int
	// Render decides whether a page that yields nothing statically is retried
	// in a browser. Nil leaves tornad's default, which is to render.
	Render *bool
	// Paginate returns fixed-size pages instead of a truncation, so a long
	// article can be walked rather than lost.
	Paginate int
	// Format picks the rendering: FormatText, FormatMarkdown, or empty for
	// both.
	Format string
}

// Page is a page as tornad read it.
type Page struct {
	Title    string
	Text     string
	Markdown string
	// Pages is filled instead of Text when Paginate was asked for.
	Pages []string
}

// Fetch reads a page's main text, rendering it when a static read comes back
// near-empty.
//
// A page tornad could not read answers ErrUpstream, which is routine on the
// open web: a caller with a fallback should take it rather than retry.
func (c *Client) Fetch(ctx context.Context, req FetchRequest) (Page, error) {
	if c.wire == nil {
		return Page{}, ErrNotConfigured
	}
	body := wire.HttpapiFetchRequest{Url: ptr(req.URL)}
	if req.MaxRunes > 0 {
		body.MaxRunes = ptr(req.MaxRunes)
	}
	if req.Render != nil {
		body.Render = req.Render
	}
	if req.Paginate > 0 {
		body.Paginate = ptr(req.Paginate)
	}
	if req.Format != "" {
		body.Format = ptr(req.Format)
	}

	res, err := c.wire.FetchPageWithResponse(ctx, body)
	if err != nil {
		return Page{}, wrap(err)
	}
	if res.JSON200 == nil {
		return Page{}, statusFrom(res.StatusCode(), res.Body)
	}
	return Page{
		Title:    deref(res.JSON200.Title),
		Text:     deref(res.JSON200.Text),
		Markdown: deref(res.JSON200.Markdown),
		Pages:    deref(res.JSON200.Pages),
	}, nil
}

// RenderRequest asks for a page's HTML after its JavaScript has run.
type RenderRequest struct {
	URL string
	// TimeoutMS defaults to 8s and is clamped to 20s whatever is asked: an
	// unbounded render starves everything queued behind it on a shared browser.
	TimeoutMS int
}

// Rendered is a page's HTML and the URL actually loaded, which a redirect can
// make differ from the one asked for.
type Rendered struct {
	HTML     string
	FinalURL string
}

// Render returns a page's HTML after its JavaScript has run.
//
// It does not go through tornad's residential proxy — it is a renderer, not a
// way around a block. Fetch is what carries both a residential address and a
// real browser's fingerprint.
func (c *Client) Render(ctx context.Context, req RenderRequest) (Rendered, error) {
	if c.wire == nil {
		return Rendered{}, ErrNotConfigured
	}
	body := wire.HttpapiRenderRequest{Url: ptr(req.URL)}
	if req.TimeoutMS > 0 {
		body.TimeoutMs = ptr(req.TimeoutMS)
	}
	res, err := c.wire.RenderPageWithResponse(ctx, body)
	if err != nil {
		return Rendered{}, wrap(err)
	}
	if res.JSON200 == nil {
		return Rendered{}, statusFrom(res.StatusCode(), res.Body)
	}
	return Rendered{HTML: deref(res.JSON200.Html), FinalURL: deref(res.JSON200.FinalUrl)}, nil
}

// Scope bounds a map or a crawl.
type Scope struct {
	URL          string
	MaxDepth     int
	MaxPages     int
	IncludePaths []string
	ExcludePaths []string
	// Seed "sitemap" starts from every URL the site declares.
	Seed string
}

// MapRequest asks for a site's URLs.
type MapRequest struct {
	Scope
	// Limit defaults to 100 and is clamped to 1000 by tornad.
	Limit int
	// DeadlineMS is clamped to 50s.
	DeadlineMS int
	// Sitemap reads the site's sitemaps before walking its links. Nil leaves
	// tornad's default, which is on — the cheapest and most complete source.
	Sitemap *bool
}

// SiteMap is every URL a site declares or links to, within a scope.
type SiteMap struct {
	URL   string
	Links []string
}

// Map returns a site's URLs, within a scope. Synchronous and bounded.
func (c *Client) Map(ctx context.Context, req MapRequest) (SiteMap, error) {
	if c.wire == nil {
		return SiteMap{}, ErrNotConfigured
	}
	body := wire.HttpapiMapRequest{Url: ptr(req.URL)}
	applyScope(&body, req.Scope)
	if req.Limit > 0 {
		body.Limit = ptr(req.Limit)
	}
	if req.DeadlineMS > 0 {
		body.DeadlineMs = ptr(req.DeadlineMS)
	}
	if req.Sitemap != nil {
		body.Sitemap = req.Sitemap
	}
	res, err := c.wire.MapSiteWithResponse(ctx, body)
	if err != nil {
		return SiteMap{}, wrap(err)
	}
	if res.JSON200 == nil {
		return SiteMap{}, statusFrom(res.StatusCode(), res.Body)
	}
	return SiteMap{URL: deref(res.JSON200.Url), Links: deref(res.JSON200.Links)}, nil
}

func applyScope(body *wire.HttpapiMapRequest, s Scope) {
	if s.MaxDepth > 0 {
		body.MaxDepth = ptr(s.MaxDepth)
	}
	if s.MaxPages > 0 {
		body.MaxPages = ptr(s.MaxPages)
	}
	if len(s.IncludePaths) > 0 {
		body.IncludePaths = ptr(s.IncludePaths)
	}
	if len(s.ExcludePaths) > 0 {
		body.ExcludePaths = ptr(s.ExcludePaths)
	}
	if s.Seed != "" {
		body.Seed = ptr(s.Seed)
	}
}

// wrap turns a transport failure into ErrUnavailable, preserving what failed.
func wrap(err error) error {
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// statusFrom maps a response with no decoded body onto a sentinel.
func statusFrom(code int, body []byte) error {
	if err := statusError(code, bytesReader(body)); err != nil {
		return err
	}
	return fmt.Errorf("%w: unexpected status %d", ErrUnavailable, code)
}

func bytesReader(b []byte) *strings.Reader { return strings.NewReader(string(b)) }
