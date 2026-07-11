package obs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/logger"
)

type pingCmd struct{ N int }
type pingRes struct{ Pong int }

type fakeMeter struct {
	mu       sync.Mutex
	counters map[string]int
	values   map[string][]float64
}

func newFakeMeter() *fakeMeter {
	return &fakeMeter{counters: map[string]int{}, values: map[string][]float64{}}
}

func (f *fakeMeter) Counter(name string, _ ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counters[name]++
}

func (f *fakeMeter) Observe(name string, v float64, _ ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[name] = append(f.values[name], v)
}

type fakeTracer struct {
	started int32
	ended   int32
	errs    int32
}

type fakeSpan struct{ t *fakeTracer }

func (s *fakeSpan) SetError(error) { atomic.AddInt32(&s.t.errs, 1) }
func (s *fakeSpan) End()           { atomic.AddInt32(&s.t.ended, 1) }

func (f *fakeTracer) StartSpan(ctx context.Context, _ string, _ ...string) (context.Context, Span) {
	atomic.AddInt32(&f.started, 1)
	return ctx, &fakeSpan{t: f}
}

func TestMetricsMiddleware_RecordsOkAndError(t *testing.T) {
	m := newFakeMeter()
	mw := MetricsMiddleware[pingCmd, pingRes](m)

	okHandler := mw(func(_ context.Context, c pingCmd) (pingRes, error) {
		return pingRes{Pong: c.N}, nil
	})
	errHandler := mw(func(_ context.Context, _ pingCmd) (pingRes, error) {
		return pingRes{}, errors.New("boom")
	})

	_, err := okHandler(context.Background(), pingCmd{N: 1})
	require.NoError(t, err)
	_, _ = errHandler(context.Background(), pingCmd{N: 2})

	assert.Equal(t, 2, m.counters["cqrs_handler_total"])
	assert.Len(t, m.values["cqrs_handler_duration_seconds"], 2)
}

func TestTracingMiddleware_StartsAndEndsSpan(t *testing.T) {
	tr := &fakeTracer{}
	mw := TracingMiddleware[pingCmd, pingRes](tr)
	h := mw(func(_ context.Context, _ pingCmd) (pingRes, error) {
		return pingRes{}, errors.New("nope")
	})
	_, err := h(context.Background(), pingCmd{})
	require.Error(t, err)

	assert.Equal(t, int32(1), atomic.LoadInt32(&tr.started))
	assert.Equal(t, int32(1), atomic.LoadInt32(&tr.ended))
	assert.Equal(t, int32(1), atomic.LoadInt32(&tr.errs))
}

func TestLoggingMiddleware_WritesStructuredLog(t *testing.T) {
	buf := &bytes.Buffer{}
	sl := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log := logger.NewSlogLogger(sl)

	mw := LoggingMiddleware[pingCmd, pingRes](log)
	h := mw(func(_ context.Context, c pingCmd) (pingRes, error) { return pingRes{Pong: c.N}, nil })

	_, err := h(context.Background(), pingCmd{N: 7})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, `"msg":"cqrs handler start"`)
	assert.Contains(t, out, `"msg":"cqrs handler ok"`)
	assert.Contains(t, out, `"kind":"obs.pingCmd"`)
}
