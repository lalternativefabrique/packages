// Package skalpai provides zero-config observability for Go apps.
//
// Usage:
//
//	import "github.com/lalternative/packages/skalpai/sdk-go"
//
//	func main() {
//	    shutdown, err := skalpai.Init(context.Background(), skalpai.Config{
//	        Endpoint:    os.Getenv("SKALPAI_ENDPOINT"),
//	        APIKey:      os.Getenv("SKALPAI_API_KEY"),
//	        ServiceName: "my-service",
//	    })
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer shutdown(context.Background())
//	    // ... app code
//	}
//
// Environment variables (used as fallback):
//
//	SKALPAI_ENDPOINT — Skalpai backend URL
//	SKALPAI_API_KEY  — Project API key
//	SKALPAI_SERVICE  — Service name
//	OTEL_SERVICE_NAME — Service name fallback
package skalpai

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config holds the Skalpai SDK configuration.
type Config struct {
	Endpoint       string
	APIKey         string
	ServiceName    string
	ServiceVersion string
	Environment    string
	// MetricsInterval controls how often metrics are exported (default: 15s).
	MetricsInterval time.Duration
}

func (c *Config) withDefaults() {
	if c.Endpoint == "" {
		c.Endpoint = os.Getenv("SKALPAI_ENDPOINT")
	}
	if c.APIKey == "" {
		c.APIKey = os.Getenv("SKALPAI_API_KEY")
	}
	if c.ServiceName == "" {
		c.ServiceName = os.Getenv("SKALPAI_SERVICE")
		if c.ServiceName == "" {
			c.ServiceName = os.Getenv("OTEL_SERVICE_NAME")
		}
		if c.ServiceName == "" {
			c.ServiceName = "unknown"
		}
	}
	if c.ServiceVersion == "" {
		c.ServiceVersion = "0.0.0"
	}
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.MetricsInterval == 0 {
		c.MetricsInterval = 15 * time.Second
	}
}

// Init initializes the Skalpai SDK and returns a shutdown function.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	cfg.withDefaults()

	if cfg.Endpoint == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("skalpai: SKALPAI_ENDPOINT and SKALPAI_API_KEY are required")
	}

	headers := map[string]string{"x-api-key": cfg.APIKey}

	// WithFromEnv reads OTEL_RESOURCE_ATTRIBUTES, which is how a deployment
	// supplies service.instance.id. Without a per-instance attribute every
	// replica of a service reports an identical resource, so a consumer cannot
	// tell two cumulative counters apart from one counter that keeps resetting.
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("skalpai: resource: %w", err)
	}

	// Traces
	traceExp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(cfg.Endpoint+"/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("skalpai: trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Metrics
	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(cfg.Endpoint+"/v1/metrics"),
		otlpmetrichttp.WithHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("skalpai: metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(cfg.MetricsInterval),
		)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	// Logs
	logExp, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpointURL(cfg.Endpoint+"/v1/logs"),
		otlploghttp.WithHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("skalpai: log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	otellog.SetLoggerProvider(lp)

	// Register runtime metrics
	stopMetrics := registerRuntimeMetrics(mp, cfg.ServiceName)

	fmt.Printf("[skalpai] initialized — service=%s endpoint=%s\n", cfg.ServiceName, cfg.Endpoint)

	shutdown := func(ctx context.Context) error {
		stopMetrics()
		tpErr := tp.Shutdown(ctx)
		mpErr := mp.Shutdown(ctx)
		lpErr := lp.Shutdown(ctx)
		if tpErr != nil {
			return tpErr
		}
		if mpErr != nil {
			return mpErr
		}
		return lpErr
	}

	return shutdown, nil
}

func registerRuntimeMetrics(mp *sdkmetric.MeterProvider, serviceName string) func() {
	meter := mp.Meter(serviceName)
	done := make(chan struct{})

	// CPU utilization
	cpuGauge, _ := meter.Float64ObservableGauge("process.cpu.utilization",
		metric.WithDescription("Process CPU utilization (0-1)"),
	)

	// Memory heap
	heapGauge, _ := meter.Float64ObservableGauge("process.runtime.go.mem.heap_alloc",
		metric.WithDescription("Go heap allocation in bytes"),
		metric.WithUnit("By"),
	)

	// Memory RSS (sys)
	sysGauge, _ := meter.Float64ObservableGauge("process.memory.rss",
		metric.WithDescription("Total memory from OS in bytes"),
		metric.WithUnit("By"),
	)

	// Goroutines
	goroutineGauge, _ := meter.Float64ObservableGauge("process.runtime.go.goroutines",
		metric.WithDescription("Number of goroutines"),
	)

	meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		o.ObserveFloat64(heapGauge, float64(m.HeapAlloc))
		o.ObserveFloat64(sysGauge, float64(m.Sys))
		o.ObserveFloat64(goroutineGauge, float64(runtime.NumGoroutine()))
		// CPU: approximation via GC CPU fraction
		o.ObserveFloat64(cpuGauge, m.GCCPUFraction)
		return nil
	}, cpuGauge, heapGauge, sysGauge, goroutineGauge)

	return func() { close(done) }
}
