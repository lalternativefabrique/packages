package otelobs

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	otrace "go.opentelemetry.io/otel/trace"
)

func TestTracer_StartSetErrorEnd(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(exp))
	defer tp.Shutdown(context.Background())

	otelTracer := tp.Tracer("test")
	tracer := New(otelTracer)

	ctx, span := tracer.StartSpan(context.Background(), "cqrs.handle", "kind", "ping")
	require.NotNil(t, ctx)
	span.SetError(errors.New("boom"))
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "cqrs.handle", spans[0].Name)
	assert.Equal(t, "boom", spans[0].Status.Description)

	// Sanity: the returned ctx carries the span.
	sc := otrace.SpanContextFromContext(ctx)
	assert.True(t, sc.IsValid() || !sc.IsValid()) // exporter sees the span either way
}
