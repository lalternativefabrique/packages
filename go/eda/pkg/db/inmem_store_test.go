package db

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

type tCreated struct {
	Name string `json:"name"`
}

func (tCreated) EventKind() string { return "test.created" }

type tRenamed struct {
	Name string `json:"name"`
}

func (tRenamed) EventKind() string { return "test.renamed" }

func env(t *testing.T, aggID string, version int, payload ddd.EventPayload) ddd.EventEnvelope[string] {
	t.Helper()
	return ddd.NewEnvelope[string](
		ddd.FixedClock{T: time.Unix(1700000000, 0).UTC()},
		"Test",
		aggID,
		version,
		payload,
	)
}

func TestInMemoryStore_SaveAndLoad(t *testing.T) {
	s := NewInMemoryStore[string]()
	ctx := context.Background()

	envs := []ddd.EventEnvelope[string]{
		env(t, "a-1", 1, tCreated{Name: "first"}),
		env(t, "a-1", 2, tRenamed{Name: "second"}),
	}
	require.NoError(t, s.Save(ctx, "a-1", 0, envs))

	got, err := s.Load(ctx, "a-1")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "test.created", got[0].EventType)
	assert.Equal(t, "test.renamed", got[1].EventType)
}

func TestInMemoryStore_OCCConflict(t *testing.T) {
	s := NewInMemoryStore[string]()
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "a-2", 0, []ddd.EventEnvelope[string]{env(t, "a-2", 1, tCreated{Name: "x"})}))

	// expectedVersion 0 but the aggregate already has v1.
	err := s.Save(ctx, "a-2", 0, []ddd.EventEnvelope[string]{env(t, "a-2", 2, tRenamed{Name: "y"})})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConcurrencyConflict))
}

func TestInMemoryStore_NonContiguousVersionsRejected(t *testing.T) {
	s := NewInMemoryStore[string]()
	ctx := context.Background()

	err := s.Save(ctx, "a-3", 0, []ddd.EventEnvelope[string]{
		env(t, "a-3", 1, tCreated{Name: "x"}),
		env(t, "a-3", 3, tRenamed{Name: "y"}), // skip
	})
	require.Error(t, err)
}

func TestInMemoryStore_LoadFromVersion(t *testing.T) {
	s := NewInMemoryStore[string]()
	ctx := context.Background()
	require.NoError(t, s.Save(ctx, "a-4", 0, []ddd.EventEnvelope[string]{
		env(t, "a-4", 1, tCreated{Name: "x"}),
		env(t, "a-4", 2, tRenamed{Name: "y"}),
		env(t, "a-4", 3, tRenamed{Name: "z"}),
	}))

	got, err := s.LoadFromVersion(ctx, "a-4", 1)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, 2, got[0].AggregateVersion)
	assert.Equal(t, 3, got[1].AggregateVersion)
}

func TestInMemoryStore_AggregateNotFound(t *testing.T) {
	s := NewInMemoryStore[string]()
	_, err := s.Load(context.Background(), "missing")
	assert.True(t, errors.Is(err, ErrAggregateNotFound))
}

func TestInMemoryStore_Subscribe(t *testing.T) {
	s := NewInMemoryStore[string]()
	ctx := context.Background()

	var received int32
	require.NoError(t, s.Subscribe(ctx, func(_ context.Context, _ ddd.EventEnvelope[string]) error {
		atomic.AddInt32(&received, 1)
		return nil
	}))

	require.NoError(t, s.Save(ctx, "a-5", 0, []ddd.EventEnvelope[string]{
		env(t, "a-5", 1, tCreated{Name: "x"}),
		env(t, "a-5", 2, tRenamed{Name: "y"}),
	}))

	assert.Equal(t, int32(2), atomic.LoadInt32(&received))
}

func TestInMemorySnapshotStore_RoundTrip(t *testing.T) {
	s := NewInMemorySnapshotStore[string]()
	ctx := context.Background()

	snap := Snapshot[string]{
		AggregateID:      "agg-1",
		AggregateType:    "Test",
		AggregateVersion: 5,
		State:            []byte(`{"count":5}`),
	}
	require.NoError(t, s.Save(ctx, snap))

	got, err := s.Load(ctx, "agg-1")
	require.NoError(t, err)
	assert.Equal(t, snap.AggregateVersion, got.AggregateVersion)
	assert.JSONEq(t, `{"count":5}`, string(got.State))

	require.NoError(t, s.Delete(ctx, "agg-1"))
	_, err = s.Load(ctx, "agg-1")
	assert.True(t, errors.Is(err, ErrSnapshotNotFound))
}
