// Package otelobs provides an OpenTelemetry-backed Tracer for pkg/obs.
//
// It is a thin, opt-in adapter that wraps an OTEL tracer.Tracer into the
// obs.Tracer interface so the CQRS TracingMiddleware can drive spans
// without forcing the core obs package to depend on OTEL.
package otelobs

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/lalternative/packages/go/eda/pkg/obs"
)

// Tracer adapts an OTEL Tracer to obs.Tracer.
type Tracer struct {
	t trace.Tracer
}

// New wires an OTEL trace.Tracer (typically obtained via otel.Tracer("...")).
func New(t trace.Tracer) *Tracer { return &Tracer{t: t} }

// Span adapts an OTEL Span to obs.Span.
type Span struct {
	s trace.Span
}

// SetError records the error on the span and marks the status as Error.
func (s Span) SetError(err error) {
	if err == nil {
		return
	}
	s.s.RecordError(err)
	s.s.SetStatus(codes.Error, err.Error())
}

// End closes the span.
func (s Span) End() { s.s.End() }

// StartSpan starts a child span on ctx with the given name and attributes
// expressed as (key,value) string pairs (matching obs.Tracer).
func (t *Tracer) StartSpan(ctx context.Context, name string, attrs ...string) (context.Context, obs.Span) {
	kv := make([]attribute.KeyValue, 0, len(attrs)/2)
	for i := 0; i+1 < len(attrs); i += 2 {
		kv = append(kv, attribute.String(attrs[i], attrs[i+1]))
	}
	ctx, span := t.t.Start(ctx, name, trace.WithAttributes(kv...))
	return ctx, Span{s: span}
}

// Compile-time interface assertions.
var _ obs.Tracer = (*Tracer)(nil)
var _ obs.Span = Span{}
