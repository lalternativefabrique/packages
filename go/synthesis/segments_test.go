package synthesis

import "testing"

func TestGroupSegmentsEmpty(t *testing.T) {
	if got := groupSegments(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestGroupSegmentsMergesUnderTarget(t *testing.T) {
	in := []Segment{
		{Text: "a", Start: 0, End: 10},
		{Text: "b", Start: 10, End: 20},
		{Text: "c", Start: 20, End: 30},
	}
	got := groupSegments(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 group (under 60s target), got %d", len(got))
	}
	if got[0].Content != "a b c" {
		t.Fatalf("expected merged text, got %q", got[0].Content)
	}
	if got[0].StartSeconds != 0 || got[0].EndSeconds != 30 {
		t.Fatalf("unexpected bounds: %+v", got[0])
	}
}

func TestGroupSegmentsSplitsOnTargetDuration(t *testing.T) {
	in := []Segment{
		{Text: "a", Start: 0, End: 60},
		{Text: "b", Start: 60, End: 70},
	}
	got := groupSegments(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 groups (first group reached target duration), got %d", len(got))
	}
}

func TestGroupSegmentsSplitsOnGap(t *testing.T) {
	in := []Segment{
		{Text: "a", Start: 0, End: 10},
		{Text: "b", Start: 100, End: 110}, // 90s gap
	}
	got := groupSegments(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 groups due to silence gap, got %d", len(got))
	}
}

func TestChunkCountFollowsGrouping(t *testing.T) {
	in := []Segment{{Text: "a", Start: 0, End: 10}, {Text: "b", Start: 100, End: 110}}
	if got := ChunkCount(in); got != 2 {
		t.Fatalf("ChunkCount = %d, want 2", got)
	}
}
