# search

Queries a metasearch backend, fuses results across providers, and extracts
the main text of a page.

No provider is hardcoded: `Provider` is a plain interface, so SearXNG, a
commercial SERP API, or a fixture in a test all look the same to a caller.

```go
import "github.com/lalternative/packages/go/search"
```

Forked from Synthiz's `apps/core/search`
(`github.com/digstack/synthiz/apps/core/search`) and
`apps/core/cerveau/infrastructure/fetch_url_tool.go`. The two copies are not
kept in sync automatically — if this logic changes here, or drifts from the
source, that is expected, not a bug.

## Providers

```go
sx := searxng.New("http://searxng.ai.svc.cluster.local:8080", nil)
res, err := sx.Search(ctx, search.Query{Text: "gramsci", Category: search.CategoryGeneral, Limit: 10})
```

`searxng.Provider` speaks SearXNG's JSON API exactly as Synthiz's original
client did. `brave.Provider` speaks the Brave Search API — recommended as a
commercial fallback because it runs its own independent index rather than
reselling Google or Bing, so it covers what a self-hosted SearXNG actually
needs covered: an upstream engine getting rate-limited or blocked, not a
scrape of the same source.

```go
provider := search.WithFallback(sx, brave.New(apiKey, nil))
```

`WithFallback` only calls the secondary provider when the primary errors or
returns nothing — no retries, no health checks, no circuit breaker: a caller
that needs those composes them around this.

## Merging categories or providers

SearXNG's own score is not comparable across categories — its web category
peaks around 4.0 where its academic engines cap at 1.0 — and a commercial
provider scores on its own scale entirely. Sorting a concatenation by raw
score buries whichever list scores lowest, measured on a `synthiz` query
where the merged top 15 held no web result at all.

`Merge` fuses ranked lists by reciprocal rank fusion instead, which is
scale-free: the head of every list survives, and a URL returned by more than
one provider is promoted.

```go
res, err := search.Merge(ctx, []search.Provider{webSearxng, academicSearxng}, search.Query{Text: "gramsci", Limit: 10}, 4*time.Second)
```

The `deadline` argument bounds how long `Merge` waits for the slowest
provider. Measured against SearXNG, the web category answers in ~1s while the
academic one takes 6-8s — waiting for both makes every search feel as slow as
the worst one, for what is often a box a user types into. A provider that
misses the deadline is dropped from that response and `Response.Partial` is
set, rather than holding the whole response hostage to it.

## Reading a page

```go
page, err := fetch.FetchStatic(ctx, url, 6000, nil)
```

`FetchStatic` downloads a page and extracts its main text with
[go-readability](https://codeberg.org/readeck/go-readability). It does not
execute JavaScript — a page whose content is rendered client-side yields
empty text, not an error, since half the web is like this and the caller
should decide what to do about it rather than have this package guess.

```go
page, err := fetch.FetchWithFallback(ctx, url, renderer, 6000, nil)
```

`FetchWithFallback` retries through a `Renderer` when the static fetch yields
fewer than 200 runes of text — a JS-only page's initial HTML often still
carries shell boilerplate (nav, footer) that readability extracts a few dozen
runes from, which is not an article. Pass `nil` for `renderer` to skip this
entirely; `FetchWithFallback` then behaves exactly like `FetchStatic`.

`rendersvc.Client` implements `Renderer` against a small standalone
JavaScript-rendering service (a single `POST /render` endpoint backed by a
headless browser). That service is not part of this module: it owns a
long-lived browser process — its own memory footprint, its own crash/restart
lifecycle, a Chromium binary in its image — which is a deployment concern
distinct from a stateless library, not something every consumer of this
package should have to bundle. Deploy it separately and point `rendersvc`'s
`baseURL` at it; a caller with no such service simply never wires a
`Renderer` in.

```go
pages := page.Paginate(4000)
```

`Paginate` splits `Page.Text` into fixed-size pages instead of a hard
truncation, so a caller (a follow-up tool call, a "load more" in a web app)
can walk the rest of a long article instead of losing it.

## Caching

```go
cache := fetch.NewMemoryCache(15 * time.Minute)
page, err := fetch.FetchStatic(ctx, url, 6000, cache)
```

Both `FetchStatic` and `FetchWithFallback` take a `Cache`, keyed by URL. It
holds each page's full, untruncated text — not the `maxRunes`-truncated
result — so a second call with a different `maxRunes`, or a `Paginate` call
that follows an earlier fetch, still sees the whole page rather than whatever
the first call happened to keep. `FetchWithFallback` caches a rendered page
separately from its static attempt, since they are two different extractions
of the same URL.

Pass `nil` to skip caching entirely. `NewMemoryCache` is a plain in-process
map with a TTL and no background eviction — sized for a single process's
lifetime, not a shared store across replicas. A caller that needs sharing
implements `Cache` against Redis or anything else; the interface is two
methods.

## Panics

`go-readability` panics on some malformed DOMs. `FetchStatic` and
`FetchWithFallback` recover internally and return an empty `Page` rather than
propagating the panic — a caller reading arbitrary URLs from the web cannot
control what comes back, and one malformed page should not take down the
request that fetched it.
