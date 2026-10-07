package skalpai

import (
	"context"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
)

// SlogHandlerOption customizes the handler returned by NewSlogHandler.
type SlogHandlerOption func(*slogHandlerConfig)

type slogHandlerConfig struct {
	minLevel slog.Level
}

// WithMinLevel filters out records below the given slog.Level.
// Default is slog.LevelInfo.
func WithMinLevel(level slog.Level) SlogHandlerOption {
	return func(c *slogHandlerConfig) { c.minLevel = level }
}

// NewSlogHandler returns a slog.Handler that emits records to the OTLP log
// pipeline configured by Init (sent to the Skalpai backend).
//
// The handler does NOT call slog.SetDefault — the caller stays in control of
// routing. Compose it with other handlers via Fanout when both stdout and
// remote ingestion are wanted:
//
//	stdout := slog.NewJSONHandler(os.Stdout, nil)
//	otlp   := skalpai.NewSlogHandler("my-service")
//	logger := slog.New(skalpai.Fanout(stdout, otlp))
//	slog.SetDefault(logger)
//
// If Init has not been called, global.GetLoggerProvider() returns a no-op
// provider, so records are silently dropped — safe to use in tests and dev
// environments without an endpoint.
//
// scopeName is the OTel instrumentation scope (typically the service name).
//
// Trace correlation: when the record's context carries an active span, its
// trace_id and span_id are attached as OTEL log attributes.
func NewSlogHandler(scopeName string, opts ...SlogHandlerOption) slog.Handler {
	cfg := slogHandlerConfig{minLevel: slog.LevelInfo}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &otlpSlogHandler{
		logger:   global.GetLoggerProvider().Logger(scopeName),
		minLevel: cfg.minLevel,
	}
}

// EnableSlogBridge redirects slog.Default() to a handler that emits to OTLP
// while keeping the previous default handler as a stdout fallback.
//
// Deprecated: this calls slog.SetDefault, which since Go 1.21 also redirects
// the stdlib log package. With the OTEL batch processor sitting behind the
// default handler, log.Printf calls made during init (NATS, pgx, etc.) can
// back-pressure into the OTEL export queue and deadlock boot before the HTTP
// server binds. Prefer NewSlogHandler + Fanout, which lets the caller decide
// when (and whether) to install a global default — and never redirects the
// stdlib log package implicitly.
func EnableSlogBridge(serviceName string) {
	fallback := slog.Default().Handler()
	slog.SetDefault(slog.New(&fanoutHandler{
		handlers: []slog.Handler{fallback, NewSlogHandler(serviceName)},
	}))
}

type otlpSlogHandler struct {
	logger   otellog.Logger
	minLevel slog.Level
	attrs    []attribute.KeyValue
	groups   []string
}

func (h *otlpSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *otlpSlogHandler) Handle(ctx context.Context, rec slog.Record) error {
	var otelRec otellog.Record
	otelRec.SetTimestamp(rec.Time)
	otelRec.SetBody(attribute.StringValue(rec.Message))
	otelRec.SetSeverity(slogLevelToOTEL(rec.Level))
	otelRec.SetSeverityText(rec.Level.String())

	attrs := make([]attribute.KeyValue, 0, rec.NumAttrs()+len(h.attrs)+2)
	attrs = append(attrs, h.attrs...)

	prefix := groupPrefix(h.groups)
	rec.Attrs(func(a slog.Attr) bool {
		attrs = appendSlogAttr(attrs, prefix, a)
		return true
	})

	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		attrs = append(attrs,
			attribute.String("trace_id", sc.TraceID().String()),
			attribute.String("span_id", sc.SpanID().String()),
		)
	}

	otelRec.AddAttributes(attrs...)
	h.logger.Emit(ctx, otelRec)
	return nil
}

func (h *otlpSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	prefix := groupPrefix(h.groups)
	newAttrs := make([]attribute.KeyValue, len(h.attrs), len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	for _, a := range attrs {
		newAttrs = appendSlogAttr(newAttrs, prefix, a)
	}
	return &otlpSlogHandler{
		logger:   h.logger,
		minLevel: h.minLevel,
		attrs:    newAttrs,
		groups:   h.groups,
	}
}

func (h *otlpSlogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := make([]string, len(h.groups)+1)
	copy(groups, h.groups)
	groups[len(h.groups)] = name
	return &otlpSlogHandler{
		logger:   h.logger,
		minLevel: h.minLevel,
		attrs:    h.attrs,
		groups:   groups,
	}
}

func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.Join(groups, ".") + "."
}

// appendSlogAttr flattens a slog.Attr into one or more OTEL log attributes,
// honoring group nesting by dot-prefixing keys.
func appendSlogAttr(dst []attribute.KeyValue, prefix string, a slog.Attr) []attribute.KeyValue {
	if a.Equal(slog.Attr{}) {
		return dst
	}
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		sub := v.Group()
		if len(sub) == 0 {
			return dst
		}
		nextPrefix := prefix
		if a.Key != "" {
			nextPrefix = prefix + a.Key + "."
		}
		for _, sa := range sub {
			dst = appendSlogAttr(dst, nextPrefix, sa)
		}
		return dst
	}
	return append(dst, slogValueToOTEL(prefix+a.Key, v))
}

func slogValueToOTEL(key string, v slog.Value) attribute.KeyValue {
	switch v.Kind() {
	case slog.KindString:
		return attribute.String(key, v.String())
	case slog.KindInt64:
		return attribute.Int64(key, v.Int64())
	case slog.KindUint64:
		// otel/log has no uint64; widen to int64 (lossy for very large values).
		return attribute.Int64(key, int64(v.Uint64()))
	case slog.KindFloat64:
		return attribute.Float64(key, v.Float64())
	case slog.KindBool:
		return attribute.Bool(key, v.Bool())
	case slog.KindDuration:
		return attribute.Int64(key, v.Duration().Nanoseconds())
	case slog.KindTime:
		return attribute.String(key, v.Time().Format("2006-01-02T15:04:05.000Z07:00"))
	default:
		return attribute.String(key, v.String())
	}
}

func slogLevelToOTEL(level slog.Level) otellog.Severity {
	switch {
	case level >= slog.LevelError:
		return otellog.SeverityError
	case level >= slog.LevelWarn:
		return otellog.SeverityWarn
	case level >= slog.LevelInfo:
		return otellog.SeverityInfo
	default:
		return otellog.SeverityDebug
	}
}
