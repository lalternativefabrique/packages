package skalpai

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricapi "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddlewareConfig controls the HTTP server telemetry emitted by the SDK.
type HTTPMiddlewareConfig struct {
	ServiceName    string
	RouteExtractor func(*http.Request) string
	// EmitAccessLogs emits one slog.Info record per request with method,
	// route, status, and duration. 5xx responses are always logged at Error
	// level regardless of this setting.
	EmitAccessLogs bool
	// Redaction controls query-string sanitization in access logs.
	// Zero value = redaction ON with DefaultSensitiveQueryKeys.
	// Set Redaction.Disabled = true to opt out entirely.
	Redaction RedactionConfig
	// DisableTracing turns off the server span. Tracing is on by default:
	// a caller that wires metrics but forgets an opt-in would otherwise
	// export a silently trace-less service.
	DisableTracing bool
	// SkipTracing suppresses the span for requests it returns true for,
	// leaving metrics and access logs untouched. Nil = trace everything.
	// See SkipPaths for the usual probe-endpoint case.
	//
	// A skipped request starts no span, so anything it calls downstream
	// roots its own trace instead of continuing this one. Harmless for
	// probes; a reason not to skip business routes just because they are
	// noisy.
	SkipTracing func(*http.Request) bool
}

// SkipPaths returns a SkipTracing predicate matching the request path against
// an exact set, for dropping spans from probe endpoints scraped on a timer.
func SkipPaths(paths ...string) func(*http.Request) bool {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[p] = struct{}{}
	}

	return func(r *http.Request) bool {
		if r.URL == nil {
			return false
		}
		_, ok := set[r.URL.Path]
		return ok
	}
}

type httpServerInstruments struct {
	requestCount    metricapi.Int64Counter
	requestDuration metricapi.Float64Histogram
	activeRequests  metricapi.Int64UpDownCounter
	responseSize    metricapi.Int64Histogram
}

var (
	httpServerMu                   sync.Mutex
	httpServerInstrumentsByService = map[string]httpServerInstruments{}
)

// NewHTTPMiddleware returns a standard net/http middleware that records
// normalized request count, duration, active request, and response size
// metrics, and emits a server span per request unless DisableTracing or
// SkipTracing suppresses it.
func NewHTTPMiddleware(cfg HTTPMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return WrapHTTPHandler(next, cfg)
	}
}

// WrapHTTPHandler wraps a net/http handler with Skalpai HTTP server metrics
// and tracing.
func WrapHTTPHandler(next http.Handler, cfg HTTPMiddlewareConfig) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	serviceName := resolveServiceName(cfg.ServiceName)
	instruments := getHTTPServerInstruments(serviceName)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseAttrs := requestAttributes(r, cfg.RouteExtractor, 0)

		tracing := !cfg.DisableTracing && !(cfg.SkipTracing != nil && cfg.SkipTracing(r))

		span := trace.SpanFromContext(r.Context())
		if tracing {
			var ctx context.Context
			ctx, span = startServerSpan(r, cfg, serviceName, baseAttrs)
			defer span.End()
			r = r.WithContext(ctx)
		}

		instruments.activeRequests.Add(r.Context(), 1, metricapi.WithAttributes(baseAttrs...))
		defer instruments.activeRequests.Add(r.Context(), -1, metricapi.WithAttributes(baseAttrs...))

		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		duration := time.Since(startedAt)
		finalAttrs := requestAttributes(r, cfg.RouteExtractor, recorder.status)

		if tracing {
			recordSpanStatus(span, recorder.status, finalAttrs)
		}

		instruments.requestCount.Add(r.Context(), 1, metricapi.WithAttributes(finalAttrs...))
		instruments.requestDuration.Record(r.Context(), duration.Seconds(), metricapi.WithAttributes(finalAttrs...))
		instruments.responseSize.Record(r.Context(), recorder.bytes, metricapi.WithAttributes(finalAttrs...))

		emitAccessLog(r, cfg, recorder, duration)
	})
}

// startServerSpan continues the caller's trace when the request carries
// W3C traceparent headers, so a span started here joins the upstream trace
// instead of rooting an orphan one.
func startServerSpan(r *http.Request, cfg HTTPMiddlewareConfig, serviceName string, attrs []attribute.KeyValue) (context.Context, trace.Span) {
	ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

	return otel.Tracer(serviceName).Start(ctx, spanName(r, cfg.RouteExtractor),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
}

// spanName favours the templated route over the raw path to keep span names
// bounded: a path carrying ids explodes cardinality in the trace backend.
func spanName(r *http.Request, routeExtractor func(*http.Request) string) string {
	if routeExtractor != nil {
		if route := strings.TrimSpace(routeExtractor(r)); route != "" {
			return r.Method + " " + route
		}
	}
	return r.Method
}

func recordSpanStatus(span trace.Span, statusCode int, attrs []attribute.KeyValue) {
	span.SetAttributes(attrs...)
	if statusCode >= 500 {
		span.SetStatus(codes.Error, statusClass(statusCode))
		return
	}
	span.SetStatus(codes.Ok, "")
}

func getHTTPServerInstruments(serviceName string) httpServerInstruments {
	httpServerMu.Lock()
	defer httpServerMu.Unlock()

	if instruments, ok := httpServerInstrumentsByService[serviceName]; ok {
		return instruments
	}

	meter := otel.Meter(serviceName)
	requestCount, _ := meter.Int64Counter(
		"http.server.request.count",
		metricapi.WithDescription("Total number of HTTP server requests."),
		metricapi.WithUnit("{request}"),
	)
	requestDuration, _ := meter.Float64Histogram(
		"http.server.request.duration",
		metricapi.WithDescription("Total HTTP server request duration in seconds."),
		metricapi.WithUnit("s"),
	)
	activeRequests, _ := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metricapi.WithDescription("Current number of in-flight HTTP server requests."),
		metricapi.WithUnit("{request}"),
	)
	responseSize, _ := meter.Int64Histogram(
		"http.server.response.size",
		metricapi.WithDescription("HTTP response size in bytes."),
		metricapi.WithUnit("By"),
	)

	instruments := httpServerInstruments{
		requestCount:    requestCount,
		requestDuration: requestDuration,
		activeRequests:  activeRequests,
		responseSize:    responseSize,
	}
	httpServerInstrumentsByService[serviceName] = instruments
	return instruments
}

func resolveServiceName(serviceName string) string {
	if trimmed := strings.TrimSpace(serviceName); trimmed != "" {
		return trimmed
	}
	if env := strings.TrimSpace(os.Getenv("SKALPAI_SERVICE")); env != "" {
		return env
	}
	if env := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")); env != "" {
		return env
	}
	return "unknown"
}

func requestAttributes(r *http.Request, routeExtractor func(*http.Request) string, statusCode int) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("http.request.method", r.Method),
		attribute.String("server.address", r.Host),
		attribute.String("url.scheme", requestScheme(r)),
	}
	if routeExtractor != nil {
		if route := strings.TrimSpace(routeExtractor(r)); route != "" {
			attrs = append(attrs, attribute.String("http.route", route))
		}
	}
	if statusCode > 0 {
		attrs = append(attrs,
			attribute.Int("http.response.status_code", statusCode),
			attribute.String("http.response.status_class", statusClass(statusCode)),
		)
	}
	return attrs
}

func requestScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.URL != nil && r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	return "http"
}

func statusClass(statusCode int) string {
	if statusCode <= 0 {
		return "unknown"
	}
	return strconv.Itoa(statusCode/100) + "xx"
}

func emitAccessLog(r *http.Request, cfg HTTPMiddlewareConfig, rec *statusRecorder, duration time.Duration) {
	isServerError := rec.status >= 500
	if !cfg.EmitAccessLogs && !isServerError {
		return
	}

	route := r.URL.Path
	if cfg.RouteExtractor != nil {
		if extracted := strings.TrimSpace(cfg.RouteExtractor(r)); extracted != "" {
			route = extracted
		}
	}

	attrs := []any{
		slog.String("http.request.method", r.Method),
		slog.String("http.route", route),
		slog.String("url.path", r.URL.Path),
		slog.String("url.query", RedactQuery(r.URL.RawQuery, cfg.Redaction)),
		slog.String("url.scheme", requestScheme(r)),
		slog.String("server.address", r.Host),
		slog.Int("http.response.status_code", rec.status),
		slog.Int64("http.response.size", rec.bytes),
		slog.Float64("http.server.request.duration_ms", float64(duration.Microseconds())/1000.0),
	}

	if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
		attrs = append(attrs,
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}

	level := slog.LevelInfo
	if isServerError {
		level = slog.LevelError
	}
	slog.Default().LogAttrs(r.Context(), level, "http request", attrsToSlog(attrs)...)
}

func attrsToSlog(in []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(in))
	for _, a := range in {
		if attr, ok := a.(slog.Attr); ok {
			out = append(out, attr)
		}
	}
	return out
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)
	return n, err
}
