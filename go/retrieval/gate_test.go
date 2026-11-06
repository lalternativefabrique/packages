package retrieval

import (
	"math"
	"testing"
)

// The distributions measured on a production corpus of 948 summary embeddings
// (bge-m3). Two of these peak 0.09 apart yet one has five relevant documents
// and the other none, which is why no fixed cutoff works.
var measured = map[string]struct {
	best        float64
	dist        Distribution
	hasRelevant bool
}{
	"féodalisme":      {best: 0.510, dist: Distribution{Mean: 0.279, StdDev: 0.042, Count: 948}, hasRelevant: true},
	"marketing":       {best: 0.501, dist: Distribution{Mean: 0.363, StdDev: 0.041, Count: 948}, hasRelevant: true},
	"vin degustation": {best: 0.649, dist: Distribution{Mean: 0.326, StdDev: 0.039, Count: 948}, hasRelevant: true},
	"nothing matches": {best: 0.424, dist: Distribution{Mean: 0.288, StdDev: 0.034, Count: 948}, hasRelevant: false},
}

func TestGateOnMeasuredDistributions(t *testing.T) {
	g := DefaultGate()
	for query, c := range measured {
		if got := g.Admits(c.best, c.dist); got != c.hasRelevant {
			t.Errorf("%q: best=%.3f floor=%.3f admitted=%v, want %v",
				query, c.best, g.Floor(c.dist), got, c.hasRelevant)
		}
	}
}

// The control clears the relative test at 4 sigma — when nothing matches the
// spread collapses and the least-bad document looks detached. Only the absolute
// floor stops it, so a change lowering SimilarityFloor below 0.424 silently
// readmits it.
func TestAbsoluteFloorIsWhatStopsTheControl(t *testing.T) {
	c := measured["nothing matches"]
	z := (c.best - c.dist.Mean) / c.dist.StdDev
	if z < DefaultZThreshold {
		t.Fatalf("control z=%.2f is already below the threshold, this test no longer "+
			"proves the absolute floor is doing the work", z)
	}
	relativeOnly := Gate{ZThreshold: DefaultZThreshold, SimilarityFloor: 0}
	if !relativeOnly.Admits(c.best, c.dist) {
		t.Error("expected the relative test alone to admit the control")
	}
	if DefaultGate().Admits(c.best, c.dist) {
		t.Error("the absolute floor must reject it")
	}
}

// A corpus too small to have a spread says nothing about what is typical, so
// only the absolute floor applies rather than an arbitrary verdict.
func TestGateFallsBackToAbsoluteOnTinyCorpus(t *testing.T) {
	for _, d := range []Distribution{{}, {Mean: 0.5, StdDev: 0, Count: 1}} {
		g := DefaultGate()
		if got := g.Floor(d); got != DefaultSimilarityFloor {
			t.Errorf("Floor(%+v) = %v, want the absolute floor %v", d, got, DefaultSimilarityFloor)
		}
		if !g.Admits(0.9, d) {
			t.Errorf("a clearly close candidate must survive a spreadless corpus")
		}
		if g.Admits(0.1, d) {
			t.Errorf("a distant candidate must not")
		}
	}
}

func TestDescribeSimilaritiesMatchesSampleStdDev(t *testing.T) {
	d := DescribeSimilarities([]float64{0.2, 0.4, 0.6, 0.8})
	if d.Count != 4 {
		t.Errorf("Count = %d, want 4", d.Count)
	}
	if math.Abs(d.Mean-0.5) > 1e-9 {
		t.Errorf("Mean = %v, want 0.5", d.Mean)
	}
	// Sample stddev of that set, as Postgres stddev() reports it.
	if math.Abs(d.StdDev-0.2581988897471611) > 1e-9 {
		t.Errorf("StdDev = %v, want the sample stddev", d.StdDev)
	}
}

// DenseScore reads distance from the corpus, not rank within the result set.
// Scaling against the best candidate would hand 1.0 to whatever topped any
// field, however weak — the two documents below would then score the same
// despite sitting 2 sigma apart.
func TestDenseScoreReadsDistanceNotRank(t *testing.T) {
	d := measured["vin degustation"].dist
	g := DefaultGate()

	best := g.DenseScore(0.649, d)     // ~8 sigma, saturates
	middling := g.DenseScore(0.420, d) // ~2.4 sigma, just past the threshold

	if middling >= best {
		t.Errorf("middling=%.2f best=%.2f — the top of a set must not lift a weak "+
			"candidate to the same score", middling, best)
	}
	if middling <= 0 || middling >= 1 {
		t.Errorf("middling=%.2f, want a partial score rather than a verdict", middling)
	}

	// Two documents that both saturate stay tied: past the band the score says
	// "clearly relevant" and stops discriminating, which the absolute floor and
	// the lexical side are there to complement.
	if g.DenseScore(0.649, d) != g.DenseScore(0.700, d) {
		t.Error("expected the score to saturate at the top of the band")
	}
}
