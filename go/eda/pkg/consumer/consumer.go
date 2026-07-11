// Package consumer provides a production-grade durable JetStream work-queue
// consumer. It exists so application code never hand-rolls JetStream
// redelivery semantics: the dangerous parts — Term() on permanent errors,
// bounded MaxDeliver, staged BackOff, a dead-letter stream, ack heartbeats,
// idempotency and an auto-reconnect loop — all live here, once.
//
// A handler implements EventHandler and only writes Handle(). Everything in
// the table below is provided by default:
//
//	Concern                       Provided by
//	----------------------------- ------------------------------
//	Permanent error -> Term()     ErrPermanent sentinel
//	Bounded MaxDeliver            EventHandler.MaxDeliver()
//	Staged BackOff                Config.BackOff (sane default)
//	Dead-letter stream (DLQ)      advisory MAX_DELIVERIES stream
//	Heartbeat anti-AckWait        InProgress() ticker
//	Idempotency                   optional IdempotencyStore
//	Reconnect / retry loop        Run()
//
// Contrast: JetStreamStore.Subscribe uses an OrderedConsumer for event-sourcing
// replay (no ack, no redelivery). This package is the opposite: a durable
// AckExplicit work queue for processing integration events as tasks.
package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lalternative/packages/go/eda/pkg/logger"
)

// ErrPermanent marks an error as non-retryable. A handler returning an error
// that wraps ErrPermanent causes the message to be Term()'d immediately
// instead of redelivered — use it for malformed payloads or business-rule
// rejections that no retry can fix. Any other error is treated as transient
// and redelivered (up to MaxDeliver, then routed to the DLQ).
var ErrPermanent = errors.New("permanent error")

// Permanent wraps err so the consumer terminates the message instead of
// retrying. Permanent(nil) returns nil.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrPermanent, err)
}

// EventHandler is the contract an application handler implements. The consumer
// owns all JetStream wiring; the handler owns only business logic in Handle.
type EventHandler interface {
	// Name identifies the handler in logs and metrics.
	Name() string
	// Subject is the NATS subject (filter) the consumer binds to,
	// e.g. "integration.source.content.created" or "integration.>".
	Subject() string
	// DurableName is the durable consumer name; it persists the consumer
	// position across restarts and is the idempotency scope.
	DurableName() string
	// MaxDeliver bounds delivery attempts before the message is dead-lettered.
	// Typical value: 3 (first attempt + 2 retries).
	MaxDeliver() int
	// Handle processes one message. Return nil on success; wrap ErrPermanent
	// (or use Permanent) for non-retryable failures; return any other error
	// to trigger a bounded, backed-off retry.
	Handle(ctx context.Context, msg *nats.Msg) error
}

// ConcurrentHandler is an optional interface: a handler implementing it is
// driven with up to MaxConcurrency in-flight messages. Handlers that don't
// implement it are processed sequentially (concurrency 1).
type ConcurrentHandler interface {
	MaxConcurrency() int
}

// IdempotencyStore is an optional dedup gate keyed by (durable, eventID).
// When supplied, the consumer skips any message whose event_id was already
// processed by this durable, making at-least-once delivery effectively
// exactly-once for the handler. Inject nil to disable.
type IdempotencyStore interface {
	IsProcessed(ctx context.Context, durable, eventID string) (bool, error)
	MarkProcessed(ctx context.Context, durable, eventID string) error
}

// Config tunes the consumer. The zero value is usable: missing fields fall
// back to the defaults documented on each field.
type Config struct {
	// StreamName is the work-queue stream handlers consume from.
	// Default: "INTEGRATION_PIPELINE".
	StreamName string
	// StreamSubjects are the subjects bound to StreamName when it is created.
	// Default: ["integration.>"].
	StreamSubjects []string
	// DLQStreamName captures MAX_DELIVERIES advisories (the dead letters).
	// Default: "DLQ".
	DLQStreamName string
	// DLQMaxAge is how long dead letters are retained. Default: 7 days.
	DLQMaxAge time.Duration
	// AckWait is the redelivery timeout for an un-acked message.
	// Default: 30s. Long handlers should rely on the ack heartbeat rather
	// than a large AckWait.
	AckWait time.Duration
	// BackOff is the staged redelivery schedule. Its length should be >=
	// the largest MaxDeliver you use. Default: 30s, 60s, 120s.
	BackOff []time.Duration
	// MaxAckPending caps unacked in-flight messages. Default: 1000.
	MaxAckPending int
	// HeartbeatInterval is how often InProgress() is sent (for concurrent
	// handlers, or any handler when HeartbeatAlways is set) to defer AckWait
	// during long processing. Default: 10s.
	HeartbeatInterval time.Duration
	// HeartbeatAlways arms the ack heartbeat even for sequential (concurrency
	// 1) handlers. Set it for long single-threaded workers whose processing
	// can exceed AckWait. Default: false (heartbeat only when concurrency > 1).
	HeartbeatAlways bool
	// RetryBackoff is the wait before re-establishing a dropped consumer in
	// Run. Default: 2s.
	RetryBackoff time.Duration
	// Idempotency, when non-nil, gates duplicate event_ids. Default: disabled.
	Idempotency IdempotencyStore
	// Logger receives structured progress logs. Default: a discard logger.
	Logger logger.Logger
}

func (c *Config) withDefaults() {
	if c.StreamName == "" {
		c.StreamName = "INTEGRATION_PIPELINE"
	}
	if len(c.StreamSubjects) == 0 {
		c.StreamSubjects = []string{"integration.>"}
	}
	if c.DLQStreamName == "" {
		c.DLQStreamName = "DLQ"
	}
	if c.DLQMaxAge == 0 {
		c.DLQMaxAge = 7 * 24 * time.Hour
	}
	if c.AckWait == 0 {
		c.AckWait = 30 * time.Second
	}
	if len(c.BackOff) == 0 {
		c.BackOff = []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}
	}
	if c.MaxAckPending == 0 {
		c.MaxAckPending = 1000
	}
	if c.HeartbeatInterval == 0 {
		c.HeartbeatInterval = 10 * time.Second
	}
	if c.RetryBackoff == 0 {
		c.RetryBackoff = 2 * time.Second
	}
	if c.Logger == nil {
		c.Logger = logger.Nop{}
	}
}

// Start runs a durable consumer for handler until ctx is cancelled. It ensures
// the work-queue and DLQ streams exist, creates/updates the durable consumer
// with bounded MaxDeliver + staged BackOff, and dispatches messages with
// Term-on-permanent / Nak-on-transient / Ack-on-success semantics. It blocks
// until ctx is done, draining in-flight work before returning.
//
// For resilience against connection drops, prefer Run, which wraps Start in a
// reconnect loop.
func Start(ctx context.Context, nc *nats.Conn, handler EventHandler, cfg Config) error {
	cfg.withDefaults()
	log := cfg.Logger

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream context: %w", err)
	}
	if err := ensurePipelineStream(ctx, js, cfg); err != nil {
		return err
	}
	if err := ensureDLQStream(ctx, js, cfg); err != nil {
		return err
	}

	// JetStream rejects a consumer whose BackOff is at least as long as
	// MaxDeliver (err 10116). Clamp defensively so a caller that sets a long
	// BackOff or a small MaxDeliver never produces a consumer that fails to
	// create and silently loops in the reconnect path.
	backoff := cfg.BackOff
	if md := handler.MaxDeliver(); md > 0 && len(backoff) >= md {
		backoff = backoff[:md-1]
	}

	cons, err := js.CreateOrUpdateConsumer(ctx, cfg.StreamName, jetstream.ConsumerConfig{
		Durable:       handler.DurableName(),
		FilterSubject: handler.Subject(),
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    handler.MaxDeliver(),
		DeliverPolicy: jetstream.DeliverAllPolicy,
		ReplayPolicy:  jetstream.ReplayInstantPolicy,
		MaxAckPending: cfg.MaxAckPending,
		BackOff:       backoff,
	})
	if err != nil {
		return fmt.Errorf("create consumer %s: %w", handler.DurableName(), err)
	}

	concurrency := handlerConcurrency(handler)
	log.Info("consumer started",
		logger.String("handler", handler.Name()),
		logger.String("subject", handler.Subject()),
		logger.String("durable", handler.DurableName()),
		logger.Int("concurrency", concurrency),
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	consumeCtx, err := cons.Consume(func(msg jetstream.Msg) {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// Heartbeat (InProgress) defers AckWait during long processing.
			// Armed for concurrent handlers, and for any handler that opts in
			// with HeartbeatAlways — long single-threaded workers (e.g. a
			// transcoder) need it too, otherwise AckWait fires mid-job and the
			// message is redelivered while still being processed.
			var stopHeartbeat func()
			if concurrency > 1 || cfg.HeartbeatAlways {
				stopHeartbeat = startAckHeartbeat(msg, cfg.HeartbeatInterval)
			}
			err := process(ctx, msg, handler, cfg)
			if stopHeartbeat != nil {
				stopHeartbeat()
			}

			switch {
			case err == nil:
				_ = msg.Ack()
			case errors.Is(err, ErrPermanent):
				log.Warn("message terminated (permanent)",
					logger.String("handler", handler.Name()),
					logger.String("subject", msg.Subject()),
					logger.String("error", err.Error()),
				)
				_ = msg.Term()
			default:
				log.Error("message failed (will retry)",
					logger.String("handler", handler.Name()),
					logger.String("subject", msg.Subject()),
					logger.String("error", err.Error()),
				)
				_ = msg.Nak()
			}
		}()
	})
	if err != nil {
		return fmt.Errorf("start consume: %w", err)
	}
	defer consumeCtx.Stop()

	<-ctx.Done()
	log.Info("consumer stopping, draining in-flight",
		logger.String("handler", handler.Name()))
	wg.Wait()
	log.Info("consumer stopped", logger.String("handler", handler.Name()))
	return ctx.Err()
}

// Run wraps Start in a reconnect/retry loop: if the consumer drops for any
// reason other than ctx cancellation, it waits Config.RetryBackoff and
// re-establishes. It returns only when ctx is cancelled.
func Run(ctx context.Context, nc *nats.Conn, handler EventHandler, cfg Config) {
	cfg.withDefaults()
	log := cfg.Logger
	for {
		if ctx.Err() != nil {
			return
		}
		if err := Start(ctx, nc, handler, cfg); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Error("consumer dropped, retrying",
				logger.String("handler", handler.Name()),
				logger.String("error", err.Error()),
			)
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.RetryBackoff):
			}
		}
	}
}

// process runs the idempotency gate then the handler. The returned error keeps
// its ErrPermanent wrapping (if any) so the caller can choose Term vs Nak.
func process(ctx context.Context, msg jetstream.Msg, handler EventHandler, cfg Config) error {
	natsMsg := &nats.Msg{
		Subject: msg.Subject(),
		Data:    msg.Data(),
		Header:  msg.Headers(),
	}
	log := cfg.Logger

	if cfg.Idempotency == nil {
		return handler.Handle(ctx, natsMsg)
	}

	eventID := extractEventID(natsMsg.Data)
	if eventID == "" {
		// No event_id: process without dedup rather than drop the message.
		log.Warn("no event_id, processing without idempotency",
			logger.String("handler", handler.Name()),
			logger.String("subject", natsMsg.Subject),
		)
		return handler.Handle(ctx, natsMsg)
	}

	processed, err := cfg.Idempotency.IsProcessed(ctx, handler.DurableName(), eventID)
	if err != nil {
		return fmt.Errorf("idempotency check: %w", err)
	}
	if processed {
		log.Info("duplicate event skipped",
			logger.String("handler", handler.Name()),
			logger.String("subject", natsMsg.Subject),
			logger.String("event_id", eventID),
		)
		return nil
	}

	if err := handler.Handle(ctx, natsMsg); err != nil {
		return err
	}

	if err := cfg.Idempotency.MarkProcessed(ctx, handler.DurableName(), eventID); err != nil {
		// Handler already succeeded; a mark failure is logged, not retried,
		// to avoid re-running side effects.
		log.Error("failed to record idempotency",
			logger.String("handler", handler.Name()),
			logger.String("event_id", eventID),
			logger.String("error", err.Error()),
		)
	}
	return nil
}

func extractEventID(data []byte) string {
	var env struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return ""
	}
	return env.EventID
}

func handlerConcurrency(h EventHandler) int {
	if ch, ok := h.(ConcurrentHandler); ok {
		if n := ch.MaxConcurrency(); n > 1 {
			return n
		}
	}
	return 1
}

// startAckHeartbeat sends InProgress() on interval to defer AckWait during
// long-running processing. The returned func stops the ticker.
func startAckHeartbeat(msg jetstream.Msg, interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_ = msg.InProgress()
			}
		}
	}()
	return func() { close(done) }
}

// ensurePipelineStream creates/updates the durable work-queue stream.
func ensurePipelineStream(ctx context.Context, js jetstream.JetStream, cfg Config) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.StreamName,
		Subjects:  cfg.StreamSubjects,
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
	})
	if err != nil {
		return fmt.Errorf("ensure stream %s: %w", cfg.StreamName, err)
	}
	return nil
}

// ensureDLQStream captures the MAX_DELIVERIES advisories JetStream emits when a
// message exhausts MaxDeliver — a persistent, inspectable dead-letter record.
func ensureDLQStream(ctx context.Context, js jetstream.JetStream, cfg Config) error {
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      cfg.DLQStreamName,
		Subjects:  []string{"$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"},
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    cfg.DLQMaxAge,
	})
	if err != nil {
		return fmt.Errorf("ensure DLQ stream %s: %w", cfg.DLQStreamName, err)
	}
	return nil
}
