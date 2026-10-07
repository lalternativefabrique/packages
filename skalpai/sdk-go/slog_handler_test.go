package skalpai

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	logembedded "go.opentelemetry.io/otel/log/embedded"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// captureProcessor stores every emitted log record for assertions.
type captureProcessor struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (c *captureProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, *r)
	return nil
}

func (c *captureProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool {
	return true
}

func (c *captureProcessor) Shutdown(context.Context) error   { return nil }
func (c *captureProcessor) ForceFlush(context.Context) error { return nil }

func (c *captureProcessor) snapshot() []sdklog.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]sdklog.Record, len(c.records))
	copy(out, c.records)
	return out
}

// installCaptureLoggerProvider replaces the global OTEL logger provider with
// one backed by a captureProcessor. Returns the processor and a teardown.
func installCaptureLoggerProvider(t *testing.T) *captureProcessor {
	t.Helper()
	cap := &captureProcessor{}
	prov := sdklog.NewLoggerProvider(sdklog.WithProcessor(cap))
	prev := global.GetLoggerProvider()
	global.SetLoggerProvider(prov)
	t.Cleanup(func() {
		_ = prov.Shutdown(context.Background())
		global.SetLoggerProvider(prev)
	})
	return cap
}

func findAttr(rec sdklog.Record, key string) (attribute.Value, bool) {
	var (
		v     attribute.Value
		found bool
	)
	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		if string(kv.Key) == key {
			v = kv.Value
			found = true
			return false
		}
		return true
	})
	return v, found
}

func TestNewSlogHandler_EmitsBasicRecord(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	logger := slog.New(NewSlogHandler("test-svc"))

	logger.Info("hello", "user_id", "u123", "count", 42)

	recs := cap.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	if got := r.Body().AsString(); got != "hello" {
		t.Errorf("body = %q, want %q", got, "hello")
	}
	if r.Severity() != otellog.SeverityInfo {
		t.Errorf("severity = %v, want Info", r.Severity())
	}
	if r.SeverityText() != "INFO" {
		t.Errorf("severityText = %q, want INFO", r.SeverityText())
	}
	if v, ok := findAttr(r, "user_id"); !ok || v.AsString() != "u123" {
		t.Errorf("user_id attr missing or wrong: %v ok=%v", v, ok)
	}
	if v, ok := findAttr(r, "count"); !ok || v.AsInt64() != 42 {
		t.Errorf("count attr missing or wrong: %v ok=%v", v, ok)
	}
}

func TestNewSlogHandler_SeverityMapping(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	logger := slog.New(NewSlogHandler("test", WithMinLevel(slog.LevelDebug)))

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	got := cap.snapshot()
	want := []otellog.Severity{
		otellog.SeverityDebug,
		otellog.SeverityInfo,
		otellog.SeverityWarn,
		otellog.SeverityError,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d records, got %d", len(want), len(got))
	}
	for i, sev := range want {
		if got[i].Severity() != sev {
			t.Errorf("record %d severity = %v, want %v", i, got[i].Severity(), sev)
		}
	}
}

func TestNewSlogHandler_FiltersByMinLevel(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	logger := slog.New(NewSlogHandler("test", WithMinLevel(slog.LevelWarn)))

	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	got := cap.snapshot()
	if len(got) != 2 {
		t.Fatalf("want 2 records (Warn+Error), got %d", len(got))
	}
	if got[0].Body().AsString() != "w" || got[1].Body().AsString() != "e" {
		t.Errorf("unexpected bodies: %v / %v", got[0].Body(), got[1].Body())
	}
}

func TestNewSlogHandler_WithAttrsAndGroups(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	base := slog.New(NewSlogHandler("test"))
	logger := base.With("svc", "auth").WithGroup("http").With("status", 200)

	logger.Info("done", "method", "GET")

	recs := cap.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	r := recs[0]
	cases := map[string]string{
		"svc":         "auth",
		"http.status": "200",
		"http.method": "GET",
	}
	for k, want := range cases {
		v, ok := findAttr(r, k)
		if !ok {
			t.Errorf("attr %q missing", k)
			continue
		}
		switch v.Type() {
		case attribute.STRING:
			if v.AsString() != want {
				t.Errorf("attr %q = %q, want %q", k, v.AsString(), want)
			}
		case attribute.INT64:
			// 200 was emitted as int64 — string compare for convenience.
			if v.AsInt64() != 200 {
				t.Errorf("attr %q = %d, want 200", k, v.AsInt64())
			}
		default:
			t.Errorf("attr %q unexpected kind %v", k, v.Type())
		}
	}
}

func TestNewSlogHandler_AttachesTraceContext(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	logger := slog.New(NewSlogHandler("test"))
	logger.InfoContext(ctx, "with trace")

	recs := cap.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	want := span.SpanContext().TraceID().String()
	v, ok := findAttr(recs[0], "trace_id")
	if !ok || v.AsString() != want {
		t.Errorf("trace_id attr = %v ok=%v, want %s", v, ok, want)
	}
	if _, ok := findAttr(recs[0], "span_id"); !ok {
		t.Errorf("span_id attr missing")
	}
}

func TestNewSlogHandler_NoopWithoutInit(t *testing.T) {
	// Save and restore the global provider so this test does not leak state.
	prev := global.GetLoggerProvider()
	global.SetLoggerProvider(noopLoggerProvider{})
	t.Cleanup(func() { global.SetLoggerProvider(prev) })

	logger := slog.New(NewSlogHandler("test"))
	// Should not panic, should not block.
	done := make(chan struct{})
	go func() {
		logger.Info("dropped")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Info() with noop provider blocked")
	}
}

// noopLoggerProvider returns no-op loggers. Used to assert NewSlogHandler is
// safe even when Init has not been called.
type noopLoggerProvider struct {
	logembedded.LoggerProvider
}

func (noopLoggerProvider) Logger(string, ...otellog.LoggerOption) otellog.Logger {
	return noopLogger{}
}

type noopLogger struct {
	logembedded.Logger
}

func (noopLogger) Emit(context.Context, otellog.Record) {}
func (noopLogger) Enabled(context.Context, otellog.EnabledParameters) bool {
	return false
}

func TestFanout_DispatchesAndIsolatesErrors(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	good1 := slog.NewJSONHandler(&buf1, nil)
	good2 := slog.NewJSONHandler(&buf2, nil)
	bad := errHandler{err: errors.New("boom")}

	h := Fanout(good1, bad, good2)
	logger := slog.New(h)
	logger.Info("hi", "k", "v")

	if buf1.Len() == 0 || buf2.Len() == 0 {
		t.Fatalf("both good handlers should have received output (b1=%d b2=%d)", buf1.Len(), buf2.Len())
	}
}

func TestFanout_PropagatesAttrsAndGroups(t *testing.T) {
	cap := installCaptureLoggerProvider(t)
	var buf bytes.Buffer
	stdout := slog.NewJSONHandler(&buf, nil)
	otlp := NewSlogHandler("test")

	logger := slog.New(Fanout(stdout, otlp)).With("svc", "core").WithGroup("req")
	logger.Info("done", "id", "abc")

	if buf.Len() == 0 {
		t.Errorf("stdout handler received no output")
	}
	recs := cap.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 OTLP record, got %d", len(recs))
	}
	if v, ok := findAttr(recs[0], "svc"); !ok || v.AsString() != "core" {
		t.Errorf("svc attr missing or wrong: %v ok=%v", v, ok)
	}
	if v, ok := findAttr(recs[0], "req.id"); !ok || v.AsString() != "abc" {
		t.Errorf("req.id attr missing or wrong: %v ok=%v", v, ok)
	}
}

type errHandler struct{ err error }

func (e errHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (e errHandler) Handle(context.Context, slog.Record) error { return e.err }
func (e errHandler) WithAttrs([]slog.Attr) slog.Handler        { return e }
func (e errHandler) WithGroup(string) slog.Handler             { return e }
