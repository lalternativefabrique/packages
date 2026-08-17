// Package tts reads written text aloud.
//
// It speaks the OpenAI /v1/audio/speech protocol, which is the shape hosted
// services and self-hosted shims alike expose: pointing BaseURL at a Piper
// container in your own cluster and at api.openai.com are the same code path,
// and moving between them is a config change.
//
// Long text is cut up, read in parallel and joined back together, because the
// endpoint takes a few thousand characters at a time. That plumbing is the
// reason this package exists — it is where the mistakes are, and they are
// expensive to rediscover: a cut in the wrong place is audible, a silent empty
// response drops a paragraph, and a stream without a declared length stops
// playing partway through.
package tts

import "context"

// Voice reads text aloud, either all at once or as it goes.
type Voice interface {
	// Speak returns the finished audio.
	//
	// The whole payload is held rather than passed along so the caller can
	// serve it with a Content-Length. Without one, browsers infer the duration
	// from the first MP3 frame header and stop playback at the first cut — a
	// long text plays only its opening seconds, with nothing reported as
	// wrong. Use this for audio that is stored, published, or served to a
	// plain <audio src>.
	Speak(ctx context.Context, text string) (audio []byte, mime string, err error)

	// SpeakStream hands each piece of audio to emit as it becomes ready, in
	// reading order, so listening can start on the first one instead of after
	// the last. Use it when someone is waiting to hear the text.
	//
	// The pieces join into exactly what Speak returns, but the receiving end
	// has to be able to play a growing stream: served as chunked HTTP, it
	// needs MediaSource on the other side, since a plain <audio src> stops at
	// the first cut for want of a declared duration.
	//
	// Order holds even though pieces are read in parallel: one that finishes
	// early waits for those before it, since audio arriving out of order is
	// text read out of order. An emit that returns an error ends the reading —
	// nothing keeps running once nobody is listening.
	SpeakStream(ctx context.Context, text string, emit func(audio []byte) error) (mime string, err error)
}

var _ Voice = (*OpenAIVoice)(nil)
