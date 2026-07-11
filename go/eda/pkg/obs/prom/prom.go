// Package prom provides a Prometheus-backed Meter for pkg/obs.
//
// It is a thin, opt-in adapter that translates obs.Meter calls
// (Counter / Observe with string tag pairs) into Prometheus counter and
// histogram vectors.
//
// Importing this package pulls in github.com/prometheus/client_golang.
// The core obs package stays dependency-free.
package prom

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lalternative/packages/go/eda/pkg/obs"
)

// Meter implements obs.Meter on top of a Prometheus registry. Counters
// and histograms are created lazily on first observation and cached.
//
// The set of label keys is captured the first time a metric is seen and
// must be stable across subsequent observations of the same metric name —
// this matches Prometheus's own contract.
type Meter struct {
	reg   prometheus.Registerer
	buckets []float64

	mu         sync.Mutex
	counters   map[string]*prometheus.CounterVec
	histograms map[string]*prometheus.HistogramVec
	labelKeys  map[string][]string
}

// Option configures a Meter.
type Option func(*Meter)

// WithBuckets overrides the default histogram buckets.
func WithBuckets(b []float64) Option { return func(m *Meter) { m.buckets = b } }

// New wires a Meter to the given Prometheus registerer.
func New(reg prometheus.Registerer, opts ...Option) *Meter {
	m := &Meter{
		reg:        reg,
		buckets:    prometheus.DefBuckets,
		counters:   make(map[string]*prometheus.CounterVec),
		histograms: make(map[string]*prometheus.HistogramVec),
		labelKeys:  make(map[string][]string),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Counter records one increment of the named counter with the given
// label pairs. Tags follow the same key,value,... convention as obs.Meter.
func (m *Meter) Counter(name string, tags ...string) {
	keys, values := splitPairs(tags)
	cv := m.counterVec(name, keys)
	if cv != nil {
		cv.WithLabelValues(values...).Inc()
	}
}

// Observe records one observation on the named histogram.
func (m *Meter) Observe(name string, value float64, tags ...string) {
	keys, values := splitPairs(tags)
	hv := m.histogramVec(name, keys)
	if hv != nil {
		hv.WithLabelValues(values...).Observe(value)
	}
}

func (m *Meter) counterVec(name string, keys []string) *prometheus.CounterVec {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cv, ok := m.counters[name]; ok {
		return cv
	}
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name}, keys)
	if err := m.reg.Register(cv); err != nil {
		// Already registered by something else — try to recover the
		// existing collector.
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(*prometheus.CounterVec); ok {
				cv = existing
			} else {
				return nil
			}
		} else {
			return nil
		}
	}
	m.counters[name] = cv
	m.labelKeys[name] = keys
	return cv
}

func (m *Meter) histogramVec(name string, keys []string) *prometheus.HistogramVec {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hv, ok := m.histograms[name]; ok {
		return hv
	}
	hv := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Buckets: m.buckets}, keys)
	if err := m.reg.Register(hv); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(*prometheus.HistogramVec); ok {
				hv = existing
			} else {
				return nil
			}
		} else {
			return nil
		}
	}
	m.histograms[name] = hv
	m.labelKeys[name] = keys
	return hv
}

func splitPairs(tags []string) (keys, values []string) {
	n := len(tags) / 2
	keys = make([]string, n)
	values = make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = tags[2*i]
		values[i] = tags[2*i+1]
	}
	return keys, values
}

// Compile-time check that *Meter satisfies obs.Meter.
var _ obs.Meter = (*Meter)(nil)
