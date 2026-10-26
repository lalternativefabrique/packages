package synthesis

import "strings"

// synthesisTargets dimensions the requested output to source length.
type synthesisTargets struct {
	SummaryLines [2]int
	Concepts     [2]int
	DevAxes      [2]int
	Words        [2]int
}

// computeSynthesisTargets scales the sheet to the source length in words.
func computeSynthesisTargets(totalWords int) synthesisTargets {
	switch {
	case totalWords < 500:
		return synthesisTargets{SummaryLines: [2]int{3, 5}, Concepts: [2]int{2, 3}, DevAxes: [2]int{1, 2}, Words: [2]int{150, 300}}
	case totalWords < 2000:
		return synthesisTargets{SummaryLines: [2]int{5, 7}, Concepts: [2]int{3, 5}, DevAxes: [2]int{2, 3}, Words: [2]int{300, 500}}
	case totalWords < 5000:
		return synthesisTargets{SummaryLines: [2]int{6, 8}, Concepts: [2]int{4, 6}, DevAxes: [2]int{2, 3}, Words: [2]int{400, 700}}
	default:
		return synthesisTargets{SummaryLines: [2]int{8, 10}, Concepts: [2]int{5, 7}, DevAxes: [2]int{2, 4}, Words: [2]int{600, 1000}}
	}
}

// countWords returns the whitespace-delimited word count of all chunk contents.
func countWords(chunks []chunk) int {
	total := 0
	for _, c := range chunks {
		total += len(strings.Fields(c.Content))
	}
	return total
}

// wordsToTokenRatio is a conservative words→tokens estimate. Measured on a real
// French PDF: 165674 words → 267500 tokens = 1.61. We round up to leave margin,
// since strings.Fields undercounts real tokenization (punctuation, sub-words).
const wordsToTokenRatio = 1.6

// estimateTokens returns a conservative token estimate for a set of chunks,
// used to decide the direct-vs-map-reduce path and to size batches. It is an
// over-estimate on purpose: better to split one batch too many than to overflow
// the model's context window and get a hard 400.
func estimateTokens(chunks []chunk) int {
	return int(float64(countWords(chunks)) * wordsToTokenRatio)
}

// estimateTextTokens is the string-level counterpart of estimateTokens, used to
// size the reduce input built from partial summaries.
func estimateTextTokens(text string) int {
	return int(float64(len(strings.Fields(text))) * wordsToTokenRatio)
}
