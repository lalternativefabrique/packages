// natstrace.go adds OpenTelemetry tracing for NATS JetStream: publish spans
// with trace-context injection, consume spans with extraction, and the
// header-normalization fix JetStream requires. It lives in otelobs (not the
// core consumer package) so the core stays free of any OTEL dependency — the
// consumer accepts a Config.TraceExtractor hook that TraceExtractor here fills.
package otelobs

import (
	"context"
	"net/http"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
)

var natsTracer = otel.Tracer("nats-instrumentation")

// InstrumentedJetStream wraps nats.JetStreamContext, adding a publish span and
// trace-context injection to every PublishWithContext. All other JetStream
// methods delegate unchanged.
type InstrumentedJetStream struct {
	nats.JetStreamContext
}

// NewInstrumentedJetStream wraps js. The embedded interface means every method
// not overridden below (StreamInfo, AddStream, AddConsumer, ...) delegates to
// the underlying context, so this is a drop-in replacement.
func NewInstrumentedJetStream(js nats.JetStreamContext) *InstrumentedJetStream {
	return &InstrumentedJetStream{JetStreamContext: js}
}

// PublishWithContext publishes data on subject, starting a "publish.<subject>"
// span and injecting the current trace context into the message headers so a
// downstream consumer can link its span to this one.
func (i *InstrumentedJetStream) PublishWithContext(ctx context.Context, subject string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	ctx, span := natsTracer.Start(ctx, "publish."+subject)
	defer span.End()

	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination", subject),
		attribute.String("messaging.operation", "publish"),
	)

	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

	ack, err := i.JetStreamContext.PublishMsg(msg, opts...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetStatus(codes.Ok, "")
	return ack, nil
}

// Publish delegates to PublishWithContext with a background context.
func (i *InstrumentedJetStream) Publish(subject string, data []byte, opts ...nats.PubOpt) (*nats.PubAck, error) {
	return i.PublishWithContext(context.Background(), subject, data, opts...)
}

// NormalizeNATSHeaders converts NATS lowercase header keys to canonical HTTP
// form so propagation.HeaderCarrier (which uses http.Header.Get, expecting
// "Traceparent" not "traceparent") can find them. JetStream stores header keys
// lowercase, so without this the extracted context is always empty.
func NormalizeNATSHeaders(h nats.Header) nats.Header {
	if h == nil {
		return nil
	}
	normalized := make(nats.Header, len(h))
	for k, v := range h {
		normalized[http.CanonicalHeaderKey(k)] = v
	}
	return normalized
}

// ExtractTraceContext derives a context carrying the producer's trace context
// from a NATS message's headers, applying the lowercase-key normalization. It
// has the exact signature the consumer's Config.TraceExtractor expects:
//
//	cfg.TraceExtractor = otelobs.ExtractTraceContext
func ExtractTraceContext(msg *nats.Msg) context.Context {
	ctx := context.Background()
	if msg.Header != nil {
		normalized := NormalizeNATSHeaders(msg.Header)
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(normalized))
	}
	return ctx
}

// ConsumeMessage wraps handler with a "consume.<subject>" span whose parent is
// the producer's span (restored from the message headers). Use it when driving
// messages outside the eda consumer; inside the consumer, prefer wiring
// ExtractTraceContext as Config.TraceExtractor.
func ConsumeMessage(msg *nats.Msg, handler func(context.Context, *nats.Msg) error) error {
	ctx := ExtractTraceContext(msg)

	ctx, span := natsTracer.Start(ctx, "consume."+msg.Subject)
	defer span.End()

	span.SetAttributes(
		attribute.String("messaging.system", "nats"),
		attribute.String("messaging.destination", msg.Subject),
		attribute.String("messaging.operation", "consume"),
	)

	if err := handler(ctx, msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}
