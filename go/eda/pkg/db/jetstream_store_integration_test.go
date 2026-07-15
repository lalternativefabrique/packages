//go:build integration

// Run: go test -tags=integration ./pkg/db/...
//
// Requires a local NATS server with JetStream enabled. Quick start:
//
//	docker run --rm -p 4222:4222 nats:2.10 -js
package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

type itCreated struct {
	Name string `json:"name"`
}

func (itCreated) EventKind() string { return "it.created" }

type itRenamed struct {
	Name string `json:"name"`
}

func (itRenamed) EventKind() string { return "it.renamed" }

func newRegistry() *PayloadRegistry {
	reg := NewPayloadRegistry()
	reg.Register("it.created", func() ddd.EventPayload { return &itCreated{} })
	reg.Register("it.renamed", func() ddd.EventPayload { return &itRenamed{} })
	return reg
}

func connectOrSkip(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("nats server not reachable at %s: %v", url, err)
	}
	return nc
}

func uniqueStream(prefix string) string {
	return prefix + "-" + uuid.NewString()[:8]
}

func mkEnv(version int, payload ddd.EventPayload) ddd.EventEnvelope[string] {
	return mkEnvFor("agg-1", version, payload)
}

func mkEnvFor(aggregateID string, version int, payload ddd.EventPayload) ddd.EventEnvelope[string] {
	return ddd.NewEnvelope[string](
		ddd.SystemClock{},
		"Test",
		aggregateID,
		version,
		payload,
	)
}

func TestIntegration_JetStreamStore_SaveLoad(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	stream := uniqueStream("EVENTS")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewJetStreamStore[string](ctx, nc, JetStreamStoreConfig[string]{
		StreamName:            stream,
		SubjectPrefix:         "events",
		AggregateType:         "Test",
		Payloads:              newRegistry(),
		IDs:                   StringIDCodec{},
		CreateStreamIfMissing: true,
		MaxAge:                10 * time.Minute,
	})
	require.NoError(t, err)
	defer cleanupStream(ctx, nc, stream)

	envs := []ddd.EventEnvelope[string]{
		mkEnv(1, itCreated{Name: "first"}),
		mkEnv(2, itRenamed{Name: "second"}),
		mkEnv(3, itRenamed{Name: "third"}),
	}
	require.NoError(t, store.Save(ctx, "agg-1", 0, envs))

	got, err := store.Load(ctx, "agg-1")
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "it.created", got[0].EventType)
	assert.Equal(t, "it.renamed", got[1].EventType)
	assert.Equal(t, 3, got[2].AggregateVersion)
}

// Regression: a second Save introducing a new event type publishes to a
// subject that has never seen a message. The store must not reject it
// (v0.3.1 failed here with "wrong last sequence: 0" because the OCC
// expectation was anchored on the per-type subject).
func TestIntegration_JetStreamStore_SecondEventType(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	stream := uniqueStream("EVENTS")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewJetStreamStore[string](ctx, nc, JetStreamStoreConfig[string]{
		StreamName:            stream,
		SubjectPrefix:         "events",
		AggregateType:         "Test",
		Payloads:              newRegistry(),
		IDs:                   StringIDCodec{},
		CreateStreamIfMissing: true,
	})
	require.NoError(t, err)
	defer cleanupStream(ctx, nc, stream)

	require.NoError(t, store.Save(ctx, "agg-multi", 0, []ddd.EventEnvelope[string]{
		mkEnvFor("agg-multi", 1, itCreated{Name: "first"}),
	}))

	// New event type => new subject with no prior message.
	require.NoError(t, store.Save(ctx, "agg-multi", 1, []ddd.EventEnvelope[string]{
		mkEnvFor("agg-multi", 2, itRenamed{Name: "second"}),
	}))

	got, err := store.Load(ctx, "agg-multi")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "it.renamed", got[1].EventType)
	assert.Equal(t, 2, got[1].AggregateVersion)
}

func TestIntegration_JetStreamStore_OCCConflict(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	stream := uniqueStream("EVENTS")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewJetStreamStore[string](ctx, nc, JetStreamStoreConfig[string]{
		StreamName:            stream,
		SubjectPrefix:         "events",
		AggregateType:         "Test",
		Payloads:              newRegistry(),
		IDs:                   StringIDCodec{},
		CreateStreamIfMissing: true,
	})
	require.NoError(t, err)
	defer cleanupStream(ctx, nc, stream)

	require.NoError(t, store.Save(ctx, "agg-2", 0, []ddd.EventEnvelope[string]{mkEnv(1, itCreated{Name: "x"})}))

	// Second writer thinks the aggregate is fresh — should conflict.
	err = store.Save(ctx, "agg-2", 0, []ddd.EventEnvelope[string]{mkEnv(1, itCreated{Name: "racing"})})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConcurrencyConflict))
}

func TestIntegration_JetStreamStore_Idempotent(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	stream := uniqueStream("EVENTS")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewJetStreamStore[string](ctx, nc, JetStreamStoreConfig[string]{
		StreamName:            stream,
		SubjectPrefix:         "events",
		AggregateType:         "Test",
		Payloads:              newRegistry(),
		IDs:                   StringIDCodec{},
		CreateStreamIfMissing: true,
	})
	require.NoError(t, err)
	defer cleanupStream(ctx, nc, stream)

	env := mkEnv(1, itCreated{Name: "once"})
	require.NoError(t, store.Save(ctx, "agg-3", 0, []ddd.EventEnvelope[string]{env}))

	// Re-publishing the same EventID within the dedupe window is a no-op
	// at the server, but our local OCC will reject (expectedVersion=0 yet
	// the store holds v1). We assert the OCC error explicitly.
	err = store.Save(ctx, "agg-3", 0, []ddd.EventEnvelope[string]{env})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrConcurrencyConflict))
}

func TestIntegration_JetStreamStore_Subscribe(t *testing.T) {
	nc := connectOrSkip(t)
	defer nc.Close()

	stream := uniqueStream("EVENTS")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	store, err := NewJetStreamStore[string](ctx, nc, JetStreamStoreConfig[string]{
		StreamName:            stream,
		SubjectPrefix:         "events",
		AggregateType:         "Test",
		Payloads:              newRegistry(),
		IDs:                   StringIDCodec{},
		CreateStreamIfMissing: true,
	})
	require.NoError(t, err)
	defer cleanupStream(ctx, nc, stream)

	received := make(chan ddd.EventEnvelope[string], 4)
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	go func() {
		_ = store.Subscribe(subCtx, func(_ context.Context, env ddd.EventEnvelope[string]) error {
			received <- env
			return nil
		})
	}()

	time.Sleep(150 * time.Millisecond) // let the consumer attach

	require.NoError(t, store.Save(ctx, "agg-4", 0, []ddd.EventEnvelope[string]{
		mkEnv(1, itCreated{Name: "live"}),
	}))

	select {
	case env := <-received:
		assert.Equal(t, "it.created", env.EventType)
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive subscribed event in time")
	}
}

func cleanupStream(ctx context.Context, nc *nats.Conn, name string) {
	js, err := jetstream.New(nc)
	if err != nil {
		return
	}
	_ = js.DeleteStream(ctx, name)
}
