package sdk

import (
	"context"

	"github.com/lalternative/packages/vvaves/sdk-go/internal/wire"
)

// CrawlJob is a crawl as vvaves reports it.
type CrawlJob struct {
	ID     string
	Status string
	Pages  int
	Failed int
	Error  string
}

// StartCrawl starts a crawl over a scope and returns its job, without waiting
// for it: a hundred pages is minutes of work.
//
// It answers ErrUnavailable where the deployment has no crawl store — crawling
// needs NATS, which search and fetch do not, so a deployment can have one and
// not the other.
func (c *Client) StartCrawl(ctx context.Context, s Scope) (CrawlJob, error) {
	if c.wire == nil {
		return CrawlJob{}, ErrNotConfigured
	}
	body := wire.HttpapiCrawlRequest{Url: ptr(s.URL)}
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
	res, err := c.wire.StartCrawlWithResponse(ctx, body)
	if err != nil {
		return CrawlJob{}, wrap(err)
	}
	if res.JSON202 == nil {
		return CrawlJob{}, statusFrom(res.StatusCode(), res.Body)
	}
	return CrawlJob{
		ID:     deref(res.JSON202.Id),
		Status: deref(res.JSON202.Status),
		Pages:  deref(res.JSON202.Pages),
		Failed: deref(res.JSON202.Failed),
		Error:  deref(res.JSON202.Error),
	}, nil
}

// CrawledPage is one page a crawl read.
type CrawledPage struct {
	URL      string
	Depth    int
	Title    string
	Text     string
	Markdown string
	// Error says why this page could not be read. The crawl itself continues:
	// a page that fails reports here rather than ending the job.
	Error string
}

// CrawlProgress is a crawl's state and the pages read so far.
type CrawlProgress struct {
	CrawlJob
	Results []CrawledPage
	// Next is the offset of the following page of results, when there is one.
	Next *int
}

// CrawlStatusRequest reads back a crawl, a page of results at a time.
type CrawlStatusRequest struct {
	ID     string
	Offset int
	// Limit defaults to 20.
	Limit int
}

// CrawlStatus returns a crawl's progress and the pages it has read.
func (c *Client) CrawlStatus(ctx context.Context, req CrawlStatusRequest) (CrawlProgress, error) {
	if c.wire == nil {
		return CrawlProgress{}, ErrNotConfigured
	}
	params := &wire.CrawlStatusParams{}
	if req.Offset > 0 {
		params.Offset = ptr(req.Offset)
	}
	if req.Limit > 0 {
		params.Limit = ptr(req.Limit)
	}
	res, err := c.wire.CrawlStatusWithResponse(ctx, req.ID, params)
	if err != nil {
		return CrawlProgress{}, wrap(err)
	}
	if res.JSON200 == nil {
		return CrawlProgress{}, statusFrom(res.StatusCode(), res.Body)
	}
	out := CrawlProgress{
		CrawlJob: CrawlJob{
			ID:     deref(res.JSON200.Id),
			Status: deref(res.JSON200.Status),
			Pages:  deref(res.JSON200.Pages),
			Failed: deref(res.JSON200.Failed),
			Error:  deref(res.JSON200.Error),
		},
		Next: res.JSON200.Next,
	}
	for _, p := range deref(res.JSON200.Results) {
		out.Results = append(out.Results, CrawledPage{
			URL:      deref(p.Url),
			Depth:    deref(p.Depth),
			Title:    deref(p.Title),
			Text:     deref(p.Text),
			Markdown: deref(p.Markdown),
			Error:    deref(p.Error),
		})
	}
	return out, nil
}
