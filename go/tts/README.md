# Go TTS

Reads written text aloud, over the OpenAI `/v1/audio/speech` protocol. That
protocol is the shape hosted services and self-hosted shims alike expose, so
pointing at `api.openai.com` and at a Piper container in your own cluster is
the same code path and a config change.

Module path: `github.com/lalternative/packages/go/tts`. Standard library only.

## Why this exists

Synthiz built this against OpenAI, moved to self-hosted Piper without touching
the calling code, and paid for a handful of mistakes on the way. This package
is that extraction, minus its cost-tracking and logging: the plumbing between
"here is some text" and "here is audio that plays to the end" is the part worth
sharing, and the part nobody should discover twice.

## Two ways to listen

```go
voice := tts.NewOpenAIVoice(tts.Config{
    BaseURL: os.Getenv("TTS_URL"),   // empty talks to OpenAI
    APIKey:  os.Getenv("TTS_API_KEY"),
    Model:   "fr_FR-siwis-medium",
    VoiceID: "fr_FR-upmc-medium",
})

audio, mime, err := voice.Speak(ctx, article)
```

`Speak` hands back the finished audio. Use it for anything stored, published,
or served to a plain `<audio src>` — it lets the HTTP layer send a
`Content-Length`, without which browsers infer the duration from the first MP3
frame header and stop playing at the first cut.

```go
mime, err := voice.SpeakStream(ctx, article, func(audio []byte) error {
    _, err := w.Write(audio)
    return err
})
```

`SpeakStream` hands over each piece as it is ready, so listening starts on the
first one instead of after the last. The receiving end has to cope with a
growing stream: served as chunked HTTP, that means MediaSource on the other
side, for the same reason `Speak` exists at all.

Returning an error from `emit` ends the reading. Nothing keeps running once
nobody is listening.

## What the plumbing handles

**Cutting.** `/v1/audio/speech` takes `MaxChars` runes at a time. `Split`
prefers the boundaries a reader would pause at anyway — paragraphs, then
sentences, then words — and only breaks mid-word for a word longer than a whole
request. Where a cut lands is not cosmetic: each piece is read as a complete
utterance, so cutting mid-sentence gives the first half a falling intonation and
the second half a fresh opening one, and the seam is plainly audible.

**Order.** Pieces are read in parallel, four at a time by default, and joined
in reading order. A piece that finishes early waits for those before it: audio
arriving out of order is text read out of order.

**Formats.** Pieces are joined as bytes, which works for frame-based codecs —
mp3, opus, aac, flac — because codec settings stay identical across requests for
one `(model, voice, format)`. `wav` would need its header rewritten to declare
the real length; a `wav` built this way announces the duration of its first
piece alone.

**Empty answers.** A `200` carrying no bytes is treated as a failure rather than
as silence. Joined with the rest it would drop that piece's text with nothing
reported anywhere, and the gap would outlive the request in whatever cache the
caller keeps.

**Failures.** The first real failure cancels the rest — a partial reading is
unusable, so there is no reason to pay for the remainder — and the error names
the piece that actually failed rather than the cancellations that followed it.

## Metering

Billing is per character of input, which is known before any audio comes back.
`Config.OnUsage` is called with the rune count as reading begins:

```go
tts.Config{OnUsage: func(chars int) { meter.Record("tts", chars) }}
```

The package has no opinion on what that hooks into, and no dependency on one.

## Turning it off

There is no "disabled" mode here. A caller that wants the feature to vanish when
unconfigured — no endpoint, no button — should build the voice only when its URL
is set, and treat a nil `Voice` as the feature being off.
