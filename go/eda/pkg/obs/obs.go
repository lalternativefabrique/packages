// Package obs provides minimal observability primitives (metrics + tracing)
// that integrate with the typed CQRS layer in pkg/cqrs.
//
// The goal is to ship useful middlewares with zero external dependencies.
// Real backends (Prometheus, OpenTelemetry) plug in via the small Meter
// and Tracer interfaces below.
package obs

import (
	"context"
	"time"
)

// Meter records counters and durations. Implementations must be
// goroutine-safe. Backends (Prometheus, OTEL meter, statsd) wrap this.
type Meter interface {
	Counter(name string, tags ...string)
	Observe(name string, value float64, tags ...string)
}

// NopMeter is a Meter that ignores every call. Useful as the default.
type NopMeter struct{}

func (NopMeter) Counter(string, ...string)         {}
func (NopMeter) Observe(string, float64, ...string) {}

// Tracer creates spans for the typed CQRS dispatch path.
// Span.End is called when the wrapped handler returns.
type Tracer interface {
	StartSpan(ctx context.Context, name string, attrs ...string) (context.Context, Span)
}

// Span is the minimal contract this package needs from a tracing span.
type Span interface {
	// SetError annotates the span with the error and marks it as failed.
	SetError(err error)
	// End closes the span.
	End()
}

// NopTracer returns NopSpan for every call.
type NopTracer struct{}

// NopSpan implements Span and does nothing.
type NopSpan struct{}

func (NopSpan) SetError(error) {}
func (NopSpan) End()           {}

func (NopTracer) StartSpan(ctx context.Context, _ string, _ ...string) (context.Context, Span) {
	return ctx, NopSpan{}
}

// Clock is exposed here so users can inject a fake clock into the
// observability middlewares for deterministic tests.
type Clock interface {
	Now() time.Time
}

// SystemClock wraps time.Now.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
