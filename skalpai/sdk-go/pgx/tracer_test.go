package pgx

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) (*tracetest.SpanRecorder, *QueryTracer) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return rec, &QueryTracer{tracer: tp.Tracer("db")}
}

func traceQuery(qt *QueryTracer, sql string, err error) {
	ctx := qt.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql})
	qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
}

// The span name carries the operation and table, never the full statement:
// raw SQL would explode span-name cardinality and can embed literal values.
func TestSpanNameIsOperationAndTable(t *testing.T) {
	rec, qt := recordSpans(t)

	traceQuery(qt, "INSERT INTO synthesis (id, title) VALUES ($1, $2)", nil)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	if got := spans[0].Name(); got != "db.INSERT synthesis" {
		t.Fatalf("span name = %q, want %q", got, "db.INSERT synthesis")
	}
}

func TestSpanNameCoversEachOperation(t *testing.T) {
	cases := []struct {
		query string
		want  string
	}{
		{"SELECT id FROM synthesis WHERE id = $1", "db.SELECT synthesis"},
		{"UPDATE synthesis SET title = $1", "db.UPDATE synthesis"},
		{"DELETE FROM outbox_events WHERE id = $1", "db.DELETE outbox_events"},
		{"\n\t\tSELECT id\n\t\tFROM outbox_events\n\t", "db.SELECT outbox_events"},
		{"INSERT INTO public.synthesis (id) VALUES ($1)", "db.INSERT public.synthesis"},
		{"WITH claimed AS (SELECT 1) SELECT * FROM claimed", "db.query"},
		{"", "db.query"},
	}

	for _, tc := range cases {
		rec, qt := recordSpans(t)
		traceQuery(qt, tc.query, nil)

		spans := rec.Ended()
		if len(spans) != 1 {
			t.Fatalf("query %q: got %d spans, want 1", tc.query, len(spans))
		}
		if got := spans[0].Name(); got != tc.want {
			t.Fatalf("query %q: span name = %q, want %q", tc.query, got, tc.want)
		}
	}
}

// A failing query must mark its span as an error, otherwise a broken request
// shows a tree of green spans and the failure has to be hunted in the logs.
func TestFailedQueryMarksSpanError(t *testing.T) {
	rec, qt := recordSpans(t)

	traceQuery(qt, "SELECT 1 FROM synthesis", errors.New("connection refused"))

	if code := rec.Ended()[0].Status().Code; code != codes.Error {
		t.Fatalf("status = %v, want %v", code, codes.Error)
	}
}

// pgx reports no rows as an error on QueryRow, but an empty lookup is a normal
// result: marking it failed would paint healthy traces red.
func TestNoRowsIsNotASpanError(t *testing.T) {
	rec, qt := recordSpans(t)

	traceQuery(qt, "SELECT id FROM synthesis WHERE id = $1", pgx.ErrNoRows)

	if code := rec.Ended()[0].Status().Code; code == codes.Error {
		t.Fatalf("status = %v, want no error for pgx.ErrNoRows", code)
	}
}

// The statement lands on an attribute rather than the span name, so a trace
// still shows what ran without every distinct query minting its own name.
func TestSpanCarriesStatementAttribute(t *testing.T) {
	rec, qt := recordSpans(t)

	traceQuery(qt, "SELECT 1 FROM synthesis", nil)

	var statement string
	for _, attr := range rec.Ended()[0].Attributes() {
		if string(attr.Key) == "db.statement" {
			statement = attr.Value.AsString()
		}
	}
	if statement != "SELECT 1 FROM synthesis" {
		t.Fatalf("db.statement = %q, want the executed query", statement)
	}
}

// Query arguments must never reach the span: they carry user content and
// secrets, and a trace backend is not a place to store either.
func TestSpanOmitsQueryArguments(t *testing.T) {
	rec, qt := recordSpans(t)

	ctx := qt.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT id FROM users WHERE email = $1",
		Args: []any{"secret@example.com"},
	})
	qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	for _, attr := range rec.Ended()[0].Attributes() {
		if attr.Value.AsString() == "secret@example.com" {
			t.Fatalf("attribute %q leaked a query argument", attr.Key)
		}
	}
}

// The query span must descend from the caller's span, which is what puts the
// database work under the handler that issued it.
func TestQuerySpanNestsUnderCallerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	qt := &QueryTracer{tracer: tp.Tracer("db")}

	parentCtx, parent := tp.Tracer("test").Start(context.Background(), "handler.Something")
	ctx := qt.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1 FROM synthesis"})
	qt.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	parent.End()

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	if spans[0].Parent().SpanID() != spans[1].SpanContext().SpanID() {
		t.Fatal("query span is not a child of the caller span")
	}
}
