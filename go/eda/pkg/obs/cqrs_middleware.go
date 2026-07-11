package obs

import (
	"context"
	"reflect"
	"time"

	"github.com/lalternative/packages/go/eda/pkg/cqrs"
	"github.com/lalternative/packages/go/eda/pkg/logger"
)

// LoggingMiddleware logs every handler call: start, end, latency, error.
// The handler's static input type is used as the log "kind" so commands
// and queries appear with their Go type name.
func LoggingMiddleware[In, Out any](log logger.Logger) cqrs.Middleware[In, Out] {
	var zero In
	kind := reflect.TypeOf(&zero).Elem().String()
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (Out, error) {
			start := time.Now()
			log.Debug("cqrs handler start", logger.String("kind", kind))
			out, err := next(ctx, in)
			elapsed := time.Since(start)
			if err != nil {
				log.Error("cqrs handler error",
					logger.String("kind", kind),
					logger.String("error", err.Error()),
					logger.Int64("elapsed_us", elapsed.Microseconds()),
				)
				return out, err
			}
			log.Info("cqrs handler ok",
				logger.String("kind", kind),
				logger.Int64("elapsed_us", elapsed.Microseconds()),
			)
			return out, nil
		}
	}
}

// MetricsMiddleware records a counter and a duration observation per
// handler invocation. Tags: kind=<Go type>, status=ok|error.
func MetricsMiddleware[In, Out any](m Meter) cqrs.Middleware[In, Out] {
	if m == nil {
		m = NopMeter{}
	}
	var zero In
	kind := reflect.TypeOf(&zero).Elem().String()
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (Out, error) {
			start := time.Now()
			out, err := next(ctx, in)
			elapsed := time.Since(start).Seconds()
			status := "ok"
			if err != nil {
				status = "error"
			}
			m.Counter("cqrs_handler_total", "kind", kind, "status", status)
			m.Observe("cqrs_handler_duration_seconds", elapsed, "kind", kind, "status", status)
			return out, err
		}
	}
}

// TracingMiddleware starts a span around the handler and annotates it
// with errors.
func TracingMiddleware[In, Out any](t Tracer) cqrs.Middleware[In, Out] {
	if t == nil {
		t = NopTracer{}
	}
	var zero In
	kind := reflect.TypeOf(&zero).Elem().String()
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (Out, error) {
			ctx, span := t.StartSpan(ctx, "cqrs.handle", "kind", kind)
			defer span.End()
			out, err := next(ctx, in)
			if err != nil {
				span.SetError(err)
			}
			return out, err
		}
	}
}
