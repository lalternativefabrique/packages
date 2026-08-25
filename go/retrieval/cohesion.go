package retrieval

// Passage is one retrieved chunk seen through the document it belongs to.
type Passage struct {
	ID       string
	SourceID string
	// Distance is the dense distance to the query, lower being closer. Nil when
	// the search returned no vector for this row.
	Distance *float64
	// SourceChunks is how many chunks the whole document holds, 0 when unknown.
	SourceChunks int
}

// Cohesion decides which documents earn their place among retrieved passages.
type Cohesion struct {
	// MinChunks is how many passages a document must place to count as being
	// about the topic rather than mentioning it.
	MinChunks int
	// MinShare is the fraction of its own chunks a document may place instead of
	// reaching MinChunks.
	MinShare float64
	// LeadMargin lets a lone passage survive when it sits this much closer than
	// the best passage of any cohesive document.
	LeadMargin float64
}

// DefaultCohesion holds values measured on a production corpus.
//
// Passage-level distances do not separate topic from mention: on a real
// question the noise interleaves with the signal (0.305 noise, 0.324 signal,
// 0.370 noise, 0.378 signal), so no cutoff splits them. Grouping by document
// does, because it asks a different question — not "is this passage close?" but
// "is this document about the topic?". Asked about surf technique, two
// tutorials placed sixteen of the twenty closest passages while each finance
// interview placed exactly one, where somebody says "on fait du surf à Nice"
// mid-sentence. A passage can be close by accident; a document cannot.
//
// MinShare exists because a raw count reads length, not subject. Asked about
// medieval labour against six sources holding 3 to 82 chunks, a three-chunk
// article had to place two thirds of everything it contains to qualify, where a
// 1440-chunk video needs 0.1% of its own. Four relevant sources were dropped as
// passing mentions and the answer was written from general knowledge instead.
var DefaultCohesion = Cohesion{
	MinChunks:  2,
	MinShare:   0.25,
	LeadMargin: 0.10,
}

// KeepCohesive drops passages whose document placed too few among the
// candidates, which is what a passing mention looks like.
//
// Never returns an empty slice for a non-empty input: an over-eager filter
// recreates the empty-context answer this rule exists to prevent.
func KeepCohesive(passages []Passage, cfg Cohesion) []Passage {
	if len(passages) == 0 || cfg.MinChunks <= 1 {
		return passages
	}

	placed := make(map[string]int, len(passages))
	total := make(map[string]int, len(passages))
	withDistance := 0
	for _, p := range passages {
		placed[p.SourceID]++
		if p.SourceChunks > total[p.SourceID] {
			total[p.SourceID] = p.SourceChunks
		}
		if p.Distance != nil {
			withDistance++
		}
	}
	if withDistance == 0 {
		return passages
	}

	cohesive := func(sourceID string) bool {
		if placed[sourceID] >= cfg.MinChunks {
			return true
		}
		if cfg.MinShare <= 0 || total[sourceID] <= 0 {
			return false
		}
		return float64(placed[sourceID])/float64(total[sourceID]) >= cfg.MinShare
	}

	best := bestDistanceAmongCohesive(passages, cohesive)

	kept := make([]Passage, 0, len(passages))
	for _, p := range passages {
		if cohesive(p.SourceID) || leadsTheField(p, best, cfg.LeadMargin) {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return passages
	}
	return kept
}

func bestDistanceAmongCohesive(passages []Passage, cohesive func(string) bool) *float64 {
	var best *float64
	for _, p := range passages {
		if !cohesive(p.SourceID) || p.Distance == nil {
			continue
		}
		if best == nil || *p.Distance < *best {
			d := *p.Distance
			best = &d
		}
	}
	return best
}

// leadsTheField keeps a lone passage that sits far ahead of everything else: one
// precise note may answer a question no long document covers.
func leadsTheField(p Passage, bestCohesive *float64, margin float64) bool {
	if p.Distance == nil {
		return false
	}
	if bestCohesive == nil {
		return true
	}
	return *p.Distance+margin < *bestCohesive
}
