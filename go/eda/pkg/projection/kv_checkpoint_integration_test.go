//go:build integration

// Run: go test -tags=integration ./pkg/projection/...
package projection

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

func TestIntegration_KVCheckpointStore(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	js, err := jetstream.New(nc)
	require.NoError(t, err)

	bucket := "projection-cp-" + uuid.NewString()[:8]
	store, err := NewKVCheckpointStore[string](ctx, js, bucket)
	require.NoError(t, err)
	defer func() { _ = js.DeleteKeyValue(ctx, bucket) }()

	// Load on empty bucket returns ErrCheckpointNotFound.
	_, err = store.Load(ctx, "proj-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCheckpointNotFound))

	cp := Checkpoint[string]{PerAggregate: map[string]int{"a": 5, "b": 12}}
	require.NoError(t, store.Save(ctx, "proj-1", cp))

	got, err := store.Load(ctx, "proj-1")
	require.NoError(t, err)
	assert.Equal(t, 5, got.PerAggregate["a"])
	assert.Equal(t, 12, got.PerAggregate["b"])

	// Overwrite.
	cp.PerAggregate["a"] = 42
	require.NoError(t, store.Save(ctx, "proj-1", cp))
	got, err = store.Load(ctx, "proj-1")
	require.NoError(t, err)
	assert.Equal(t, 42, got.PerAggregate["a"])
}
