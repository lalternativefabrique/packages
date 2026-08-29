package audioreader

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	sharedtts "github.com/lalternative/packages/go/tts"
)

// ErrNotFound is what a Store's Download returns for a missing key. Callers
// wrap their own not-found error so errors.Is sees through to this one —
// see the Store doc comment.
var ErrNotFound = errors.New("audioreader: object not found")

// Store is the slice of an object store a reading needs: put one away, fetch
// one back. Narrower than a whole store on purpose — presigning and deleting
// are no business of serving audio, and a test standing in for this should
// not have to implement them.
//
// Download must report a missing key as an error satisfying
// errors.Is(err, ErrNotFound) — wrap the store's own sentinel with
// fmt.Errorf("...: %w", ErrNotFound) or return ErrNotFound directly.
type Store interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
}

// Reader serves a reading of text: from cache when there is one, otherwise by
// reading it aloud and keeping the result for the next listener.
type Reader struct {
	tts         Provider
	storage     Store
	openingSize int
	log         *slog.Logger
}

// Request is one thing to read aloud.
//
// Scope and ID name where the reading is kept; BillTo is who pays for it, and
// is not always the listener — a shared page is read on its owner's account.
type Request struct {
	Scope  string
	ID     string
	Text   string
	BillTo string
}

// NewReader returns nil when there is no voice to read with, which is how the
// feature stays absent rather than half-present: callers check for nil and
// answer that audio is unavailable instead of failing at the first byte.
//
// A nil store is allowed. Every reading is then paid for, which is worse but
// still works; no voice at all cannot be worked around.
//
// A nil logger defaults to slog.Default().
func NewReader(provider Provider, store Store, openingSize int, log *slog.Logger) *Reader {
	if provider == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reader{
		tts:         provider,
		storage:     store,
		openingSize: openingSize,
		log:         log,
	}
}

// WantsStream reports whether a request should be answered piece by piece
// rather than whole.
//
// Only a client that asked for it: a stream carries no Content-Length, and one
// that arrives at a plain <audio src> plays its opening seconds and stops,
// because the browser takes the duration from the first frame header. The
// player opts in by asking, and it only asks when it can drive MediaSource.
//
// The parameter decides on its own, and a Range header alongside it does not
// override it. The player fetches this itself and feeds the bytes to a
// SourceBuffer, so a range it never meant to send — browsers attach one to
// media requests readily — would otherwise turn the stream off on every call,
// silently, leaving the listener waiting out the whole reading again.
//
// A range without the parameter is served whole, which is what it is asking
// for: part of something whose size is known is the opposite of a stream.
func WantsStream(r *http.Request) bool {
	return r.URL.Query().Get("stream") == "1"
}

// Serve answers the request with audio, streaming it when asked.
//
// fields carries whatever the caller wants in the logs alongside what this
// records; it is written to, so callers pass a map they own. A nil fields is
// replaced with a fresh one.
func (r *Reader) Serve(w http.ResponseWriter, req *http.Request, ar Request, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	ctx := req.Context()
	startedAt := time.Now()
	key := CacheKey(ar.Scope, ar.ID, ar.Text)

	if audio, ok := r.cached(ctx, key, fields); ok {
		fields["cache"] = "hit"
		fields["duration_ms"] = time.Since(startedAt).Milliseconds()
		r.write(w, req, audio, "audio/mpeg", fields)
		return
	}

	// Nothing cached, so this is the listen that pays for the reading. Stream
	// it: a long text is several speech calls, and buffering them all leaves
	// the listener on a spinner for the whole run when the opening seconds
	// have been ready almost from the start.
	if WantsStream(req) {
		r.stream(w, req, ar, key, fields, startedAt)
		return
	}

	audio, mime, err := r.tts.Synthesize(ctx, ar.Text, ar.BillTo)
	if err != nil {
		r.log.Error("audio: synthesize failed", errAttrs(err, fields)...)
		http.Error(w, fmt.Sprintf("TTS error: %s", err.Error()), http.StatusBadGateway)
		return
	}
	// Cached before the response is written: the reading is paid for and
	// complete at this point, so it must be kept even when the listener stops
	// part way — otherwise every listen re-pays the full latency.
	r.Cache(key, audio, mime)

	fields["cache"] = "miss"
	fields["duration_ms"] = time.Since(startedAt).Milliseconds()
	r.write(w, req, audio, mime, fields)
}

// ServeCached answers only from the cache, never generating. It is what a
// route with no billable listener behind it must use: a public share view has
// no session to bill a reading to, so the owner pays once by sharing and
// every visit after that either finds the cache filled or finds nothing — it
// must never fall back to paying for a reading itself, which would let a
// visitor with no account trigger a charge on the owner's.
func (r *Reader) ServeCached(w http.ResponseWriter, req *http.Request, ar Request, fields map[string]any) {
	if fields == nil {
		fields = map[string]any{}
	}
	ctx := req.Context()
	key := CacheKey(ar.Scope, ar.ID, ar.Text)

	audio, ok := r.cached(ctx, key, fields)
	if !ok {
		http.Error(w, "no reading available yet", http.StatusNotFound)
		return
	}
	fields["cache"] = "hit"
	r.write(w, req, audio, "audio/mpeg", fields)
}

// Exists reports whether a reading is already cached, without reading its
// bytes. For a caller that only needs to know whether to offer a play
// button — a public share view — and would otherwise buffer the whole file
// just to throw it away.
func (r *Reader) Exists(ctx context.Context, ar Request) bool {
	if r.storage == nil {
		return false
	}
	obj, err := r.storage.Download(ctx, CacheKey(ar.Scope, ar.ID, ar.Text))
	if err != nil {
		return false
	}
	obj.Close()
	return true
}

// Pregenerate reads text aloud and caches it if nothing is cached yet, without
// serving an HTTP response.
//
// It is what turns sharing into the moment a public reading is paid for: the
// owner's own action pays for it once, up front, rather than leaving the
// first visitor's request as the trigger for a bill on someone else's
// account. A cache hit costs nothing, so re-priming an already-shared entity
// on every edit is cheap when nothing actually changed since the last share.
func (r *Reader) Pregenerate(ctx context.Context, ar Request) error {
	key := CacheKey(ar.Scope, ar.ID, ar.Text)
	if _, ok := r.cached(ctx, key, map[string]any{}); ok {
		return nil
	}
	audio, mime, err := r.tts.Synthesize(ctx, ar.Text, ar.BillTo)
	if err != nil {
		return fmt.Errorf("pregenerate: %w", err)
	}
	if len(audio) == 0 {
		return fmt.Errorf("pregenerate: no audio for %d runes", len([]rune(ar.Text)))
	}
	// Uploaded in-line rather than through Cache: that method is fire-and-
	// forget on purpose, built for a response already being written to the
	// listener. Here nobody is waiting on bytes yet — the point of
	// pregenerating is that the cache is filled before the share link is
	// handed out, so a failed upload has to be reported, not swallowed.
	if r.storage == nil {
		return nil
	}
	if err := r.storage.Upload(ctx, key, bytes.NewReader(audio), mime); err != nil {
		return fmt.Errorf("pregenerate: store: %w", err)
	}
	return nil
}

// cached returns a finished reading when one is kept.
//
// The object is buffered rather than piped through so the response can carry a
// Content-Length: browsers need a declared length to know the real duration
// and to allow seeking.
func (r *Reader) cached(ctx context.Context, key string, fields map[string]any) ([]byte, bool) {
	if r.storage == nil {
		return nil, false
	}
	obj, err := r.storage.Download(ctx, key)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			r.log.Warn("audio: cache lookup failed, falling back to TTS",
				slog.String("error", err.Error()), slog.String("key", key))
		}
		return nil, false
	}
	defer obj.Close()

	audio, err := io.ReadAll(obj)
	if err != nil {
		r.log.Warn("audio: cache read failed, falling back to TTS",
			slog.String("error", err.Error()), slog.String("key", key))
		return nil, false
	}
	return audio, true
}

// FramesContentType is what a streamed response carries instead of
// "audio/mpeg": each piece is a complete, independently decodable mp3, and a
// player has no way to find where one ends and the next begins in a plain
// concatenation of their bytes. This type is the signal to expect frames.
const FramesContentType = "application/x-lalter-audio-frames"

// writeFrame sends one piece length-prefixed: a big-endian uint32 byte count
// followed by that many bytes. The prefix is what lets a listener split the
// stream back into the same independently-decodable pieces it was built
// from, since concatenated mp3 frames carry no boundary of their own.
func writeFrame(w io.Writer, piece []byte) error {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(piece)))
	if _, err := w.Write(length[:]); err != nil {
		return err
	}
	_, err := w.Write(piece)
	return err
}

// stream reads the text aloud and sends each piece as it arrives, so playback
// can start on the first one.
//
// Everything that could refuse the request has to happen before the first
// write: once a byte is out, the status line is spent and an error can only be
// a truncated file. That is why a failure part way through simply ends the
// response — there is nowhere left to report it, and the log is where it goes
// instead.
//
// The pieces are kept as they pass so the cache can still be filled. A listen
// that streamed and a listen served from cache produce the same bytes; the
// difference is only who waited.
func (r *Reader) stream(w http.ResponseWriter, req *http.Request, ar Request, key string, fields map[string]any, startedAt time.Time) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-TTS-Cache", "stream")

	var whole bytes.Buffer
	var wroteHeader bool
	var firstPieceAt time.Duration

	flusher, canFlush := w.(http.Flusher)

	// The opening may already have been read before anyone asked. Sending it
	// first is what makes pressing play feel immediate: the rest is read while
	// it plays, and the listener hears it as one recording.
	opening, rest := r.openingFor(req.Context(), ar)
	if len(opening) > 0 {
		firstPieceAt = time.Since(startedAt)
		w.Header().Set("Content-Type", FramesContentType)
		w.WriteHeader(http.StatusOK)
		wroteHeader = true
		whole.Write(opening)
		if err := writeFrame(w, opening); err != nil {
			r.log.Warn("audio: listener left before the opening landed", attrs(fields)...)
			return
		}
		if canFlush {
			flusher.Flush()
		}
		fields["primed"] = true
	}

	mime, err := r.tts.SynthesizeStream(req.Context(), rest, ar.BillTo, func(piece []byte) error {
		if !wroteHeader {
			// What the listener actually waits for. The total says how long
			// the reading took; this says when they heard something, which is
			// the whole point of streaming and the only number that shows
			// whether it is working.
			firstPieceAt = time.Since(startedAt)
			// Written on the first piece rather than up front: until one
			// arrives the reading may still fail, and a 200 already sent
			// cannot be taken back.
			w.Header().Set("Content-Type", FramesContentType)
			w.WriteHeader(http.StatusOK)
			wroteHeader = true
		}
		whole.Write(piece)
		if err := writeFrame(w, piece); err != nil {
			return err
		}
		if canFlush {
			flusher.Flush()
		}
		return nil
	})

	fields["bytes"] = whole.Len()
	fields["duration_ms"] = time.Since(startedAt).Milliseconds()
	fields["first_piece_ms"] = firstPieceAt.Milliseconds()
	fields["cache"] = "stream"

	if err != nil {
		if !wroteHeader {
			r.log.Error("audio: stream failed before any audio", errAttrs(err, fields)...)
			http.Error(w, fmt.Sprintf("TTS error: %s", err.Error()), http.StatusBadGateway)
			return
		}
		// The listener has part of a track and no way to be told why it
		// stopped. Nothing is cached: a truncated reading served for an hour
		// would be worse than the wait of doing it again.
		r.log.Error("audio: stream cut short", errAttrs(err, fields)...)
		return
	}
	if whole.Len() == 0 {
		r.log.Error("audio: empty stream", errAttrs(errors.New("no audio bytes to serve"), fields)...)
		http.Error(w, "TTS returned no audio", http.StatusBadGateway)
		return
	}

	r.Cache(key, whole.Bytes(), mime)
	r.log.Info("audio: streamed", attrs(fields)...)
}

// write sends a fully-buffered payload via http.ServeContent.
//
// Serving via http.ServeContent rather than a bare Write is deliberate: it
// sets Content-Length, honours Range requests with a 206, and handles
// conditional requests. All three matter for audio — without a declared
// length the browser infers duration from the first MP3 header and stops
// playback at the first chunk boundary, and iOS Safari range-requests media
// by default, so advertising Accept-Ranges without serving ranges would break
// mobile playback.
//
// An empty payload is a server-side failure, not a valid track: the player
// would otherwise receive a well-formed silent file with no error to surface.
func (r *Reader) write(w http.ResponseWriter, req *http.Request, audio []byte, mime string, fields map[string]any) {
	fields["bytes"] = len(audio)
	fields["mime"] = mime

	if len(audio) == 0 {
		r.log.Error("audio: empty payload", errAttrs(errors.New("no audio bytes to serve"), fields)...)
		http.Error(w, "TTS returned no audio", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if cache, ok := fields["cache"].(string); ok {
		w.Header().Set("X-TTS-Cache", cache)
	}

	// modtime zero disables Last-Modified/If-Modified-Since: the cache key is
	// already content-hashed, so freshness is keyed on the URL, not on time.
	http.ServeContent(w, req, "", time.Time{}, bytes.NewReader(audio))

	r.log.Info("audio: done", attrs(fields)...)
}

// Cache stores a finished reading for the next listen.
//
// Detached context on purpose: the reading is paid for and complete, and the
// request it was made for may be torn down the moment the last piece lands.
// Losing the upload there would have every listen pay the full latency again.
func (r *Reader) Cache(key string, audio []byte, mime string) {
	if r.storage == nil || len(audio) == 0 {
		return
	}
	uploadCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	go func() {
		defer cancel()
		if err := r.storage.Upload(uploadCtx, key, bytes.NewReader(audio), mime); err != nil {
			r.log.Warn("audio: cache upload failed",
				slog.String("error", err.Error()), slog.String("key", key))
		}
	}()
}

// openingFor returns audio for the start of the text if it was read ahead of
// time, along with whatever is left to read now.
//
// The split is the one PrimeOpening made, so the two halves meet exactly where
// a cut would have fallen anyway — no word is read twice and none is skipped.
// Anything missing or unreadable gives back the whole text, which is simply
// the behaviour before priming existed.
func (r *Reader) openingFor(ctx context.Context, ar Request) (audio []byte, rest string) {
	if r.storage == nil || ar.ID == "" {
		return nil, ar.Text
	}
	pieces := sharedtts.Split(ar.Text, r.openingSize)
	if len(pieces) < 2 {
		// One piece is no faster to serve in halves, and the reading is short
		// enough that priming would save nothing worth the complication.
		return nil, ar.Text
	}

	obj, err := r.storage.Download(ctx, OpeningKey(ar.Scope, ar.ID, ar.Text))
	if err != nil {
		return nil, ar.Text
	}
	defer obj.Close()
	audio, err = io.ReadAll(obj)
	if err != nil || len(audio) == 0 {
		return nil, ar.Text
	}
	return audio, strings.Join(pieces[1:], "\n\n")
}

// OpeningChars is how much of a text counts as its opening.
//
// It matches the size a reading is cut into, so a primed opening is a piece
// the reading would have produced anyway — the two halves then meet where a
// cut would have fallen, and no word is read twice or skipped.
//
// envVar names the environment variable a caller wants consulted (e.g.
// "TTS_MAX_CHARS"); an empty or unparseable value falls back to fallback.
func OpeningChars(envVar string, fallback int) int {
	if v := os.Getenv(envVar); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func errAttrs(err error, fields map[string]any) []any {
	return append([]any{"error", err.Error()}, attrs(fields)...)
}

func attrs(fields map[string]any) []any {
	out := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		out = append(out, k, v)
	}
	return out
}
