package authz

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Logger records one decision. Every Evaluate through Logged calls it,
// whether the PDP answered or failed.
type Logger interface {
	Decision(ctx context.Context, req Request, d Decision, err error)
}

// Logged wraps pdp so every decision reaches the loggers.
func Logged(pdp PDP, loggers ...Logger) PDP {
	return PDPFunc(func(ctx context.Context, req Request) (Decision, error) {
		d, err := pdp.Evaluate(ctx, req)
		for _, l := range loggers {
			l.Decision(ctx, req, d, err)
		}
		return d, err
	})
}

type slogLogger struct{ l *slog.Logger }

// Slog logs each decision at Info, or Warn when the PDP failed.
func Slog(l *slog.Logger) Logger { return slogLogger{l: l} }

func (s slogLogger) Decision(ctx context.Context, req Request, d Decision, err error) {
	attrs := []any{
		"subject", req.Subject.ID(),
		"subject_type", req.Subject.Type(),
		"action", req.Action.Name,
		"resource", req.Resource.Type + ":" + req.Resource.ID,
		"allow", d.Allow,
		"reason", d.Reason,
	}
	if err != nil {
		s.l.WarnContext(ctx, "authz decision failed", append(attrs, "error", err.Error())...)
		return
	}
	s.l.InfoContext(ctx, "authz decision", attrs...)
}

type spanLogger struct{}

// SpanEvents adds each decision as an event on the span of the request's
// context, when there is one.
func SpanEvents() Logger { return spanLogger{} }

func (spanLogger) Decision(ctx context.Context, req Request, d Decision, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("authz.subject", req.Subject.ID()),
		attribute.String("authz.subject_type", req.Subject.Type()),
		attribute.String("authz.action", req.Action.Name),
		attribute.String("authz.resource", req.Resource.Type+":"+req.Resource.ID),
		attribute.Bool("authz.allow", d.Allow),
		attribute.String("authz.reason", d.Reason),
	}
	if err != nil {
		attrs = append(attrs, attribute.String("authz.error", err.Error()))
	}
	span.AddEvent("authz.decision", trace.WithAttributes(attrs...))
}
