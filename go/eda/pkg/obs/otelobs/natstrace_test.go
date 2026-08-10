package otelobs

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestNormalizeNATSHeaders_CanonicalizesKeys(t *testing.T) {
	// JetStream stores header keys lowercase; the carrier needs canonical form.
	in := nats.Header{
		"traceparent": []string{"00-abc-def-01"},
		"TraceState":  []string{"x=1"},
	}
	out := NormalizeNATSHeaders(in)

	if got := out.Get("Traceparent"); got != "00-abc-def-01" {
		t.Errorf(`Get("Traceparent") = %q, want the injected traceparent`, got)
	}
	if got := out.Get("Tracestate"); got != "x=1" {
		t.Errorf(`Get("Tracestate") = %q, want "x=1"`, got)
	}
}

func TestNormalizeNATSHeaders_NilIsNil(t *testing.T) {
	if NormalizeNATSHeaders(nil) != nil {
		t.Fatalf("NormalizeNATSHeaders(nil) should be nil")
	}
}

func TestExtractTraceContext_NoHeadersReturnsBackground(t *testing.T) {
	// A message with no headers must still yield a usable (empty) context, not panic.
	ctx := ExtractTraceContext(&nats.Msg{Subject: "integration.x"})
	if ctx == nil {
		t.Fatalf("ExtractTraceContext returned nil context")
	}
}

type ctxKey struct{}

func TestExtractTraceContextInto_KeepsCallerValues(t *testing.T) {
	// In a service the SDK installs this globally; a bare test process has none.
	otel.SetTextMapPropagator(propagation.TraceContext{})
	parent := context.WithValue(context.Background(), ctxKey{}, "kept")

	ctx := ExtractTraceContextInto(parent, &nats.Msg{
		Subject: "integration.x",
		Header:  nats.Header{"traceparent": []string{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"}},
	})

	if ctx.Value(ctxKey{}) != "kept" {
		t.Error("the caller's values must survive extraction")
	}
	if sc := trace.SpanContextFromContext(ctx); !sc.IsValid() {
		t.Error("the producer's span context should have been restored")
	}
}

func TestExtractTraceContextInto_NoHeadersReturnsParent(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey{}, "kept")

	if got := ExtractTraceContextInto(parent, &nats.Msg{Subject: "x"}); got.Value(ctxKey{}) != "kept" {
		t.Error("a header-less message must yield the parent untouched")
	}
	if got := ExtractTraceContextInto(parent, nil); got != parent {
		t.Error("a nil message must yield the parent itself")
	}
}

func TestConsumeMessageWithContext_RunsHandlerOnDerivedContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxKey{}, "kept")
	called := false

	err := ConsumeMessageWithContext(parent, &nats.Msg{Subject: "integration.x"}, func(ctx context.Context, _ *nats.Msg) error {
		called = true
		if ctx.Value(ctxKey{}) != "kept" {
			t.Error("the handler must see the caller's values")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ConsumeMessageWithContext: %v", err)
	}
	if !called {
		t.Error("handler never ran")
	}
}

const producerTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

func recordConsumeSpan(t *testing.T, ctx context.Context, msg *nats.Msg) tracetest.SpanStub {
	t.Helper()
	otel.SetTextMapPropagator(propagation.TraceContext{})

	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	previous := natsTracer
	natsTracer = provider.Tracer("nats-instrumentation")
	t.Cleanup(func() { natsTracer = previous })

	if err := ConsumeMessageWithContext(ctx, msg, func(context.Context, *nats.Msg) error { return nil }); err != nil {
		t.Fatalf("ConsumeMessageWithContext: %v", err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want exactly the consume span", len(spans))
	}
	return spans[0]
}

func TestConsumeMessageWithContext_LinksProducerWithoutParenting(t *testing.T) {
	// The publisher does not await the handler, so the consume span must be a
	// root that merely links back — never a child whose duration outlives it.
	span := recordConsumeSpan(t, context.Background(), &nats.Msg{
		Subject: "integration.x",
		Header:  nats.Header{"traceparent": []string{producerTraceparent}},
	})

	if span.Parent.IsValid() {
		t.Errorf("consume span has parent %v, want a root span", span.Parent.SpanID())
	}
	if len(span.Links) != 1 {
		t.Fatalf("got %d links, want one link to the producer", len(span.Links))
	}
	if got := span.Links[0].SpanContext.SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("link points at span %s, want the producer's 00f067aa0ba902b7", got)
	}
	if span.SpanKind != trace.SpanKindConsumer {
		t.Errorf("SpanKind = %v, want Consumer", span.SpanKind)
	}
}

func TestConsumeMessageWithContext_DetachesFromPreExtractedContext(t *testing.T) {
	// The eda consumer applies Config.TraceExtractor before calling in, so the
	// producer's span context arrives on ctx rather than only in the headers.
	otel.SetTextMapPropagator(propagation.TraceContext{})
	msg := &nats.Msg{
		Subject: "integration.x",
		Header:  nats.Header{"traceparent": []string{producerTraceparent}},
	}
	span := recordConsumeSpan(t, ExtractTraceContext(msg), msg)

	if span.Parent.IsValid() {
		t.Errorf("consume span adopted ctx's span as parent %v, want a root span", span.Parent.SpanID())
	}
	if len(span.Links) != 1 {
		t.Errorf("got %d links, want the producer link preserved", len(span.Links))
	}
}

func TestConsumeMessageWithContext_NoProducerYieldsUnlinkedRoot(t *testing.T) {
	span := recordConsumeSpan(t, context.Background(), &nats.Msg{Subject: "integration.x"})

	if span.Parent.IsValid() {
		t.Errorf("consume span has parent %v, want a root span", span.Parent.SpanID())
	}
	if len(span.Links) != 0 {
		t.Errorf("got %d links, want none when no producer context travelled", len(span.Links))
	}
}

func TestConsumeMessageWithContext_PropagatesHandlerError(t *testing.T) {
	want := errors.New("boom")
	got := ConsumeMessageWithContext(context.Background(), &nats.Msg{Subject: "x"},
		func(context.Context, *nats.Msg) error { return want })

	if !errors.Is(got, want) {
		t.Errorf("error = %v, want %v", got, want)
	}
}
