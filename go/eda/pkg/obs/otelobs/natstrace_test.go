package otelobs

import (
	"testing"

	"github.com/nats-io/nats.go"
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
