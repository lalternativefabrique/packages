package otelobs

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
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

func TestConsumeMessageWithContext_PropagatesHandlerError(t *testing.T) {
	want := errors.New("boom")
	got := ConsumeMessageWithContext(context.Background(), &nats.Msg{Subject: "x"},
		func(context.Context, *nats.Msg) error { return want })

	if !errors.Is(got, want) {
		t.Errorf("error = %v, want %v", got, want)
	}
}
