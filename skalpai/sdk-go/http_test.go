package skalpai

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

var instrumentSeq atomic.Int64

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return recorder
}

func TestWrapHTTPHandlerEmitsServerSpan(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), HTTPMiddlewareConfig{
		ServiceName:    "test-service",
		RouteExtractor: func(*http.Request) string { return "/v1/items/:id" },
	})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/items/42", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got := spans[0].Name(); got != "GET /v1/items/:id" {
		t.Fatalf("span name = %q, want templated route", got)
	}
	if got := spans[0].SpanKind(); got != trace.SpanKindServer {
		t.Fatalf("span kind = %v, want server", got)
	}
}

func TestWrapHTTPHandlerJoinsUpstreamTrace(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{ServiceName: "test-service"})

	req := httptest.NewRequest(http.MethodGet, "/v1/items", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got := spans[0].SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q, want the caller's trace id", got)
	}
	if got := spans[0].Parent().SpanID().String(); got != "00f067aa0ba902b7" {
		t.Fatalf("parent span id = %q, want the caller's span id", got)
	}
}

func TestWrapHTTPHandlerHandlerSeesSpanContext(t *testing.T) {
	recordSpans(t)

	var seen trace.SpanContext
	h := WrapHTTPHandler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = trace.SpanContextFromContext(r.Context())
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/items", nil))

	if !seen.IsValid() {
		t.Fatal("handler context carries no span: downstream work cannot attach to the trace")
	}
}

func TestWrapHTTPHandlerMarksServerErrors(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/items", nil))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got := spans[0].Status().Code; got != codes.Error {
		t.Fatalf("span status = %v, want error for a 500", got)
	}
}

func TestSkipPathsMatchesExactPathOnly(t *testing.T) {
	skip := SkipPaths("/health", "/metrics")

	for path, want := range map[string]bool{
		"/health":          true,
		"/metrics":         true,
		"/health/detailed": false,
		"/v1/items":        false,
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if got := skip(req); got != want {
			t.Errorf("SkipPaths(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSkipPathsIgnoresQueryString(t *testing.T) {
	skip := SkipPaths("/health")

	if !skip(httptest.NewRequest(http.MethodGet, "/health?verbose=1", nil)) {
		t.Fatal("a query string must not defeat the path match")
	}
}

func TestWrapHTTPHandlerSkipTracingDropsSpan(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{
			ServiceName: "test-service",
			SkipTracing: SkipPaths("/health"),
		})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("got %d spans for a skipped path, want 0", got)
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/items", nil))
	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("got %d spans after a traced path, want 1", got)
	}
}

func TestWrapHTTPHandlerSkipTracingKeepsResponseIntact(t *testing.T) {
	recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("brewing"))
	}), HTTPMiddlewareConfig{
		ServiceName: "test-service",
		SkipTracing: SkipPaths("/health"),
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "brewing" {
		t.Fatalf("body = %q, want brewing", rec.Body.String())
	}
}

func TestWrapHTTPHandlerSkipTracingKeepsMetrics(t *testing.T) {
	recorder := recordSpans(t)

	reader := sdkmetric.NewManualReader()
	previous := otel.GetMeterProvider()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(previous) })

	// Instruments are memoised per service name for the process lifetime, so
	// a name reused across runs (go test -count=2) would hand back
	// instruments still bound to the previous run's collected reader.
	service := fmt.Sprintf("skip-metrics-service-%d", instrumentSeq.Add(1))
	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{ServiceName: service, SkipTracing: SkipPaths("/health")})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("got %d spans, want 0", got)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !hasMetric(collected, "http.server.request.count") {
		t.Fatal("skipping a span must not suppress its request metrics")
	}
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}

func TestWrapHTTPHandlerNilSkipTracingTracesEverything(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("got %d spans, want 1 when SkipTracing is nil", got)
	}
}

func TestWrapHTTPHandlerDisableTracing(t *testing.T) {
	recorder := recordSpans(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{ServiceName: "test-service", DisableTracing: true})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/items", nil))

	if got := len(recorder.Ended()); got != 0 {
		t.Fatalf("got %d spans, want 0 when tracing is disabled", got)
	}
}

func TestStatusClass(t *testing.T) {
	if got := statusClass(201); got != "2xx" {
		t.Fatalf("statusClass(201) = %q, want 2xx", got)
	}
	if got := statusClass(503); got != "5xx" {
		t.Fatalf("statusClass(503) = %q, want 5xx", got)
	}
}

func TestWrapHTTPHandlerPreservesStatusAndBody(t *testing.T) {
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	req := httptest.NewRequest(http.MethodPost, "/v1/items", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want ok", rec.Body.String())
	}
}

func captureAccessLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	return &buf
}

func serveWithSkip(t *testing.T, path string, status int) *bytes.Buffer {
	t.Helper()

	buf := captureAccessLogs(t)
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}), HTTPMiddlewareConfig{
		ServiceName:    "test-service",
		EmitAccessLogs: true,
		SkipAccessLogs: SkipPaths("/health"),
	})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	return buf
}

func TestWrapHTTPHandlerSkipAccessLogsDropsProbeLine(t *testing.T) {
	if out := serveWithSkip(t, "/health", http.StatusOK).String(); out != "" {
		t.Fatalf("expected no access log for a skipped path, got: %s", out)
	}
}

func TestWrapHTTPHandlerSkipAccessLogsKeepsOtherRoutes(t *testing.T) {
	if out := serveWithSkip(t, "/v1/items", http.StatusOK).String(); out == "" {
		t.Fatal("expected an access log for a non-skipped path")
	}
}

func TestWrapHTTPHandlerSkipAccessLogsStillLogsServerErrors(t *testing.T) {
	out := serveWithSkip(t, "/health", http.StatusInternalServerError).String()
	if out == "" {
		t.Fatal("a failing probe must still be logged: that is when its log line carries signal")
	}
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("expected the 5xx to be logged at error level, got: %s", out)
	}
}

func TestWrapHTTPHandlerSkipAccessLogsLeavesSpanUntouched(t *testing.T) {
	recorder := recordSpans(t)
	captureAccessLogs(t)

	h := WrapHTTPHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		HTTPMiddlewareConfig{
			ServiceName:    "test-service",
			EmitAccessLogs: true,
			SkipAccessLogs: SkipPaths("/health"),
		})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("got %d spans, want 1: SkipAccessLogs must not gate tracing", got)
	}
}
