# Go Transcribe

Turns an audio source into timed transcript segments. The audio is localized,
split into chunks with ffmpeg when it is too long or too large for one
request, transcribed chunk by chunk in parallel against a speech-to-text
backend with retry and backoff, and handed back as segments re-offset to
absolute time.

Module path: `github.com/lalternative/packages/go/transcribe`. Standard
library only.

```
go get github.com/lalternative/packages/go/transcribe@go/transcribe/v0.1.0
```

## Why this exists

Synthiz built this to transcribe podcasts and uploads, and paid for the
mistakes on the way: an HLS URL that came back as a playlist instead of
audio, a six-hour file transcribed in full before anyone noticed, a 429 that
burned the retry budget instead of waiting. This is that extraction, minus
its cost tracking, logging and YouTube handling: the plumbing between "here is
some audio" and "here is a transcript with timings" is the part worth sharing.

## What it needs

**ffmpeg and ffprobe on the host.** Both are called as external binaries, on
`PATH` by default or at `Options.FFmpegPath` / `Options.FFprobePath`. ffprobe
measures the duration that routes a file single-shot or chunked and that the
duration cap judges; ffmpeg splits long files and localizes URLs. Without
ffprobe the duration is unknown and the file goes single-shot if it fits the
provider's size limit; without ffmpeg a file that must be chunked falls back
to one single-shot attempt. Tests that need the real binaries skip themselves
when they are absent.

**A `Provider`.** The `providers` subpackage ships three, all over HTTP:

```go
import (
    "github.com/lalternative/packages/go/transcribe"
    "github.com/lalternative/packages/go/transcribe/providers"
)

p := providers.NewOpenAI(providers.OpenAIConfig{
    APIURL:  "https://api.openai.com/v1/audio/transcriptions",
    APIKey:  os.Getenv("OPENAI_API_KEY"),
    ModelID: "whisper-1",
})
// providers.NewGroq(providers.GroqConfig{APIKey: ...})          // whisper-large-v3, 25 MB
// providers.NewWhisperCpp(providers.WhisperCppConfig{APIURL: ...}) // WAV 16kHz mono, no auth
// providers.FromEnv("")                                            // <prefix>WHISPER_* variables
```

Anything else satisfies the interface:

```go
type Provider interface {
    Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error)
    MaxFileSize() int64
    NeedsWAVConversion() bool
}
```

Return a `*transcribe.RateLimitError` on HTTP 429 (wrapped with `%w` if you
wrap it) and the orchestrator honours `Retry-After` and widens the retry
budget. Cost tracking belongs in a wrapper around your `Provider`; this
package never meters.

## Transcribing

```go
tr := transcribe.New(p)
res, err := tr.Transcribe(ctx, transcribe.URLSource{URL: audioURL}, transcribe.Options{
    MaxParallel:        4,
    MaxDurationSeconds: 2 * 3600,
})
if errors.Is(err, transcribe.ErrTooLong) { ... }

for _, s := range res.Segments {
    // s.Text, s.Start, s.End (absolute seconds), s.HasTiming
}
text := transcribe.JoinText(res.Segments)
```

A `Source` is where the audio comes from. `URLSource` lets ffmpeg fetch a
direct audio URL, stream-copying into a container matching the codec (or
re-encoding to WAV with `WAV: true` for whisper.cpp). `FileSource` is a path
already on disk. `ObjectSource` downloads from anything with a
`Download(ctx, key) (io.ReadCloser, error)` method, with an optional byte
bound.

`Result` carries the segments, the measured `DurationSeconds` (0 when
unknown, with `ProbeError` saying why), the backend's detected `Language`,
and `FailedChunks`. Up to half the chunks may fail permanently: their text is
replaced by a marker and the rest is kept. More than half fails the call.

## Guarding the bill

Every gate runs before the first provider call.

- `MaxDurationSeconds` refuses measured audio longer than it with
  `ErrTooLong`. Audio ffprobe could not measure passes through.
- `MaxBytesWhenUnmeasurable` refuses, with `ErrUnmeasurable`, a file ffprobe
  could not measure when it exceeds this size. A measured file is never judged
  on its size.
- `RequireMeasurableAudio` turns the probe into a gate: no decodable audio
  stream (`ErrNoAudioStream`), no finite duration (`ErrUnboundedDuration`,
  what a live stream looks like) or unreadable media (`ErrUnreadableMedia`)
  is refused.

`Probe` and `ProbeDuration` are exported for callers that want the
measurement on its own.
