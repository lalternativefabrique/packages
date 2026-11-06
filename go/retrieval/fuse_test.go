package retrieval

import "testing"

func dense(id string, sim float64) Candidate {
	return Candidate{ID: id, Similarity: sim, HasSimilarity: true}
}

func lexical(id string, rank float64) Candidate {
	return Candidate{ID: id, LexicalRank: rank, HasLexical: true}
}

// The behaviour a lexical-only search cannot deliver: a query whose words appear
// in no document still reaches the semantically closest one.
func TestFuseKeepsADenseOnlyMatch(t *testing.T) {
	d := measured["féodalisme"].dist
	got := Fuse([]Candidate{
		dense("feudal-mode", 0.510),
		dense("cleopatra", 0.350),
	}, d, DefaultGate(), EqualWeights(), 0.15)

	if len(got) != 1 {
		t.Fatalf("got %d results, want only the close one: %+v", len(got), got)
	}
	if got[0].ID != "feudal-mode" {
		t.Errorf("top = %q, want feudal-mode", got[0].ID)
	}
	if got[0].Lexical != 0 {
		t.Errorf("Lexical = %v, want 0 with no lexical hit", got[0].Lexical)
	}
}

// The objection that keeps dense retrieval out of human-facing lists: a query
// matching nothing must return nothing, not its least-bad neighbours.
func TestFuseReturnsNothingWhenNothingMatches(t *testing.T) {
	c := measured["nothing matches"]
	got := Fuse([]Candidate{
		dense("k2-climb", c.best),
		dense("something-else", 0.376),
	}, c.dist, DefaultGate(), EqualWeights(), 0.15)

	if len(got) != 0 {
		t.Errorf("got %d results, want none: %+v", len(got), got)
	}
}

// Adding dense retrieval must not regress the exact-term case a lexical search
// already handled.
func TestFuseKeepsALexicalOnlyMatch(t *testing.T) {
	got := Fuse([]Candidate{
		lexical("exact-title", 0.85),
		lexical("weaker", 0.20),
	}, measured["marketing"].dist, DefaultGate(), EqualWeights(), 0.15)

	if len(got) == 0 {
		t.Fatal("a lexical match must survive")
	}
	if got[0].ID != "exact-title" {
		t.Errorf("top = %q, want exact-title", got[0].ID)
	}
	if got[0].Lexical != 1 {
		t.Errorf("Lexical = %v, want 1 for the best hit of the set", got[0].Lexical)
	}
}

// A document both searches found should outrank one only a single search did.
func TestFuseRewardsAgreement(t *testing.T) {
	d := measured["vin degustation"].dist
	both := Candidate{ID: "both", Similarity: 0.649, HasSimilarity: true, LexicalRank: 1.0, HasLexical: true}
	onlyLex := lexical("lexical-only", 1.0)

	got := Fuse([]Candidate{onlyLex, both}, d, DefaultGate(), EqualWeights(), 0.0)
	if len(got) < 2 {
		t.Fatalf("expected both candidates, got %+v", got)
	}
	if got[0].ID != "both" {
		t.Errorf("top = %q, want the document both searches agreed on", got[0].ID)
	}
}

func TestFuseIsDeterministicOnTies(t *testing.T) {
	d := measured["féodalisme"].dist
	in := []Candidate{lexical("a", 1.0), lexical("b", 1.0)}
	first := Fuse(in, d, DefaultGate(), EqualWeights(), 0)
	for i := 0; i < 20; i++ {
		again := Fuse(in, d, DefaultGate(), EqualWeights(), 0)
		if again[0].ID != first[0].ID {
			t.Fatal("tie order is not stable across calls")
		}
	}
}
