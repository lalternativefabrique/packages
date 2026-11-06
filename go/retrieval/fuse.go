package retrieval

import "sort"

// Candidate is one document with whatever the two searches said about it. Both
// scores are optional: a document may be found by only one of them.
type Candidate struct {
	ID string
	// Similarity is the dense score, typically cosine in 0..1.
	Similarity float64
	// HasSimilarity is false when the dense search did not return this document.
	HasSimilarity bool
	// LexicalRank is the raw score of a full-text match, on any scale — it is
	// normalised against the best of the set.
	LexicalRank float64
	// HasLexical is false when the lexical search did not return this document.
	HasLexical bool
}

// Scored is a Candidate with its two normalised scores and their weighted sum.
type Scored struct {
	ID       string
	Combined float64
	Dense    float64
	Lexical  float64
}

// Weights splits the fused score between the two signals.
type Weights struct {
	Dense   float64
	Lexical float64
}

// EqualWeights weighs both signals the same.
func EqualWeights() Weights { return Weights{Dense: 0.5, Lexical: 0.5} }

// Fuse ranks candidates on normalised scores rather than on rank.
//
// Reciprocal rank fusion — the usual answer — assumes every list is of
// comparable quality, because it only reads position. Rank 1 of a list that
// found nothing good weighs exactly as much as rank 1 of a list that nailed it.
// Measured in production, the best and worst of twenty RRF candidates sat
// within 12% of each other, so its output orders results but cannot say whether
// any of them are worth reading.
//
// Scoring both signals into 0..1 makes them comparable: a weak match scores low
// and loses on its own merit. minScore then drops what neither signal supports,
// which is the "no match" answer rank fusion cannot give.
func Fuse(candidates []Candidate, d Distribution, g Gate, w Weights, minScore float64) []Scored {
	if len(candidates) == 0 {
		return nil
	}

	// ts_rank and its equivalents have no upper bound, so the lexical side is
	// normalised against the best hit of this result set.
	bestLexical := 0.0
	for _, c := range candidates {
		if c.HasLexical && c.LexicalRank > bestLexical {
			bestLexical = c.LexicalRank
		}
	}

	out := make([]Scored, 0, len(candidates))
	for _, c := range candidates {
		var dense, lexical float64
		if c.HasSimilarity && g.Admits(c.Similarity, d) {
			dense = g.DenseScore(c.Similarity, d)
		}
		if c.HasLexical && bestLexical > 0 {
			lexical = c.LexicalRank / bestLexical
		}

		combined := dense*w.Dense + lexical*w.Lexical
		if combined < minScore {
			continue
		}
		out = append(out, Scored{ID: c.ID, Combined: combined, Dense: dense, Lexical: lexical})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Combined != out[j].Combined {
			return out[i].Combined > out[j].Combined
		}
		return out[i].ID > out[j].ID
	})
	return out
}
