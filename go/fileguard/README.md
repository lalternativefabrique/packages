# fileguard

The checks a service owes on bytes and URLs it did not choose: who may submit
what, whether a URL is safe to fetch, and whether the content is what it claims.
Composable stages, no fixed chain — a service that only stores files runs the
first two, one that decodes media runs them all.

Module path: `github.com/lalternative/packages/go/fileguard`. Standard library
only.

```go
// One admission point. The policy is declared once, at wiring time.
guard := fileguard.NewGuard(fileguard.Policy{
    Allowed:          []fileguard.SourceType{fileguard.SourceYouTube, fileguard.SourcePodcast},
    AllowedAnonymous: []fileguard.SourceType{fileguard.SourceYouTube},
})

admitted, err := guard.Admit(fileguard.Request{URL: raw, Type: declared, Anonymous: isAnon})
// errors.Is(err, fileguard.ErrTypeNotAllowed | ErrAnonNotAllowed | ErrUnsafeURL)
```

`Admit` returns an `AdmittedSource`, an opaque value only it can mint. Take that
type in the pipeline rather than a `string` and a path that skipped admission
stops compiling — the check is no longer something a handler can forget.

Checks run cheapest first: the declared type, then who is asking, then the URL,
since only the last one resolves DNS.

## SSRF

```go
if err := fileguard.ValidateFetchURL(raw); err != nil { /* refuse */ }
body, err := fileguard.SafeHTTPClient(30 * time.Second).Get(raw)
```

Two halves, deliberately separate.

`ValidateFetchURL` is a pre-check on the submitted URL. On its own it does not
close DNS rebinding: the answer can change between its lookup and the
connection.

`SafeHTTPClient` resolves each host itself, refuses every address it must not
reach, and then dials **the address it just validated** rather than re-resolving
the name. That is what closes rebinding — not a second inspection, but the
absence of a second lookup. `SafeTransport` exposes the same dialing guarantee
for a caller that needs its own client settings.

**Know when only the first half applies.** Behind an HTTP proxy the transport
connects to the proxy, not to the target, so its guard inspects the wrong
endpoint. A headless browser resolves names itself and never sees this package.
In both cases `ValidateFetchURL` bounds what a caller may *ask for*, not what the
fetcher *reaches*, and the rest has to come from the network layer. Reaching for
`SafeHTTPClient` there buys a false sense of safety.

Never return these errors verbatim to an unauthenticated caller: a message
naming the address a host resolved to turns the endpoint into a scanner of the
internal network.

## Content

```go
head, body, err := fileguard.ReadHeader(r)   // sniffable prefix, and a reader that replays it
container, err := fileguard.Sniff(head)      // rejects rather than guessing
_, err = fileguard.SniffMatching(head, declaredContentType)
```

`Sniff` returns an error for bytes it cannot identify. A sniffer with a fallback
is not a gate: nothing can fail it. `SniffMatching` compares container families,
not strings — one ftyp box is spelled five ways by real clients, and demanding
equality would refuse correct uploads while catching nothing.

`Container.IsMedia` separates audio and video from documents, which `Sniff` also
recognises: a media path has to ask rather than assume the absence of an error
means what it wants.

## Size

```go
r := fileguard.LimitedReader(body, maxBytes)  // fails past the ceiling
```

`io.LimitReader` reports `io.EOF` at the ceiling, so an oversized body is stored
truncated and believed whole. This returns `ErrTooLarge` instead — the
difference between a limit and a trim. The ceiling applies to bytes that
arrive, never to a declared `Content-Length`, which is the sender's claim.

## Deposit

```go
dep, err := fileguard.DepositFromURL(ctx, bucket, key, sourceURL, fileguard.DepositOptions{
    MaxBytes: 500 << 20, RequireMedia: true,
})
// or, for bytes you already hold:
dep, err := fileguard.DepositReader(ctx, bucket, key, body, opts)
```

Fetches a source and writes it somewhere a separate process can read it by key.

This is what lets a worker that opens untrusted files run with no network of its
own. The fetch happens here, in Go, behind `SafeHTTPClient`; the worker is then
handed a key into one bucket it did not choose, and never resolves a hostname.
A worker that fetches the URL itself re-resolves the name, and no amount of
up-front validation covers that.

The bytes are sniffed *before* they are stored, so a source whose content
contradicts what the pipeline handles never comes to rest at all. `RequireMedia`
is what separates a media pipeline from a document one — `Sniff` recognises
both.

`ObjectWriter` is the one-method port it needs (`Upload(ctx, key, body,
contentType)`), declared here rather than imported so this package keeps no
dependencies. Any S3 wrapper satisfies it as it stands.
