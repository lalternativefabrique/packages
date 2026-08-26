package audioreader

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The public share view has no session to bill a reading to. A cache-miss
// there must refuse rather than fall back to paying for the reading — that
// fallback is exactly what would let a visitor with no account trigger a
// charge on the owner's.
func TestServeCachedRefusesRatherThanGenerates(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{audio: []byte("would be billed")}
	r := &Reader{tts: voice, storage: store, openingSize: testOpeningChars, log: testLogger()}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/audio", nil)
	r.ServeCached(rec, req, Request{Scope: testScope, ID: "p-1", Text: "un texte", BillTo: "owner"}, nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if len(voice.spoke) != 0 {
		t.Error("a cache-miss on the public route must never call the voice")
	}
}

func TestServeCachedServesAPrimedReading(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{audio: []byte("primed bytes")}
	r := &Reader{tts: voice, storage: store, openingSize: testOpeningChars, log: testLogger()}

	req := Request{Scope: testScope, ID: "p-1", Text: "un texte", BillTo: "owner"}
	if err := r.Pregenerate(t.Context(), req); err != nil {
		t.Fatalf("Pregenerate: %v", err)
	}

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodGet, "/audio", nil)
	r.ServeCached(rec, httpReq, req, nil)
	if rec.Body.Len() == 0 {
		t.Error("expected the primed audio to be served")
	}
}
