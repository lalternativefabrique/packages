package prom

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMeter_CounterAndHistogram(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := New(reg)

	m.Counter("requests_total", "kind", "ping", "status", "ok")
	m.Counter("requests_total", "kind", "ping", "status", "ok")
	m.Counter("requests_total", "kind", "ping", "status", "error")

	m.Observe("request_seconds", 0.005, "kind", "ping")
	m.Observe("request_seconds", 0.5, "kind", "ping")

	// Use testutil to read back values.
	want := `
# HELP requests_total
# TYPE requests_total counter
requests_total{kind="ping",status="error"} 1
requests_total{kind="ping",status="ok"} 2
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(want), "requests_total"))

	// The histogram exists and one time series (kind=ping) is registered.
	assert.Equal(t, 1, testutil.CollectAndCount(m.histograms["request_seconds"]))
}
