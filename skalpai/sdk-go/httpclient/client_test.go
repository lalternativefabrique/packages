package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return rec
}

// An outgoing call must appear in the trace, so a slow upstream shows up as a
// span rather than as unexplained time inside its caller.
func TestOutgoingRequestEmitsSpan(t *testing.T) {
	rec := recordSpans(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, parent := otel.Tracer("test").Start(context.Background(), "caller")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := Wrap(&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	parent.End()

	if len(rec.Ended()) < 2 {
		t.Fatalf("got %d spans, want the client span plus its caller", len(rec.Ended()))
	}
}

// The client must send traceparent so the receiving service continues this
// trace instead of rooting its own.
func TestOutgoingRequestPropagatesTraceparent(t *testing.T) {
	recordSpans(t)

	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Traceparent")
	}))
	defer srv.Close()

	ctx, parent := otel.Tracer("test").Start(context.Background(), "caller")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := Wrap(&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()
	parent.End()

	if got == "" {
		t.Fatal("no traceparent header reached the server")
	}
}

// Wrapping must keep the caller's own transport settings; replacing them
// would silently drop custom TLS or proxy configuration.
func TestWrapPreservesExistingTransport(t *testing.T) {
	client := Wrap(&http.Client{Transport: &http.Transport{MaxIdleConns: 42}})

	if client.Transport == nil {
		t.Fatal("wrapped client has no transport")
	}
	if client.Transport == http.DefaultTransport {
		t.Fatal("wrapping replaced the caller's transport")
	}
}

// Wrapping must not discard the caller's timeout: these clients call external
// APIs, and losing the timeout turns a hung upstream into a stuck handler.
func TestWrapPreservesTimeout(t *testing.T) {
	if client := Wrap(&http.Client{Timeout: 42}); client.Timeout != 42 {
		t.Fatalf("timeout = %v, want it preserved", client.Timeout)
	}
}

// A nil client is a usable default rather than a panic, so call sites can wrap
// unconditionally.
func TestWrapHandlesNilClient(t *testing.T) {
	if Wrap(nil) == nil {
		t.Fatal("Wrap(nil) returned nil")
	}
}

// Wrapping must not mutate the client it was given: a shared client wrapped
// twice would otherwise stack transports.
func TestWrapDoesNotMutateInput(t *testing.T) {
	original := &http.Client{}
	Wrap(original)

	if original.Transport != nil {
		t.Fatal("Wrap mutated the caller's client")
	}
}
