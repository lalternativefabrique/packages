package audioreader

import (
	"context"
	"errors"
	"testing"
)

// A cache hit costs nothing: pregenerating an already-shared entity on every
// edit must not pay for a reading twice when the text has not changed.
func TestPregenerateSkipsAnExistingReading(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{audio: []byte("audio bytes")}
	r := &Reader{tts: voice, storage: store, openingSize: testOpeningChars, log: testLogger()}

	req := Request{Scope: testScope, ID: "p-1", Text: "un texte", BillTo: "owner"}
	if err := r.Pregenerate(context.Background(), req); err != nil {
		t.Fatalf("first pregenerate: %v", err)
	}
	if err := r.Pregenerate(context.Background(), req); err != nil {
		t.Fatalf("second pregenerate: %v", err)
	}

	if len(voice.spoke) != 1 {
		t.Errorf("read %d times, want one — the second call must hit the cache", len(voice.spoke))
	}
}

// The share action is what a listener trusts to have paid for the reading
// already: the upload has to be reported so the caller can retry rather than
// silently hand out a link whose first visitor pays for it instead.
func TestPregenerateReportsAStorageFailure(t *testing.T) {
	store := newPrimedStore()
	store.fail = true
	voice := &recordingVoice{audio: []byte("audio bytes")}
	r := &Reader{tts: voice, storage: store, openingSize: testOpeningChars, log: testLogger()}

	err := r.Pregenerate(context.Background(), Request{Scope: testScope, ID: "p-1", Text: "un texte", BillTo: "owner"})
	if err == nil {
		t.Fatal("a failed upload must be reported, not swallowed")
	}
}

func TestPregenerateReportsATTSFailure(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{err: errors.New("tts unreachable")}
	r := &Reader{tts: voice, storage: store, openingSize: testOpeningChars, log: testLogger()}

	err := r.Pregenerate(context.Background(), Request{Scope: testScope, ID: "p-1", Text: "un texte", BillTo: "owner"})
	if err == nil {
		t.Fatal("a failed synthesis must be reported")
	}
}
