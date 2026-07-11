package outbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

type pinged struct{ N int }

func (pinged) EventKind() string { return "pinged" }

func mkEnv(t *testing.T, v int) ddd.EventEnvelope[string] {
	t.Helper()
	return ddd.NewEnvelope[string](
		ddd.FixedClock{T: time.Unix(1700000000, 0).UTC()},
		"Pinger",
		"agg",
		v,
		pinged{N: v},
	)
}

func TestRelay_HappyPath(t *testing.T) {
	store := NewInMemoryStore[string](nil)
	var published int32
	pub := PublisherFunc[string](func(_ context.Context, _ ddd.EventEnvelope[string]) error {
		atomic.AddInt32(&published, 1)
		return nil
	})
	relay := NewRelay[string](store, pub, RelayConfig{BatchSize: 10})

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		_, err := store.Enqueue(ctx, mkEnv(t, i))
		require.NoError(t, err)
	}

	n, err := relay.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, int32(3), atomic.LoadInt32(&published))

	for _, r := range store.Snapshot() {
		assert.Equal(t, StatusDispatched, r.Status)
		assert.Equal(t, 1, r.Attempts)
	}
}

func TestRelay_FailureMarksFailedTerminal(t *testing.T) {
	store := NewInMemoryStore[string](nil)
	pub := PublisherFunc[string](func(_ context.Context, _ ddd.EventEnvelope[string]) error {
		return errors.New("transport down")
	})
	relay := NewRelay[string](store, pub, RelayConfig{BatchSize: 10, MaxAttempts: 2})

	ctx := context.Background()
	_, err := store.Enqueue(ctx, mkEnv(t, 1))
	require.NoError(t, err)

	// First pass: 1 attempt, still pending.
	_, err = relay.RunOnce(ctx)
	require.NoError(t, err)
	snap := store.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, StatusPending, snap[0].Status)
	assert.Equal(t, 1, snap[0].Attempts)

	// Second pass: 2 attempts → terminal (MaxAttempts=2).
	_, err = relay.RunOnce(ctx)
	require.NoError(t, err)
	snap = store.Snapshot()
	assert.Equal(t, StatusFailed, snap[0].Status)
	assert.Equal(t, 2, snap[0].Attempts)
	assert.Contains(t, snap[0].LastError, "transport down")
}

func TestRelay_RunCancellation(t *testing.T) {
	store := NewInMemoryStore[string](nil)
	pub := PublisherFunc[string](func(_ context.Context, _ ddd.EventEnvelope[string]) error { return nil })
	relay := NewRelay[string](store, pub, RelayConfig{BatchSize: 10, PollInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("relay did not stop after cancellation")
	}
}

func TestInMemoryStore_FetchPendingRespectsLimit(t *testing.T) {
	store := NewInMemoryStore[string](nil)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		_, err := store.Enqueue(ctx, mkEnv(t, i))
		require.NoError(t, err)
	}

	got, err := store.FetchPending(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, got, 3)
}
