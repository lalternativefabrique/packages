package botguard

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

type stats struct {
	challengesIssued atomic.Uint64
	challengesSolved atomic.Uint64
	refusedChallenge atomic.Uint64
	refusedCeiling   atomic.Uint64
	failOpen         atomic.Uint64

	mu       sync.Mutex
	rejected map[string]uint64
}

func (s *stats) reject(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rejected == nil {
		s.rejected = map[string]uint64{}
	}
	s.rejected[reason]++
}

// MetricsHandler serves the guard's counters in the Prometheus text format.
func (g *Guard) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		g.WriteMetrics(w)
	})
}

func (g *Guard) WriteMetrics(w io.Writer) {
	s := g.stats
	aud := g.cfg.Audience
	counter := func(name string, v uint64) {
		fmt.Fprintf(w, "# TYPE botguard_%s counter\nbotguard_%s{audience=%q} %d\n", name, name, aud, v)
	}
	counter("challenges_issued_total", s.challengesIssued.Load())
	counter("challenges_solved_total", s.challengesSolved.Load())
	counter("fail_open_total", s.failOpen.Load())

	fmt.Fprintf(w, "# TYPE botguard_refused_total counter\n")
	fmt.Fprintf(w, "botguard_refused_total{audience=%q,status=\"428\"} %d\n", aud, s.refusedChallenge.Load())
	fmt.Fprintf(w, "botguard_refused_total{audience=%q,status=\"429\"} %d\n", aud, s.refusedCeiling.Load())

	s.mu.Lock()
	reasons := make([]string, 0, len(s.rejected))
	for r := range s.rejected {
		reasons = append(reasons, r)
	}
	sort.Strings(reasons)
	fmt.Fprintf(w, "# TYPE botguard_rejected_total counter\n")
	for _, r := range reasons {
		fmt.Fprintf(w, "botguard_rejected_total{audience=%q,reason=%q} %d\n", aud, r, s.rejected[r])
	}
	s.mu.Unlock()
}
