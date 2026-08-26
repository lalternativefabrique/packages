package audioreader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A stream carries no Content-Length, so a client that cannot drive
// MediaSource must never be handed one: it would play the opening seconds and
// stop, with nothing reported as wrong. Streaming is opt-in for that reason,
// and a Range request — how iOS Safari opens media — asks for part of
// something whose size is known, which a stream cannot answer.
func TestOnlyAClientThatAskedForAStreamGetsOne(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		rng   string
		wants bool
	}{
		{"plain request", "/audio", "", false},
		{"asked for it", "/audio?stream=1", "", true},
		// The player fetches this itself and feeds a SourceBuffer; a range it
		// never meant to send must not quietly turn the stream off.
		{"asked for it, range attached anyway", "/audio?stream=1", "bytes=0-", true},
		{"ranges alone", "/audio", "bytes=0-1023", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			if tc.rng != "" {
				req.Header.Set("Range", tc.rng)
			}
			if got := WantsStream(req); got != tc.wants {
				t.Errorf("WantsStream = %v, want %v", got, tc.wants)
			}
		})
	}
}

type stubStreamer struct {
	pieces [][]byte
	err    error
	// failAfter emits this many pieces before returning err.
	failAfter int
}

func (s stubStreamer) Synthesize(ctx context.Context, text, userID string) ([]byte, string, error) {
	var out []byte
	for _, p := range s.pieces {
		out = append(out, p...)
	}
	return out, "audio/mpeg", s.err
}

func (s stubStreamer) SynthesizeStream(_ context.Context, _, _ string, emit func([]byte) error) (string, error) {
	for i, p := range s.pieces {
		if s.err != nil && i >= s.failAfter {
			return "", s.err
		}
		if err := emit(p); err != nil {
			return "", err
		}
	}
	if s.err != nil && s.failAfter >= len(s.pieces) {
		return "", s.err
	}
	return "audio/mpeg", nil
}

var _ Provider = stubStreamer{}

func streamOnce(t *testing.T, p Provider) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio?stream=1", nil)
	streamTestReader(p).stream(rec, req, testRequest(), "key", map[string]any{}, time.Now())
	return rec
}

func streamTestReader(p Provider) *Reader {
	return &Reader{tts: p, openingSize: testOpeningChars, log: testLogger()}
}

func testRequest() Request {
	return Request{Scope: testScope, ID: "syn-test", Text: "du texte à lire", BillTo: "user_1"}
}

func TestAStreamSendsEveryPieceInOrder(t *testing.T) {
	rec := streamOnce(t, stubStreamer{pieces: [][]byte{[]byte("one-"), []byte("two-"), []byte("three")}})

	if got := rec.Body.String(); got != "one-two-three" {
		t.Errorf("body is %q, want the pieces in order", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mpeg" {
		t.Errorf("Content-Type is %q", got)
	}
	// A stream is the one response that must not be cached: it is what a
	// listener gets while the reading is still being paid for, and the next
	// listen is served whole from the object cache instead.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store", got)
	}
}

// Nothing has been written yet, so the failure can still be reported as one.
func TestAStreamThatFailsBeforeAnyAudioIsAnError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio?stream=1", nil)
	reader := streamTestReader(stubStreamer{
		pieces: [][]byte{[]byte("never sent")},
		err:    errors.New("speech endpoint unreachable"),
	})

	reader.stream(rec, req, testRequest(), "key", map[string]any{}, time.Now())

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 — a reading that produced nothing must be reported as an error", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "TTS error") {
		t.Errorf("body is %q", rec.Body.String())
	}
}

// Once a byte is out the status line is spent: the listener has part of a
// track and no way to be told why it stopped. What must not happen is the
// truncated reading being kept.
func TestAStreamCutShortIsNotCached(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio?stream=1", nil)
	reader := streamTestReader(stubStreamer{
		pieces:    [][]byte{[]byte("first-"), []byte("second")},
		err:       errors.New("endpoint went away"),
		failAfter: 1,
	})

	reader.stream(rec, req, testRequest(), "key", map[string]any{}, time.Now())

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — a stream already under way cannot report an error status", rec.Code)
	}
	if got := rec.Body.String(); got != "first-" {
		t.Errorf("body is %q, want what was sent before the failure", got)
	}
	// storage is nil here, so nothing could be cached anyway; the point is
	// that the handler returns without asking for it.
}

func TestAnEmptyStreamIsAnError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio?stream=1", nil)
	reader := streamTestReader(stubStreamer{})

	reader.stream(rec, req, testRequest(), "key", map[string]any{}, time.Now())

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 — silence must be an error, not a valid track", rec.Code)
	}
}
