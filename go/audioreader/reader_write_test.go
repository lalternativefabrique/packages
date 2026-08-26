package audioreader

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func writeTestReader() *Reader { return &Reader{log: testLogger()} }

// Regression test: a multi-chunk synthesis used to be served as a chunked
// response with no Content-Length. Browsers then infer duration from the
// first MP3 header and end playback at the first chunk boundary, cutting the
// audio off before the end of the text. A declared length fixes playback and
// enables seeking.
func TestWrite_DeclaresLength(t *testing.T) {
	audio := []byte("chunk-0-bytes" + "chunk-1-bytes")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio", nil)
	writeTestReader().write(rec, req, audio, "audio/mpeg", map[string]any{"cache": "miss"})

	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(len(audio)) {
		t.Errorf("Content-Length = %q, want %d", got, len(audio))
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", got)
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := rec.Header().Get("X-TTS-Cache"); got != "miss" {
		t.Errorf("X-TTS-Cache = %q, want miss", got)
	}
	// The whole payload must reach the client, not just the first chunk.
	if rec.Body.Len() != len(audio) {
		t.Errorf("body = %d bytes, want %d", rec.Body.Len(), len(audio))
	}
}

// Regression: the handler once advertised Accept-Ranges: bytes while ignoring
// Range headers, answering 200 with the whole file. iOS Safari range-requests
// audio by default, so a Range must yield 206 + the requested slice.
func TestWrite_HonoursRangeRequest(t *testing.T) {
	audio := []byte("0123456789ABCDEF")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio", nil)
	req.Header.Set("Range", "bytes=4-7")

	writeTestReader().write(rec, req, audio, "audio/mpeg", map[string]any{"cache": "hit"})

	t.Logf("status=%d content-length=%q content-range=%q body=%q bodylen=%d",
		rec.Code, rec.Header().Get("Content-Length"), rec.Header().Get("Content-Range"),
		rec.Body.String(), rec.Body.Len())

	if rec.Code != http.StatusPartialContent {
		t.Errorf("status = %d, want 206 for a Range request", rec.Code)
	}
	if rec.Body.Len() != 4 {
		t.Errorf("body = %d bytes, want 4 (bytes 4-7)", rec.Body.Len())
	}
}

// Regression: an empty payload was served as a valid 200, leaving the player
// with a well-formed silent file and no error to surface. It must fail loudly.
func TestWrite_RejectsEmptyPayload(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio", nil)

	writeTestReader().write(rec, req, []byte{}, "audio/mpeg", map[string]any{"cache": "miss"})

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}
