# tornad/sdk-go

The Go client for [tornad](https://github.com/lalternativefabrique/tornad) —
the HTTP facade over the platform's search, page extraction and JavaScript
rendering backends.

```go
c := sdk.New(os.Getenv("TORNAD_URL"), os.Getenv("TORNAD_APP_KEY"))

res, err := c.Search(ctx, sdk.SearchQuery{
    Query:      "gramsci",
    Categories: []string{sdk.CategoryGeneral, sdk.CategoryAcademic},
    Content:    3,                  // the first 3 results' pages, in one call
    Format:     sdk.FormatMarkdown, // not both — empty returns text AND markdown
})

page, err := c.Fetch(ctx, sdk.FetchRequest{URL: "https://…", MaxRunes: 6000})
if errors.Is(err, sdk.ErrUpstream) {
    // routine on the open web: the page was refused or never settled.
    // Fall back; do not retry.
}
```

## Why it exists

Every consumer was writing this by hand. `packages/go/search` carried a
`tornade` client covering `/search` alone; lalter's cortex built its own
`/fetch` request beside it. Each re-derived the same auth header, the same
status handling and the same request shape from reading tornad's source — and
neither ever learned that `/map` and `/crawl` exist.

## Where the transport comes from

`internal/wire` is generated from `openapi/tornad.json`, which is tornad's own
contract as served on `GET /openapi.json`. It owns every path, method and
parameter, so none of them is typed by hand: a route renamed upstream is a
compile error here rather than a 404 in production.

It is internal because it is not this package's API. Its methods return raw
`*http.Response` and generated pointer types; what is exported wraps them with
typed errors and the conversions that keep "this page could not be read"
distinguishable from "tornad is down".

Refreshing after an API change:

```sh
./refresh-contract.sh                 # or: ./refresh-contract.sh http://localhost:8080
```

It fetches the contract from a running tornad and regenerates, then reconcile
any compile error the new shape causes.

## What is covered

`Search`, `Fetch`, `Render`, `Map`, `StartCrawl`, `CrawlStatus`.

The admin API (`/api/v1/admin/*`) is excluded on purpose: it is authenticated
by an operator's JWT, not an application key, so an application has nothing to
call there. The exclusion is a deny list rather than an allow list — an allow
list is silent when it is wrong, which is how the client this replaces stayed
unaware of two routes. `TestEveryCallerFacingRouteIsGenerated` asserts it
against the contract.

Speech is not here at all: reading text aloud is
[vvaves](https://github.com/lalternativefabrique/vvaves), a separate service
with its own client, because a reading is authorised by a signed URL rather
than by an application key.

## Errors

| | |
|---|---|
| `ErrNotConfigured` | no base URL; the call was never made |
| `ErrUnauthorized` | the key was rejected — operator error |
| `ErrBadRequest` | tornad refused the arguments — a bug in the caller |
| `ErrNotFound` | no such crawl |
| `ErrUpstream` | tornad reached the web and got no page (502). **Routine**, not an outage |
| `ErrUnavailable` | transport failure, 5xx, or a backend this deployment lacks |

`ErrUpstream` is separate on purpose: a publisher refusing a datacenter address
is the common case on the open web, and folded into `ErrUnavailable` it reads
as "tornad is broken" — so a caller retries a URL that will never load instead
of falling back.
