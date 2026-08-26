// Package audioreader serves a text-to-speech reading over HTTP, streaming
// it to the listener as it is produced instead of making them wait for the
// whole thing.
//
// The cutting, the parallel reading, and the ordered joining live in
// packages/go/tts. What lives here is what sits on top of it regardless of
// which app is calling: caching a finished reading, priming its opening
// ahead of time, and serving both as a stream a browser can start playing on
// the first byte. A second copy of this per app is a second place for the
// MediaSource sequencing and the cache-fill to drift.
package audioreader

import "context"

// Provider turns text into audio, whole or as it is produced. An app's own
// TTS client satisfies this — see packages/go/tts for one built against the
// OpenAI-shaped /v1/audio/speech protocol.
//
// A caller with per-user billing wraps its own client to implement this
// rather than this package growing an opinion about it: attributing a
// reading's cost is business each app already owns.
type Provider interface {
	// Synthesize returns the complete audio for text.
	//
	// The full payload is returned rather than a stream so the caller can
	// serve it with a Content-Length: a chunked MP3 response has no declared
	// duration, and browsers end playback at the first chunk boundary rather
	// than at the real end of the audio.
	Synthesize(ctx context.Context, text, billTo string) (audio []byte, mime string, err error)

	// SynthesizeStream hands each piece of audio to emit as it is ready, in
	// reading order, so playback can start on the first one instead of after
	// the last. The receiving end has to cope with a growing stream — over
	// HTTP that means MediaSource on the other side, for the same reason
	// Synthesize exists at all.
	SynthesizeStream(ctx context.Context, text, billTo string, emit func(audio []byte) error) (mime string, err error)
}
