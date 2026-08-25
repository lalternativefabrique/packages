// Package retrieval decides which candidates of a similarity search are
// relevant, and how to fuse a dense score with a lexical one.
//
// It holds no storage: callers bring their own candidates, from pgvector, from
// another vector store, or from a fixture in a test.
//
// # Why a fixed threshold cannot work
//
// The instinct is to keep whatever scores above some number. Measured on a
// production corpus of 948 summary embeddings (bge-m3):
//
//	query               max     p99     mean    stddev   relevant docs
//	"féodalisme"        0.510   0.372   0.279   0.042    5
//	"marketing"         0.501   0.473   0.363   0.041    8
//	"vin degustation"   0.649   0.422   0.326   0.039    3
//	a query matching    0.424   0.376   0.288   0.034    0
//	nothing at all
//
// "féodalisme" and the nothing-query peak 0.09 apart, yet one has five relevant
// documents and the other none. No single cutoff separates them: 0.65 returned
// nothing for a user holding five feudalism sources, and lowering it far enough
// to reach them let unrelated documents back in.
//
// The scale moves with the query. A one-word query compared against a whole
// summary tops out far lower than a paragraph does, and two texts in the same
// language always share a floor of similarity — an article about Cleopatra
// still scores 0.350 against "féodalisme". There is no natural zero.
//
// # What does separate them
//
// The gap to the rest of the field. A relevant document stands out from the
// corpus; when nothing matches, every candidate bunches together. So the floor
// is computed per query as mean + z*stddev over that query's own distribution.
//
// That test alone is not enough. When nothing matches, the spread collapses and
// the least-bad document looks unusually detached — a K2 climbing story cleared
// 3.5 sigma on a nonsense query. An absolute minimum sits underneath: a
// candidate must both stand out AND be close enough in absolute terms.
package retrieval

import "math"

// Defaults measured on the corpus described in the package comment. They are
// starting points, not derived constants: a different embedding model or corpus
// shifts them, and both are meant to be tuned against real queries.
const (
	// DefaultZThreshold is how many standard deviations past the corpus mean a
	// candidate must sit.
	DefaultZThreshold = 2.0
	// DefaultSimilarityFloor is the absolute minimum, between the measured
	// nothing-query peak (0.424) and the weakest genuinely relevant document
	// (0.460).
	DefaultSimilarityFloor = 0.44
)

// Distribution describes how a query scores against a whole corpus. Computing
// it over the full corpus rather than the retrieved top-N matters: the top-N is
// above its own mean by construction, which makes the test partly circular and
// ties the floor to an arbitrary limit.
type Distribution struct {
	Mean   float64
	StdDev float64
	Count  int
}

// DescribeSimilarities builds a Distribution from every similarity a query
// produced over the corpus.
func DescribeSimilarities(similarities []float64) Distribution {
	n := len(similarities)
	if n == 0 {
		return Distribution{}
	}

	sum := 0.0
	for _, s := range similarities {
		sum += s
	}
	mean := sum / float64(n)

	if n == 1 {
		return Distribution{Mean: mean, StdDev: 0, Count: 1}
	}

	variance := 0.0
	for _, s := range similarities {
		d := s - mean
		variance += d * d
	}
	// Sample standard deviation, matching Postgres stddev().
	variance /= float64(n - 1)

	return Distribution{Mean: mean, StdDev: math.Sqrt(variance), Count: n}
}

// Gate turns a distribution into the two tests a candidate must pass.
type Gate struct {
	// ZThreshold is how far past the mean, in standard deviations, a candidate
	// must sit. Zero disables the relative test — useful for a corpus too small
	// to have a meaningful spread.
	ZThreshold float64
	// SimilarityFloor is the absolute minimum similarity, whatever the
	// distribution says.
	SimilarityFloor float64
}

// DefaultGate returns the measured defaults.
func DefaultGate() Gate {
	return Gate{ZThreshold: DefaultZThreshold, SimilarityFloor: DefaultSimilarityFloor}
}

// Floor is the similarity a candidate must reach under this distribution.
//
// A corpus of fewer than two documents has no spread to measure, so the
// relative test says nothing and only the absolute floor applies.
func (g Gate) Floor(d Distribution) float64 {
	if d.Count < 2 || d.StdDev <= 0 || g.ZThreshold <= 0 {
		return g.SimilarityFloor
	}
	return math.Max(d.Mean+g.ZThreshold*d.StdDev, g.SimilarityFloor)
}

// Admits reports whether a similarity clears both tests.
func (g Gate) Admits(similarity float64, d Distribution) bool {
	return similarity >= g.Floor(d)
}

// DenseScore maps a similarity onto 0..1 by how far past the threshold it sits,
// saturating two standard deviations beyond it.
//
// Scaling against the best candidate instead would hand 1.0 to whatever topped
// a weak field, which is how a K2 climbing story led a search for a query
// matching nothing.
func (g Gate) DenseScore(similarity float64, d Distribution) float64 {
	if d.Count < 2 || d.StdDev <= 0 {
		// No spread to speak of: report presence, not degree.
		if similarity >= g.SimilarityFloor {
			return 1
		}
		return 0
	}
	z := (similarity - d.Mean) / d.StdDev
	return clamp01((z - g.ZThreshold) / 2.0)
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
