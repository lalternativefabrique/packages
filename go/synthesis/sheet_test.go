package synthesis

import (
	"testing"

)

func TestSourceChunksKeepTheGlobalRefsAndTimecodes(t *testing.T) {
	chunks := []chunk{{Content: "a", StartSeconds: 1, EndSeconds: 2}, {Content: "b", StartSeconds: 3, EndSeconds: 4}}

	got := sourceChunks(chunks, true, 90)
	if got[0].Ref != "s91" || got[1].Ref != "s92" || *got[1].StartSeconds != 3 {
		t.Fatalf("chunks = %+v", got)
	}
	if plain := sourceChunks(chunks, false, 0); plain[0].Ref != "s1" || plain[0].StartSeconds != nil {
		t.Fatalf("chunks = %+v", plain)
	}
}

func TestSynthesizeSourceInputScalesToTheSource(t *testing.T) {
	in := newSynthesizeSourceInput([]chunk{{Content: "court"}}, Source{Type: "youtube", Title: "T"}, "")
	if in.Language != "en" || in.Targets.Concepts.Min != 2 || in.Chunks[0].StartSeconds == nil {
		t.Fatalf("input = %+v", in)
	}
}

func validSheet() SynthesisSheet {
	return SynthesisSheet{
		Title: "t", Summary: "s", WhyItMatters: "w",
		Concepts:    []SheetConcept{{Text: "c", ChunkRefs: []string{"s2", "s9"}}},
		Development: []SheetDevelopment{{Axis: "a", Content: "x", ChunkRefs: []string{"s1"}}},
	}
}

func TestSheetContentResolvesKnownRefsOnly(t *testing.T) {
	chunks := []chunk{{Content: "one", StartSeconds: 10}, {Content: "two", StartSeconds: 20}}
	source := Source{ID: "src", Type: "youtube", URL: "https://y.be/v?x=1"}

	got := validSheet().content(chunks, source)
	cites := got.Concepts[0].Citations
	if len(cites) != 1 || cites[0].Text != "two" || cites[0].URL != "https://y.be/v?x=1&t=20s" || cites[0].LocatorType != "timecode" {
		t.Fatalf("citations = %+v", cites)
	}
}

func TestSheetValidation(t *testing.T) {
	if err := validSheet().Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*SynthesisSheet){
		"no title":         func(s *SynthesisSheet) { s.Title = "" },
		"no concept":       func(s *SynthesisSheet) { s.Concepts = nil },
		"uncited concept":  func(s *SynthesisSheet) { s.Concepts[0].ChunkRefs = nil },
		"no whyItMatters":  func(s *SynthesisSheet) { s.WhyItMatters = "" },
		"half development": func(s *SynthesisSheet) { s.Development[0].Content = "" },
	}
	for name, mutate := range cases {
		s := validSheet()
		mutate(&s)
		if s.Validate() == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
