package retrieval

import "testing"

func passage(sourceID string, dist float64, sourceChunks int) Passage {
	d := dist
	return Passage{SourceID: sourceID, Distance: &d, SourceChunks: sourceChunks}
}

func keptSources(passages []Passage) map[string]bool {
	out := map[string]bool{}
	for _, p := range passages {
		out[p.SourceID] = true
	}
	return out
}

// Measured on a production corpus for "comment prendre une vague et faire un
// take off en surf": of the twenty closest passages, two surf tutorials placed
// sixteen while each finance interview placed exactly one — the signature of a
// passing mention.
func TestKeepCohesiveDropsThePassingMention(t *testing.T) {
	got := KeepCohesive([]Passage{
		passage("debuter-en-surf", 0.221, 6), passage("debuter-en-surf", 0.269, 6),
		passage("debuter-en-surf", 0.270, 6), passage("prendre-une-vague", 0.217, 5),
		passage("prendre-une-vague", 0.272, 5),
		passage("credit-lombard", 0.355, 730),
		passage("interview-finance", 0.363, 1440),
	}, DefaultCohesion)

	kept := keptSources(got)
	for _, want := range []string{"debuter-en-surf", "prendre-une-vague"} {
		if !kept[want] {
			t.Errorf("dropped %q, which placed several passages", want)
		}
	}
	for _, unwanted := range []string{"credit-lombard", "interview-finance"} {
		if kept[unwanted] {
			t.Errorf("kept %q, which placed one passage out of hundreds", unwanted)
		}
	}
}

// Counting passages reads a document's length, not its subject. A three-chunk
// article placing one is making as strong a claim as a thousand-chunk video
// placing three hundred.
func TestKeepCohesiveKeepsAShortDocumentOnItsShare(t *testing.T) {
	got := KeepCohesive([]Passage{
		passage("redevances", 0.31, 82), passage("redevances", 0.34, 82),
		passage("droits-seigneuriaux", 0.33, 3),
		passage("video-fleuve", 0.36, 1440),
	}, DefaultCohesion)

	kept := keptSources(got)
	if !kept["droits-seigneuriaux"] {
		t.Error("dropped a three-chunk document that placed a third of its content")
	}
	if kept["video-fleuve"] {
		t.Error("kept a 1440-chunk document on a single passage")
	}
}

// A lone passage far ahead of everything else is answering the question, not
// mentioning it.
func TestKeepCohesiveKeepsALoneLeader(t *testing.T) {
	got := KeepCohesive([]Passage{
		passage("tutoriel", 0.40, 6), passage("tutoriel", 0.44, 6),
		passage("note-precise", 0.12, 1),
	}, DefaultCohesion)

	if !keptSources(got)["note-precise"] {
		t.Error("dropped a lone passage sitting far closer than any cohesive document")
	}
}

// An over-eager filter recreates the empty-context answer the rule exists to
// prevent.
func TestKeepCohesiveNeverReturnsNothing(t *testing.T) {
	in := []Passage{passage("a", 0.5, 900), passage("b", 0.6, 900)}
	if got := KeepCohesive(in, DefaultCohesion); len(got) == 0 {
		t.Error("returned nothing for a non-empty input")
	}
}

func TestKeepCohesiveKeepsEverythingWithoutDistances(t *testing.T) {
	in := []Passage{{SourceID: "a"}, {SourceID: "b"}}
	if got := KeepCohesive(in, DefaultCohesion); len(got) != len(in) {
		t.Errorf("got %d passages, want all %d when no distance is known", len(got), len(in))
	}
}
