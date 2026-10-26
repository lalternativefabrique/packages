package synthesis

// partitionChunks splits chunks into ordered batches whose estimated token size
// stays under maxTokensPerBatch, so each batch fits the model context window in
// the map phase of hierarchical synthesis. Order is preserved (batch N precedes
// batch N+1) so a global, contiguous [sN] reference numbering can be derived
// from the batch offsets — which is what keeps citations traceable to the right
// source chunk after map-reduce.
//
// A single chunk larger than the budget is emitted in its own batch (best
// effort): the model may still reject it, but that is a degenerate case (one
// huge, un-splittable segment) better surfaced as an error than silently
// dropped. maxTokensPerBatch <= 0 returns a single batch (caller decides).
func partitionChunks(chunks []chunk, maxTokensPerBatch int) [][]chunk {
	if len(chunks) == 0 {
		return nil
	}
	if maxTokensPerBatch <= 0 {
		return [][]chunk{chunks}
	}

	var batches [][]chunk
	var current []chunk
	currentTokens := 0

	for _, c := range chunks {
		chunkTokens := estimateTokens([]chunk{c})

		// Flush the current batch before adding a chunk that would overflow it,
		// unless the batch is empty (a lone oversized chunk goes on its own).
		if len(current) > 0 && currentTokens+chunkTokens > maxTokensPerBatch {
			batches = append(batches, current)
			current = nil
			currentTokens = 0
		}

		current = append(current, c)
		currentTokens += chunkTokens
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}
