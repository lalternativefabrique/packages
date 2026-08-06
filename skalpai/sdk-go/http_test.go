package skalpai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

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
