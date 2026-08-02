//go:build integration

// Exercises the outbox delivery path end-to-end after the worker was moved onto
// the shared eda consumer: publish a delivery job onto the WEBHOOK_OUTBOX
// work-queue via the real publisher, and assert the worker dispatches it to the
// subscriber URL (a test HTTP server). This is the piece the read-model net does
// not cover.
package webhooks_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/consumer"

	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/lalternative/packages/go/webhooks/infrastructure"
	"github.com/lalternative/packages/go/webhooks/infrastructure/outbox"
)

// capturingDispatcher records the jobs the worker hands it and reports success,
// so the test verifies the worker's consume→decode→dispatch routing without the
// real HTTPDispatcher's SSRF guard (loopback is refused) — the HTTP behavior is
// covered separately by http_dispatcher_test.go.
type capturingDispatcher struct{ got chan providers.DeliveryJob }

func (d *capturingDispatcher) Dispatch(_ context.Context, job providers.DeliveryJob) providers.HTTPResult {
	select {
	case d.got <- job:
	default:
	}
	return providers.HTTPResult{StatusCode: 200}
}

func TestIntegration_Webhooks_OutboxWorkerDispatchesJob(t *testing.T) {
	nc, pool := backendsOrSkip(t)
	defer nc.Close()
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Clean the outbox + event streams so the worker starts empty.
	js, err := nc.JetStream()
	require.NoError(t, err)
	_ = js.PurgeStream(outbox.StreamName)
	_ = js.PurgeStream(infrastructure.StreamName)

	// Provision the work-queue stream and start the real worker on consumer.Run,
	// exactly as bootstrap wires it, with a capturing dispatcher.
	require.NoError(t, outbox.EnsureStream(mustJSv1(t, nc)))
	disp := &capturingDispatcher{got: make(chan providers.DeliveryJob, 1)}
	worker := outbox.NewWorker(newNoopRepo(), disp)
	go consumer.Run(ctx, nc, worker, consumer.Config{
		StreamName:      outbox.StreamName,
		StreamSubjects:  []string{"outbox.webhook.>"},
		StreamRetention: workQueueRetention(),
		BackOff:         outbox.BackOff(),
	})
	time.Sleep(300 * time.Millisecond)

	// Publish a delivery job onto the outbox via the real publisher.
	pub, err := outbox.NewNATSOutboxPublisher(mustJSv1(t, nc))
	require.NoError(t, err)
	want := providers.DeliveryJob{
		DeliveryID: uuid.NewString(),
		EndpointID: uuid.NewString(),
		TenantID:   "tenant-A",
		URL:        "https://subscriber.example/hook",
		Secret:     "s3cr3t",
		EventType:  "email.sent",
		Payload:    []byte(`{"hello":"world"}`),
	}
	require.NoError(t, pub.Publish(ctx, want))

	// The worker should decode and hand the job to the dispatcher within a few
	// seconds — proving consume/decode/route across the WorkQueue stream.
	select {
	case got := <-disp.got:
		require.Equal(t, want.DeliveryID, got.DeliveryID)
		require.Equal(t, want.URL, got.URL)
		require.Contains(t, string(got.Payload), "hello")
	case <-time.After(8 * time.Second):
		t.Fatal("worker never dispatched the job")
	}
}
