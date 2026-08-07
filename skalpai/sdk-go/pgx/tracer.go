// Package pgx traces database queries into the current trace.
//
// It hooks pgx itself rather than a repository or Executor wrapper, because
// repositories commonly hold a *pgxpool.Pool and query it directly: only the
// driver sees every query, so anything higher up would instrument a subset
// while reporting a partial trace as a complete one.
//
// Wire it on the pool config, before opening the pool:
//
//	cfg, err := pgxpool.ParseConfig(dsn)
//	if err != nil {
//		return nil, err
//	}
//	cfg.ConnConfig.Tracer = skalpaipgx.NewQueryTracer()
//	pool, err := pgxpool.NewWithConfig(ctx, cfg)
package pgx

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// QueryTracer emits one span per database query.
type QueryTracer struct {
	tracer trace.Tracer
}

// NewQueryTracer returns a pgx.QueryTracer reporting to the global provider.
func NewQueryTracer() *QueryTracer {
	return &QueryTracer{}
}

func (t *QueryTracer) resolve() trace.Tracer {
	if t.tracer != nil {
		return t.tracer
	}
	return otel.Tracer("db")
}

type spanKey struct{}

// TraceQueryStart opens the span. Arguments are deliberately dropped: they
// carry user content and credentials, which must not reach a trace backend.
func (t *QueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx, span := t.resolve().Start(ctx, spanName(data.SQL),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.statement", data.SQL),
		),
	)
	return context.WithValue(ctx, spanKey{}, span)
}

// TraceQueryEnd closes the span opened by TraceQueryStart.
func (t *QueryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	span, ok := ctx.Value(spanKey{}).(trace.Span)
	if !ok {
		return
	}
	defer span.End()

	// QueryRow reports an empty result as ErrNoRows; an absent row is a normal
	// outcome, so flagging it would paint healthy traces red.
	if data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
		return
	}
	span.SetStatus(codes.Ok, "")
}

// spanName reports the operation and the table it targets. The raw statement
// would mint a distinct span name per query text, so it travels as an
// attribute instead; anything this cannot parse falls back to a fixed name
// rather than leaking a fragment of SQL into the name.
func spanName(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return "db.query"
	}

	op := strings.ToUpper(fields[0])
	keyword, ok := tableKeyword[op]
	if !ok {
		return "db.query"
	}

	for i, field := range fields {
		if !strings.EqualFold(field, keyword) || i+1 >= len(fields) {
			continue
		}
		if table := strings.Trim(fields[i+1], "(\"`;"); table != "" {
			return "db." + op + " " + table
		}
	}
	return "db." + op
}

// The token naming the table differs per operation: UPDATE names it directly,
// the others put it behind a keyword.
var tableKeyword = map[string]string{
	"SELECT": "FROM",
	"INSERT": "INTO",
	"DELETE": "FROM",
	"UPDATE": "UPDATE",
}
