//go:build integration

// Run: go test -tags=integration ./pkg/kvstore/...
package kvstore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type endpointEntity struct {
	ID       string
	TenantID string
	Status   string
}

func connectOrSkip(t *testing.T) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(nats.DefaultURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("nats server not reachable at %s: %v", nats.DefaultURL, err)
	}
	return nc
}

func newStore(t *testing.T) (*Store[string, endpointEntity], func()) {
	t.Helper()
	nc := connectOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	js, err := jetstream.New(nc)
	require.NoError(t, err)
	bucket := "kvstore-test-" + uuid.NewString()[:8]
	store, err := Open[string, endpointEntity](ctx, js, bucket, jetstream.MemoryStorage)
	require.NoError(t, err)
	return store, func() {
		_ = js.DeleteKeyValue(ctx, bucket)
		cancel()
		nc.Close()
	}
}

func TestIntegration_PutGetDelete(t *testing.T) {
	store, cleanup := newStore(t)
	defer cleanup()
	ctx := context.Background()

	// Get on empty → ErrNotFound.
	_, err := store.Get(ctx, "e1")
	assert.ErrorIs(t, err, ErrNotFound)

	// Put then Get round-trips.
	e := endpointEntity{ID: "e1", TenantID: "t1", Status: "active"}
	require.NoError(t, store.Put(ctx, "e1", e))
	got, err := store.Get(ctx, "e1")
	require.NoError(t, err)
	assert.Equal(t, e, got)

	// Delete then Get → ErrNotFound; deleting again is a no-op.
	require.NoError(t, store.Delete(ctx, "e1"))
	_, err = store.Get(ctx, "e1")
	assert.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, store.Delete(ctx, "e1"))
}

func TestIntegration_ListAndFilter(t *testing.T) {
	store, cleanup := newStore(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "e1", endpointEntity{ID: "e1", TenantID: "t1", Status: "active"}))
	require.NoError(t, store.Put(ctx, "e2", endpointEntity{ID: "e2", TenantID: "t1", Status: "disabled"}))
	require.NoError(t, store.Put(ctx, "e3", endpointEntity{ID: "e3", TenantID: "t2", Status: "active"}))

	all, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// The "ActiveByTenant" query, generalized: filter by tenant + status.
	activeT1, err := store.Filter(ctx, func(e endpointEntity) bool {
		return e.TenantID == "t1" && e.Status == "active"
	})
	require.NoError(t, err)
	require.Len(t, activeT1, 1)
	assert.Equal(t, "e1", activeT1[0].ID)
}

func TestIntegration_UpdateOptimisticConcurrency(t *testing.T) {
	store, cleanup := newStore(t)
	defer cleanup()
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "e1", endpointEntity{ID: "e1", Status: "active"}))

	// Update mutates in place.
	require.NoError(t, store.Update(ctx, "e1", func(e endpointEntity) (endpointEntity, error) {
		e.Status = "disabled"
		return e, nil
	}))
	got, err := store.Get(ctx, "e1")
	require.NoError(t, err)
	assert.Equal(t, "disabled", got.Status)

	// Update on a missing key → ErrNotFound.
	err = store.Update(ctx, "missing", func(e endpointEntity) (endpointEntity, error) { return e, nil })
	assert.ErrorIs(t, err, ErrNotFound)
}
