package synthesis

import (
	"strings"
	"testing"
)

func mkChunk(id, content string) chunk {
	return chunk{ID: id, Content: content}
}

// wordsForTokens builds content whose estimateTokens is ~target tokens.
func wordsForTokens(target int) string {
	words := int(float64(target) / wordsToTokenRatio)
	return strings.Repeat("w ", words)
}

func TestPartitionChunks_RespectsBudgetAndOrder(t *testing.T) {
	// 4 chunks of ~600 tokens each; budget 1000 → batches of at most 1 pair.
	chunks := []chunk{
		mkChunk("a", wordsForTokens(600)),
		mkChunk("b", wordsForTokens(600)),
		mkChunk("c", wordsForTokens(600)),
		mkChunk("d", wordsForTokens(600)),
	}
	batches := partitionChunks(chunks, 1000)

	if len(batches) != 4 {
		t.Fatalf("expected 4 single-chunk batches (600+600>1000), got %d", len(batches))
	}
	// Order preserved and every batch within budget.
	var order []string
	for _, b := range batches {
		if estimateTokens(b) > 1000 {
			t.Errorf("batch over budget: %d tokens", estimateTokens(b))
		}
		for _, c := range b {
			order = append(order, c.ID)
		}
	}
	want := []string{"a", "b", "c", "d"}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order not preserved: got %v want %v", order, want)
		}
	}
}

func TestPartitionChunks_PacksMultiplePerBatch(t *testing.T) {
	// 4 chunks of ~200 tokens; budget 1000 → they pack (200*4=800 ≤ 1000).
	chunks := []chunk{
		mkChunk("a", wordsForTokens(200)),
		mkChunk("b", wordsForTokens(200)),
		mkChunk("c", wordsForTokens(200)),
		mkChunk("d", wordsForTokens(200)),
	}
	batches := partitionChunks(chunks, 1000)
	if len(batches) != 1 {
		t.Fatalf("expected 1 packed batch, got %d", len(batches))
	}
	if len(batches[0]) != 4 {
		t.Errorf("expected 4 chunks in the batch, got %d", len(batches[0]))
	}
}

func TestPartitionChunks_OversizedChunkIsolated(t *testing.T) {
	chunks := []chunk{
		mkChunk("a", wordsForTokens(100)),
		mkChunk("big", wordsForTokens(5000)), // larger than budget
		mkChunk("c", wordsForTokens(100)),
	}
	batches := partitionChunks(chunks, 1000)
	// "big" must sit alone in its own batch, not merged with neighbours.
	var bigBatch []chunk
	for _, b := range batches {
		for _, c := range b {
			if c.ID == "big" {
				bigBatch = b
			}
		}
	}
	if len(bigBatch) != 1 {
		t.Errorf("oversized chunk should be isolated, got batch of %d", len(bigBatch))
	}
}

func TestPartitionChunks_Empty(t *testing.T) {
	if partitionChunks(nil, 1000) != nil {
		t.Error("expected nil for empty input")
	}
}
