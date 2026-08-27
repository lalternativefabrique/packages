package search

import (
	"context"
	"sort"
	"time"
)

// rrfK dampens the weight of top ranks in Merge's fusion. 60 is the value
// from the original reciprocal-rank-fusion paper and the usual default.
const rrfK = 60.0

// Merge runs query q against every provider concurrently and fuses their
// ranked result lists with reciprocal rank fusion, deduplicated by URL.
//
// Raw scores are not comparable across providers or categories — SearXNG's
// web category peaks around 4.0 where its academic engines cap at 1.0, and a
// commercial API scores on its own scale entirely. Concatenating and sorting
// by score buries whichever provider scores lowest. Ranks are scale-free, so
// the head of every list survives, and a URL returned by more than one
// provider is promoted.
//
// deadline bounds how long Merge waits for every provider. A provider still
// running past it is dropped from this response and Response.Partial is set,
// so one slow backend does not hold up the others.
func Merge(ctx context.Context, providers []Provider, q Query, deadline time.Duration) (*Response, error) {
	type outcome struct {
		res *Response
		err error
	}
	done := make(chan outcome, len(providers))
	for _, p := range providers {
		go func(p Provider) {
			res, err := p.Search(ctx, q)
			done <- outcome{res: res, err: err}
		}(p)
	}

	timer := time.NewTimer(deadline)
	defer timer.Stop()

	lists := make([][]Result, 0, len(providers))
	var firstErr error
	for range providers {
		select {
		case out := <-done:
			if out.err != nil {
				firstErr = out.err
				continue
			}
			lists = append(lists, out.res.Results)
		case <-timer.C:
			if len(lists) == 0 {
				// Nothing to show yet: a slow answer beats an empty one.
				out := <-done
				if out.err != nil {
					return nil, out.err
				}
				lists = append(lists, out.res.Results)
			}
			return &Response{
				Query:   q.Text,
				Results: fuseByRank(lists, q.Limit),
				Partial: true,
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(lists) == 0 {
		return nil, firstErr
	}

	return &Response{
		Query:   q.Text,
		Results: fuseByRank(lists, q.Limit),
		Partial: len(lists) < len(providers),
	}, nil
}

func fuseByRank(lists [][]Result, limit int) []Result {
	scores := make(map[string]float64)
	best := make(map[string]Result)
	order := make([]string, 0)

	for _, list := range lists {
		for rank, r := range list {
			if _, known := best[r.URL]; !known {
				best[r.URL] = r
				order = append(order, r.URL)
			}
			scores[r.URL] += 1.0 / (rrfK + float64(rank+1))
		}
	}

	sort.SliceStable(order, func(i, j int) bool {
		return scores[order[i]] > scores[order[j]]
	})

	if limit > 0 && len(order) > limit {
		order = order[:limit]
	}
	fused := make([]Result, 0, len(order))
	for _, u := range order {
		r := best[u]
		r.Score = scores[u]
		fused = append(fused, r)
	}
	return fused
}
