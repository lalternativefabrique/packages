//go:build integration

// Run: go test -tags=integration ./pkg/processmanager/...
package processmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func connectOrSkip(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("nats server not reachable at %s: %v", nats.DefaultURL, err)
	}
	return nc
}

type itState struct {
	N int
}

func TestIntegration_KVStateStore_HappyPath(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	bucket := "pm-state-" + uuid.NewString()[:8]
	store, err := NewKVStateStore[itState](ctx, js, bucket)
	require.NoError(t, err)
	defer func() { _ = js.DeleteKeyValue(ctx, bucket) }()

	// Missing key.
	_, err = store.Load(ctx, "def", "i-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInstanceNotFound))

	// First save (Create path).
	require.NoError(t, store.Save(ctx, "def", "i-1", 0, Stored[itState]{
		Version: 1, State: itState{N: 1}, LastEventID: "e-1",
	}))

	got, err := store.Load(ctx, "def", "i-1")
	require.NoError(t, err)
	assert.Equal(t, 1, got.Version)
	assert.Equal(t, 1, got.State.N)

	// Update path: must round-trip through Load to capture KV revision.
	require.NoError(t, store.Save(ctx, "def", "i-1", 1, Stored[itState]{
		Version: 2, State: itState{N: 2}, LastEventID: "e-2",
	}))
	got, err = store.Load(ctx, "def", "i-1")
	require.NoError(t, err)
	assert.Equal(t, 2, got.State.N)
}

func TestIntegration_KVStateStore_ConcurrencyConflict(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)
	bucket := "pm-state-" + uuid.NewString()[:8]
	a, err := NewKVStateStore[itState](ctx, js, bucket)
	require.NoError(t, err)
	defer func() { _ = js.DeleteKeyValue(ctx, bucket) }()

	require.NoError(t, a.Save(ctx, "def", "i-2", 0, Stored[itState]{Version: 1, State: itState{N: 1}}))

	// Two engines load the same revision...
	require.NoError(t, mustLoad(a, ctx, "def", "i-2"))
	b, err := NewKVStateStore[itState](ctx, js, bucket)
	require.NoError(t, err)
	require.NoError(t, mustLoad(b, ctx, "def", "i-2"))

	// First one saves (revision bumps).
	require.NoError(t, a.Save(ctx, "def", "i-2", 1, Stored[itState]{Version: 2, State: itState{N: 2}}))

	// Second one tries to save with the stale revision.
	err = b.Save(ctx, "def", "i-2", 1, Stored[itState]{Version: 2, State: itState{N: 99}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConcurrencyConflict))
}

func mustLoad[S any](s *KVStateStore[S], ctx context.Context, defName, id string) error {
	_, err := s.Load(ctx, defName, id)
	return err
}
