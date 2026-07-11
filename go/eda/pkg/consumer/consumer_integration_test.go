//go:build integration

// Run: go test -tags=integration ./pkg/consumer/...
//
// Requires a local NATS server with JetStream enabled. Quick start:
//
//	docker run --rm -p 4222:4222 nats:2.10 -js
//
// These tests exercise the real JetStream redelivery semantics the brick
// exists to own: Ack on success, Term on permanent errors (no redelivery),
// and bounded transient retries that end up in the DLQ stream.
package consumer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/logger"
)

// testLogger routes brick logs into the test output for diagnostics.
type testLogger struct{ t *testing.T }

func (l testLogger) log(level, msg string, f []logger.Field) {
	l.t.Logf("[%s] %s %v", level, msg, f)
}
func (l testLogger) Debug(msg string, f ...logger.Field) { l.log("DEBUG", msg, f) }
func (l testLogger) Info(msg string, f ...logger.Field)  { l.log("INFO", msg, f) }
func (l testLogger) Warn(msg string, f ...logger.Field)  { l.log("WARN", msg, f) }
func (l testLogger) Error(msg string, f ...logger.Field) { l.log("ERROR", msg, f) }
func (l testLogger) Fatal(msg string, f ...logger.Field) { l.log("FATAL", msg, f) }

// testHandler is a configurable EventHandler: it counts invocations and returns
// whatever handleFn yields, so each test can drive Ack / Term / retry paths.
type testHandler struct {
	name     string
	subject  string
	durable  string
	maxDeliv int
	calls    int32
	handleFn func(ctx context.Context, msg *nats.Msg) error
}

func (h *testHandler) Name() string        { return h.name }
func (h *testHandler) Subject() string     { return h.subject }
func (h *testHandler) DurableName() string { return h.durable }
func (h *testHandler) MaxDeliver() int     { return h.maxDeliv }
func (h *testHandler) Handle(ctx context.Context, msg *nats.Msg) error {
	atomic.AddInt32(&h.calls, 1)
	return h.handleFn(ctx, msg)
}

// itSetup connects, builds a uniquely-named work-queue + DLQ stream (so tests
// don't collide), and returns a ready Config plus a publish helper. It skips
// the test if no NATS server is reachable.
func itSetup(t *testing.T, tag string) (*nats.Conn, Config, func(data string)) {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("nats server not reachable at %s: %v", nats.DefaultURL, err)
	}

	stream := "IT_CONSUMER_" + tag
	dlq := "IT_DLQ_" + tag
	subject := "itconsumer." + tag

	cfg := Config{
		StreamName: stream,
		// Per-tag subject so concurrent tests' streams never overlap (JetStream
		// rejects/misroutes streams that share subjects).
		StreamSubjects: []string{subject},
		DLQStreamName:  dlq,
		Logger:         testLogger{t},
		// Short waits so the bounded-retry test finishes quickly. BackOff must
		// stay shorter than the smallest MaxDeliver used here (3) — JetStream
		// requires MaxDeliver > len(BackOff).
		AckWait: 1 * time.Second,
		BackOff: []time.Duration{200 * time.Millisecond, 200 * time.Millisecond},
	}

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	// Clean any leftovers from a previous run, then publish helper.
	ctx := context.Background()
	_ = js.DeleteStream(ctx, stream)
	_ = js.DeleteStream(ctx, dlq)

	publish := func(data string) {
		// The stream is created by Start; ensure it exists before publishing
		// by retrying briefly.
		var perr error
		for i := 0; i < 50; i++ {
			if _, perr = js.Publish(ctx, subject, []byte(data)); perr == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		require.NoError(t, perr, "publish to %s", subject)
	}

	t.Cleanup(func() {
		_ = js.DeleteStream(context.Background(), stream)
		_ = js.DeleteStream(context.Background(), dlq)
		nc.Close()
	})
	return nc, cfg, publish
}

// runConsumer starts Run in the background and returns a cancel func.
func runConsumer(nc *nats.Conn, h EventHandler, cfg Config) (context.CancelFunc, *sync.WaitGroup) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		Run(ctx, nc, h, cfg)
	}()
	return cancel, &wg
}

// TestIntegration_AckOnSuccess: a handler returning nil is invoked once and the
// message is acked (not redelivered).
func TestIntegration_AckOnSuccess(t *testing.T) {
	nc, cfg, publish := itSetup(t, "ack")
	h := &testHandler{
		name: "ack", subject: "itconsumer.ack", durable: "ack-consumer", maxDeliv: 5,
		handleFn: func(context.Context, *nats.Msg) error { return nil },
	}
	cancel, wg := runConsumer(nc, h, cfg)
	defer func() { cancel(); wg.Wait() }()

	publish(`{"event_id":"e1"}`)

	require.Eventually(t, func() bool { return atomic.LoadInt32(&h.calls) == 1 },
		3*time.Second, 50*time.Millisecond, "handler should be called once")
	// Give any erroneous redelivery a chance to show up.
	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&h.calls), "must not be redelivered after Ack")
}

// TestIntegration_PermanentTerminates: a handler returning consumer.Permanent
// is invoked exactly once — the message is Term()'d, not redelivered, even with
// a high MaxDeliver.
func TestIntegration_PermanentTerminates(t *testing.T) {
	nc, cfg, publish := itSetup(t, "perm")
	h := &testHandler{
		name: "perm", subject: "itconsumer.perm", durable: "perm-consumer", maxDeliv: 5,
		handleFn: func(context.Context, *nats.Msg) error {
			return Permanent(fmt.Errorf("malformed, never retry"))
		},
	}
	cancel, wg := runConsumer(nc, h, cfg)
	defer func() { cancel(); wg.Wait() }()

	publish(`{"event_id":"e2"}`)

	require.Eventually(t, func() bool { return atomic.LoadInt32(&h.calls) == 1 },
		3*time.Second, 50*time.Millisecond, "handler should be called once")
	// With AckWait=1s and BackOff, a non-Term'd failure would redeliver within
	// ~2s. Term must prevent that.
	time.Sleep(2500 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&h.calls),
		"permanent error must Term the message, not redeliver it")
}

// TestIntegration_TransientRetriesThenDLQ: a handler always returning a plain
// error is redelivered up to MaxDeliver, after which JetStream emits a
// MAX_DELIVERIES advisory captured by the DLQ stream.
func TestIntegration_TransientRetriesThenDLQ(t *testing.T) {
	nc, cfg, publish := itSetup(t, "dlq")
	const maxDeliv = 3
	h := &testHandler{
		name: "dlq", subject: "itconsumer.dlq", durable: "dlq-consumer", maxDeliv: maxDeliv,
		handleFn: func(context.Context, *nats.Msg) error {
			return fmt.Errorf("transient, keep retrying")
		},
	}
	cancel, wg := runConsumer(nc, h, cfg)
	defer func() { cancel(); wg.Wait() }()

	publish(`{"event_id":"e3"}`)

	// Retried up to MaxDeliver, then no more.
	require.Eventually(t, func() bool { return atomic.LoadInt32(&h.calls) >= maxDeliv },
		10*time.Second, 100*time.Millisecond, "handler should be retried up to MaxDeliver")
	time.Sleep(1500 * time.Millisecond)
	assert.Equal(t, int32(maxDeliv), atomic.LoadInt32(&h.calls),
		"must stop after MaxDeliver attempts")

	// The DLQ stream should have captured the MAX_DELIVERIES advisory.
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		st, err := js.Stream(context.Background(), cfg.DLQStreamName)
		if err != nil {
			return false
		}
		info, err := st.Info(context.Background())
		if err != nil {
			return false
		}
		return info.State.Msgs >= 1
	}, 5*time.Second, 100*time.Millisecond, "DLQ stream should capture the dead letter")
}
