package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

const (
	durableName = "webhooks-outbox-worker"
	maxDeliver  = 8 // total attempts, ~24h with the backoff schedule below
)

// Worker posts webhook delivery jobs to subscriber URLs. It is a
// consumer.EventHandler: the durable pull, ack/nak/term and staged backoff come
// from pkg/consumer via consumer.Run. Handle classifies each delivery outcome —
// OK (ack), permanent (Term), transient (Nak with backoff) — by returning nil,
// a Permanent error, or a plain error.
type Worker struct {
	repo       repository.EndpointRepository
	dispatcher providers.HTTPDispatcher
}

func NewWorker(repo repository.EndpointRepository, dispatcher providers.HTTPDispatcher) *Worker {
	return &Worker{repo: repo, dispatcher: dispatcher}
}

// EventHandler contract ------------------------------------------------------

func (*Worker) Name() string        { return durableName }
func (*Worker) Subject() string     { return "outbox.webhook.>" }
func (*Worker) DurableName() string { return durableName }
func (*Worker) MaxDeliver() int     { return maxDeliver }

// BackOff is the staged redelivery schedule the consumer uses for transient
// failures. Cumulative timeline: 30s, 2m, 10m, 30m, 1h, 4h, 8h (then terminal).
func BackOff() []time.Duration {
	return []time.Duration{
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
		30 * time.Minute,
		1 * time.Hour,
		4 * time.Hour,
		8 * time.Hour,
	}
}

// Handle dispatches one job and records the attempt on the endpoint aggregate.
// The delivery attempt number comes from the JetStream metadata (NumDelivered),
// so the recorded attempt count matches the redelivery count.
func (w *Worker) Handle(ctx context.Context, m *nats.Msg) error {
	job, err := DecodeJob(m.Data)
	if err != nil {
		return consumer.Permanent(fmt.Errorf("decode job: %w", err))
	}

	deliveries := deliveryAttempt(m)
	dispatchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	res := w.dispatcher.Dispatch(dispatchCtx, job)
	outcome := classify(res)
	errMsg := ""
	if res.Err != nil {
		errMsg = res.Err.Error()
	}

	switch outcome {
	case aggregate.OutcomeOK:
		if err := w.recordAttempt(ctx, job, deliveries, res.StatusCode, outcome, errMsg, res.DurationMS); err != nil {
			return fmt.Errorf("record attempt %s: %w", job.DeliveryID, err) // transient → nak
		}
		if err := w.markSucceeded(ctx, job, deliveries); err != nil {
			return fmt.Errorf("mark succeeded %s: %w", job.DeliveryID, err)
		}
		return nil

	case aggregate.OutcomePermanent:
		_ = w.recordAttempt(ctx, job, deliveries, res.StatusCode, outcome, errMsg, res.DurationMS)
		_ = w.markFailed(ctx, job, deliveries, fmt.Sprintf("permanent: %s", errMsg))
		return consumer.Permanent(fmt.Errorf("permanent delivery failure: %s", errMsg))

	default: // aggregate.OutcomeTransient
		_ = w.recordAttempt(ctx, job, deliveries, res.StatusCode, outcome, errMsg, res.DurationMS)
		if deliveries >= maxDeliver {
			_ = w.markFailed(ctx, job, deliveries, fmt.Sprintf("retries exhausted after %d attempts: %s", deliveries, errMsg))
			// Let the consumer dead-letter it: return permanent so it Terms now
			// rather than waiting for the (already reached) MaxDeliver.
			return consumer.Permanent(fmt.Errorf("retries exhausted: %s", errMsg))
		}
		return fmt.Errorf("transient delivery failure: %s", errMsg) // nak + staged backoff
	}
}

var _ consumer.EventHandler = (*Worker)(nil)

func deliveryAttempt(m *nats.Msg) int {
	md, err := m.Metadata()
	if err != nil {
		return 1
	}
	return int(md.NumDelivered)
}

func (w *Worker) recordAttempt(ctx context.Context, job providers.DeliveryJob, attempt int, status int, outcome aggregate.DeliveryOutcome, errMsg string, durationMS int64) error {
	e, err := w.repo.Load(ctx, job.EndpointID)
	if err != nil {
		return err
	}
	if e.Version == 0 {
		return nil // endpoint vanished mid-flight; nothing to record
	}
	e.RecordDeliveryAttempt(job.DeliveryID, job.EventType, job.SourceEventID, attempt, status, outcome, errMsg, durationMS)
	return w.repo.Save(ctx, e)
}

func (w *Worker) markSucceeded(ctx context.Context, job providers.DeliveryJob, attempts int) error {
	e, err := w.repo.Load(ctx, job.EndpointID)
	if err != nil {
		return err
	}
	if e.Version == 0 {
		return nil
	}
	e.MarkDeliverySucceeded(job.DeliveryID, job.EventType, job.SourceEventID, attempts)
	return w.repo.Save(ctx, e)
}

func (w *Worker) markFailed(ctx context.Context, job providers.DeliveryJob, attempts int, reason string) error {
	e, err := w.repo.Load(ctx, job.EndpointID)
	if err != nil {
		return err
	}
	if e.Version == 0 {
		return nil
	}
	e.MarkDeliveryFailed(job.DeliveryID, job.EventType, job.SourceEventID, attempts, reason)
	return w.repo.Save(ctx, e)
}
