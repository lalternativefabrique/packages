package search

import "context"

// WithFallback returns a Provider that queries primary, and falls back to
// secondary only when primary errors or returns no results — e.g. SearXNG's
// upstream engines are rate-limited or blocked, and a commercial SERP API
// covers the gap.
func WithFallback(primary, secondary Provider) Provider {
	return &fallbackProvider{primary: primary, secondary: secondary}
}

type fallbackProvider struct {
	primary   Provider
	secondary Provider
}

func (f *fallbackProvider) Search(ctx context.Context, q Query) (*Response, error) {
	res, err := f.primary.Search(ctx, q)
	if err == nil && res != nil && len(res.Results) > 0 {
		return res, nil
	}
	return f.secondary.Search(ctx, q)
}
