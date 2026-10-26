package busevents

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestSynthesisCachedRoundTrip(t *testing.T) {
	in := SynthesisCachedEvent{
		Meta:          Meta{EventID: "ev-s", OccurredAt: at},
		SourceType:    "youtube",
		SourceURL:     "https://www.youtube.com/watch?v=abc",
		Language:      "fr",
		EngineVersion: "1",
		Segments:      json.RawMessage(`[{"text":"t","start_seconds":0,"end_seconds":4}]`),
		Content:       json.RawMessage(`{"title":"T"}`),
	}
	body, _, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode[SynthesisCachedEvent](body)
	if err != nil || out.SourceURL != in.SourceURL || string(out.Content) != `{"title":"T"}` {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if Subject(Synthiz, SynthesisCached) != "events.synthiz.synthesis.cached" {
		t.Fatal(Subject(Synthiz, SynthesisCached))
	}
}

func TestSynthesisCachedNeedsItsKeyAndSheet(t *testing.T) {
	_, _, err := Encode(SynthesisCachedEvent{Meta: Meta{EventID: "ev", OccurredAt: at}, SourceType: "webpage", SourceURL: "https://a.b/", Language: "fr", EngineVersion: "1"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}
