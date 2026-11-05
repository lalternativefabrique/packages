package botguard

// Counters is what a guard has seen since it started.
type Counters struct {
	ChallengesIssued uint64            `json:"challenges_issued"`
	ChallengesSolved uint64            `json:"challenges_solved"`
	RefusedChallenge uint64            `json:"refused_challenge"`
	RefusedCeiling   uint64            `json:"refused_ceiling"`
	FailOpen         uint64            `json:"fail_open"`
	Rejected         map[string]uint64 `json:"rejected,omitempty"`
}

// Counters counts verdicts in observe mode too: RefusedChallenge is what
// enforcement would have refused.
func (g *Guard) Counters() Counters {
	s := g.stats
	c := Counters{
		ChallengesIssued: s.challengesIssued.Load(),
		ChallengesSolved: s.challengesSolved.Load(),
		RefusedChallenge: s.refusedChallenge.Load(),
		RefusedCeiling:   s.refusedCeiling.Load(),
		FailOpen:         s.failOpen.Load(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rejected) > 0 {
		c.Rejected = make(map[string]uint64, len(s.rejected))
		for k, v := range s.rejected {
			c.Rejected[k] = v
		}
	}
	return c
}

// Since is what happened between an earlier reading and this one.
func (c Counters) Since(earlier Counters) Counters {
	d := Counters{
		ChallengesIssued: c.ChallengesIssued - earlier.ChallengesIssued,
		ChallengesSolved: c.ChallengesSolved - earlier.ChallengesSolved,
		RefusedChallenge: c.RefusedChallenge - earlier.RefusedChallenge,
		RefusedCeiling:   c.RefusedCeiling - earlier.RefusedCeiling,
		FailOpen:         c.FailOpen - earlier.FailOpen,
	}
	for k, v := range c.Rejected {
		if n := v - earlier.Rejected[k]; n > 0 {
			if d.Rejected == nil {
				d.Rejected = map[string]uint64{}
			}
			d.Rejected[k] = n
		}
	}
	return d
}
