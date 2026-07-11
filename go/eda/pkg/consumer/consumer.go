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
//	Already-applied -> Ack()      ErrAlreadyDone sentinel
//	Bounded MaxDeliver            EventHandler.MaxDeliver()
//	Staged BackOff                Config.BackOff (sane default)
//	Dead-letter stream (DLQ)      advisory MAX_DELIVERIES stream
//	Heartbeat anti-AckWait        InProgress() ticker (immediate + interval)
//	Pre-claim idempotency         optional IdempotencyStore (claim/done/release)
//	Trace propagation             optional Config.TraceExtractor
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

// ErrAlreadyDone marks an error as "this effect was already applied" — the
// message is Ack()'d as an idempotent success rather than retried. Return it
// (or wrap it with AlreadyDone) when the handler detects that its side effect
// is already in place: e.g. a unique-constraint violation on an INSERT the
// handler is responsible for, or a state that a prior delivery already reached.
//
// This closes the residual race a pre-claim IdempotencyStore cannot: two claims
// expiring at the same instant, both handlers running, the second losing the
// INSERT. Without this sentinel that second delivery would Nak and redeliver
// forever until MaxDeliver dead-letters a message that in fact succeeded.
var ErrAlreadyDone = errors.New("already done")

// AlreadyDone wraps err as an idempotent-success signal (see ErrAlreadyDone).
// AlreadyDone(nil) returns nil.
func AlreadyDone(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrAlreadyDone, err)
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

// IdempotencyStore is an optional pre-claim dedup gate keyed by
// (durable, eventID). When supplied, the consumer claims an event BEFORE running
// the handler, so two replicas that both receive the same delivery cannot both
// run the (often expensive) handler. Inject nil to disable.
//
// The model is a three-state claim: none → in_progress → done.
//
//   - TryClaim atomically reserves the event. It returns true when THIS caller
//     won the claim and must run the handler. It returns false when another
//     replica holds a fresh in_progress claim or the event is already done — the
//     caller then skips and Acks. A claim older than ttl is treated as orphaned
//     (its replica died mid-handler) and is reclaimable, so processing is never
//     wedged forever.
//   - MarkDone transitions a won claim to done after the handler succeeded.
//     Later deliveries of the same event then skip.
//   - Release removes an in_progress claim after the handler failed, so the NATS
//     redelivery can reclaim and retry immediately instead of waiting out ttl.
//     It must only delete in_progress rows, never a done row.
//
// Writing the in_progress claim before running the handler is the whole point:
// it closes the race where two replicas both read "not done" and both run the
// handler. See ErrAlreadyDone for the residual same-instant-expiry race.
type IdempotencyStore interface {
	TryClaim(ctx context.Context, durable, eventID string, ttl time.Duration) (bool, error)
	MarkDone(ctx context.Context, durable, eventID string) error
	Release(ctx context.Context, durable, eventID string) error
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
	// StreamStorage is the storage type used when the work-queue stream is
	// created. It must match the storage of a pre-existing stream of the same
	// name — JetStream rejects an update that changes storage type (err 10052),
	// so an app that provisions the stream itself with MemoryStorage (e.g. in
	// tests) must set this accordingly. The zero value is FileStorage, so the
	// default needs no withDefaults entry.
	StreamStorage jetstream.StorageType
	// StreamRetention is the retention policy used when the stream is created.
	// Like StreamStorage it must match a pre-existing stream of the same name —
	// JetStream rejects an update that changes retention policy. Set it to
	// WorkQueuePolicy for a true work-queue (each message delivered to exactly
	// one consumer, removed on ack), or InterestPolicy as needed. The zero value
	// is LimitsPolicy (the default work-queue-of-events shape).
	StreamRetention jetstream.RetentionPolicy
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
	// ClaimTTL bounds how long an in_progress idempotency claim is honoured.
	// Past it, the claim is treated as orphaned (its replica died mid-handler)
	// and a later delivery may reclaim it. Size it well above the slowest
	// expected handler so a live handler is never reclaimed from under itself.
	// Only used when Idempotency is set. Default: 3 minutes.
	ClaimTTL time.Duration
	// TraceExtractor, when set, derives the handler's context from the message
	// headers — restoring the producer's trace context across the async NATS
	// boundary so publish→consume spans link up. It keeps this core package free
	// of any OpenTelemetry dependency: the otel wiring lives in an obs adapter
	// that supplies this hook. Default: nil (handler runs with the base context).
	TraceExtractor func(msg *nats.Msg) context.Context
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
	if c.ClaimTTL == 0 {
		c.ClaimTTL = 3 * time.Minute
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
			case errors.Is(err, ErrAlreadyDone):
				// The effect was already applied (idempotent success). Ack
				// instead of retrying — a redelivery would only fail the same
				// way until MaxDeliver dead-letters a message that succeeded.
				log.Info("message already applied (idempotent success)",
					logger.String("handler", handler.Name()),
					logger.String("subject", msg.Subject()),
				)
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

// process runs the pre-claim idempotency gate then the handler. The returned
// error keeps its ErrPermanent / ErrAlreadyDone wrapping (if any) so the caller
// can choose Term / Ack / Nak.
func process(ctx context.Context, msg jetstream.Msg, handler EventHandler, cfg Config) error {
	natsMsg := &nats.Msg{
		Subject: msg.Subject(),
		Data:    msg.Data(),
		Header:  msg.Headers(),
	}
	log := cfg.Logger

	// Restore the producer's trace context across the async boundary, if wired.
	if cfg.TraceExtractor != nil {
		ctx = cfg.TraceExtractor(natsMsg)
	}

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

	durable := handler.DurableName()

	// Pre-claim BEFORE running the handler. Losing the claim means another
	// replica owns it or it is already done → skip and Ack.
	claimed, err := cfg.Idempotency.TryClaim(ctx, durable, eventID, cfg.ClaimTTL)
	if err != nil {
		return fmt.Errorf("idempotency claim: %w", err)
	}
	if !claimed {
		log.Info("duplicate event skipped",
			logger.String("handler", handler.Name()),
			logger.String("subject", natsMsg.Subject),
			logger.String("event_id", eventID),
		)
		return nil
	}

	if herr := handler.Handle(ctx, natsMsg); herr != nil {
		// Handler failed: release the claim so redelivery can retry immediately
		// instead of waiting out ClaimTTL. On ErrAlreadyDone the effect is in
		// place (idempotent success) — mark done, don't release.
		if errors.Is(herr, ErrAlreadyDone) {
			if merr := cfg.Idempotency.MarkDone(ctx, durable, eventID); merr != nil {
				log.Error("failed to mark idempotency done after already-applied",
					logger.String("handler", handler.Name()),
					logger.String("event_id", eventID),
					logger.String("error", merr.Error()),
				)
			}
			return herr
		}
		if rerr := cfg.Idempotency.Release(ctx, durable, eventID); rerr != nil {
			log.Error("failed to release idempotency claim",
				logger.String("handler", handler.Name()),
				logger.String("event_id", eventID),
				logger.String("error", rerr.Error()),
			)
		}
		return herr
	}

	if err := cfg.Idempotency.MarkDone(ctx, durable, eventID); err != nil {
		// Handler already succeeded; a mark failure is logged, not retried,
		// to avoid re-running side effects. The stale in_progress claim expires
		// after ClaimTTL and a later delivery re-runs — acceptable at-least-once.
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

// startAckHeartbeat sends InProgress() immediately and then on interval to
// defer AckWait during long-running processing. The immediate t=0 signal
// matters: without it a handler slower than AckWait but faster than interval
// could be redelivered before the first tick ever fires. The returned func
// stops the ticker.
func startAckHeartbeat(msg jetstream.Msg, interval time.Duration) func() {
	// Immediate signal so a handler that outlives AckWait but finishes before
	// the first tick is still protected.
	_ = msg.InProgress()
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
		Storage:   cfg.StreamStorage,
		Retention: cfg.StreamRetention,
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
