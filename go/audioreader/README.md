# Go Audio Reader

Serves a text-to-speech reading over HTTP, streaming it to the listener as
it is produced instead of making them wait for the whole thing.

Module path: `github.com/lalternative/packages/go/audioreader`. Standard
library only, plus [`packages/go/tts`](../tts) for the `Provider` shape.

## Why this exists

Synthiz built this against a Go-typed `apps/core/pkg/audio` package, wired
to Echo and its own logger. This is that extraction, minus the framework and
the app-specific billing wrapper: the plumbing between "here is text worth
listening to" and "here is a stream a browser can start playing on the first
byte" is the part worth sharing, and the part nobody should discover twice.

## What it needs from a caller

A `Provider` — anything that turns text into audio, whole or as a stream.
`packages/go/tts` already builds one against the OpenAI-shaped
`/v1/audio/speech` protocol; wrap your own client to satisfy the interface
if you're pointed elsewhere:

```go
type Provider interface {
    Synthesize(ctx context.Context, text, billTo string) (audio []byte, mime string, err error)
    SynthesizeStream(ctx context.Context, text, billTo string, emit func(audio []byte) error) (mime string, err error)
}
```

`billTo` exists because most apps bill per user; a `Provider` implementation
is the seam for whatever metering you wire on top — this package has no
opinion on it.

A `Store` — an object store to cache finished readings and primed openings
in. A missing key must be reported as an error satisfying
`errors.Is(err, audioreader.ErrNotFound)`.

## Serving a reading

```go
reader := audioreader.NewReader(provider, store, audioreader.OpeningChars("TTS_MAX_CHARS", 800), nil)

http.HandleFunc("/synthesis/audio", func(w http.ResponseWriter, r *http.Request) {
    reader.Serve(w, r, audioreader.Request{
        Scope:  "synthesis",
        ID:     synthesisID,
        Text:   text,
        BillTo: userID,
    }, map[string]any{"synthesis_id": synthesisID})
})
```

`Serve` answers from cache when there is one. Otherwise, a request with
`?stream=1` gets the reading piece by piece as it's synthesized — the point
of this package — everything else gets it whole, with a `Content-Length` a
plain `<audio src>` can use. `NewReader` returns `nil` when `provider` is
`nil`, which is how a caller keeps the feature absent rather than
half-present: check for `nil` and answer that audio isn't configured instead
of failing on the first request.

A streamed response carries `Content-Type: audioreader.FramesContentType`,
not `audio/mpeg`: each piece is a complete, independently decodable mp3, and
concatenating their bytes on the wire gives a listener no way to find where
one ends and the next begins. Each piece is instead sent length-prefixed — a
big-endian uint32 byte count followed by that many bytes — so a frontend can
split the stream back into the same pieces and decode them one by one (e.g.
with Web Audio's `decodeAudioData`, queuing an `AudioBufferSourceNode` per
piece), starting playback on the first one without waiting for the rest.
MediaSource is not the fit here: mp3 support in a `SourceBuffer` is
inconsistent across browsers and absent on iOS Safari.

## Priming an opening

```go
primer := audioreader.NewPrimer(provider, store, audioreader.OpeningChars("TTS_MAX_CHARS", 800), nil)
primer.PrimeOpening(ctx, "synthesis", synthesisID, text)
```

Reads and caches the first piece of a text ahead of time — call this the
moment a reading becomes shareable (published, sent to a public link),
before anyone has asked to listen. `Reader.stream` looks for a primed
opening automatically and, when one exists, serves it as the first chunk
before synthesizing the rest — pressing play is then a cache hit, not the
start of a synthesis.

## Other pieces

- `Reader.Pregenerate` reads a whole text aloud and caches it without
  serving an HTTP response — for pre-paying a reading's cost at the moment
  it becomes shareable rather than on a visitor's first listen.
- `Reader.Exists` reports whether a reading is already cached, without
  reading its bytes — for a play button that shouldn't render before there's
  something to play.
- `Reader.ServeCached` answers only from cache, never generating — for a
  route with no billable listener behind it (a public share view), where
  falling back to a live synthesis would let a visitor trigger a charge on
  someone else's account.
- `CacheKey`/`OpeningKey` name where a reading and its primed opening are
  kept. The key hashes the text, so an edit or a language switch misses and
  is read again rather than serving stale audio.

## What makes the stream actually fast

This package streams what it's given — the latency it can deliver is
capped by how fast a `Provider` produces its first bytes. Cutting the text
small enough to keep a first piece's synthesis time under budget, and
making sure whatever serves the underlying TTS model genuinely streams
(rather than buffering a whole utterance before responding, which some
wrappers do despite a streaming-shaped API) is what turns this package's
plumbing into an actually low-latency reading.
