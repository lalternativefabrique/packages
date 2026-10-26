package synthesis

import (
	"fmt"
	"sort"
)

const (
	defaultTargetDurationSeconds = 60.0
	defaultMaxGapSeconds         = 5.0
)

// groupSegments merges consecutive transcript segments into thematic blocks.
//
// Segments are merged until cumulative duration reaches targetDurationSeconds
// or a silence gap > maxGapSeconds is detected between consecutive segments.
//
func groupSegments(segments []Segment) []chunk {
	if len(segments) == 0 {
		return nil
	}

	sorted := make([]Segment, len(segments))
	copy(sorted, segments)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Start < sorted[j].Start
	})

	groups := make([]chunk, 0)
	currentTexts := []string{sorted[0].Text}
	groupStart := sorted[0].Start
	groupEnd := sorted[0].End

	flush := func() {
		joined := join(currentTexts)
		groups = append(groups, chunk{
			ID:           fmt.Sprintf("group-%d", len(groups)),
			Content:      joined,
			StartSeconds: groupStart,
			EndSeconds:   groupEnd,
		})
	}

	for i := 1; i < len(sorted); i++ {
		seg := sorted[i]
		gap := seg.Start - groupEnd
		duration := groupEnd - groupStart

		if duration >= defaultTargetDurationSeconds || gap > defaultMaxGapSeconds {
			flush()
			currentTexts = []string{seg.Text}
			groupStart = seg.Start
			groupEnd = seg.End
		} else {
			currentTexts = append(currentTexts, seg.Text)
			groupEnd = seg.End
		}
	}
	flush()
	return groups
}

func join(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}
