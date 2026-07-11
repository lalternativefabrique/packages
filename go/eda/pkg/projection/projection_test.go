package projection

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

type pinged struct{ N int }

func (pinged) EventKind() string { return "pinged" }

// fakeSource is a tiny in-memory EventSource for tests.
type fakeSource struct {
	mu      sync.Mutex
	events  map[string][]ddd.EventEnvelope[string]
	subs    []func(context.Context, ddd.EventEnvelope[string]) error
}

func newFakeSource() *fakeSource {
	return &fakeSource{events: make(map[string][]ddd.EventEnvelope[string])}
}

func (f *fakeSource) AllAggregateIDs(_ context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := make([]string, 0, len(f.events))
	for k := range f.events {
		ids = append(ids, k)
	}
	return ids, nil
}

func (f *fakeSource) LoadFromVersion(_ context.Context, id string, from int) ([]ddd.EventEnvelope[string], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ddd.EventEnvelope[string], 0)
	for _, e := range f.events[id] {
		if e.AggregateVersion > from {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeSource) Subscribe(ctx context.Context, h func(context.Context, ddd.EventEnvelope[string]) error) error {
	f.mu.Lock()
	f.subs = append(f.subs, h)
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeSource) emit(t *testing.T, id string, n int) ddd.EventEnvelope[string] {
	t.Helper()
	f.mu.Lock()
	cur := len(f.events[id])
	env := ddd.NewEnvelope[string](
		ddd.FixedClock{T: time.Unix(1700000000, 0).UTC()},
		"Pinger", id, cur+1, pinged{N: n},
	)
	f.events[id] = append(f.events[id], env)
	subs := append([]func(context.Context, ddd.EventEnvelope[string]) error(nil), f.subs...)
	f.mu.Unlock()
	for _, s := range subs {
		_ = s(context.Background(), env)
	}
	return env
}

// recordingProjector keeps the sum of pinged.N per aggregate.
type recordingProjector struct {
	mu  sync.Mutex
	sum map[string]int
}

func (p *recordingProjector) Name() string { return "ping-sum" }

func (p *recordingProjector) Apply(_ context.Context, env ddd.EventEnvelope[string]) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sum == nil {
		p.sum = map[string]int{}
	}
	p.sum[env.AggregateID] += env.Payload.(pinged).N
	return nil
}

func TestManager_CatchUpFromEmpty(t *testing.T) {
	src := newFakeSource()
	src.emit(t, "a", 3)
	src.emit(t, "a", 4)
	src.emit(t, "b", 10)

	proj := &recordingProjector{}
	mgr := NewManager[string](src, NewInMemoryCheckpointStore[string](), proj, ManagerConfig{})

	require.NoError(t, mgr.CatchUp(context.Background()))

	assert.Equal(t, 7, proj.sum["a"])
	assert.Equal(t, 10, proj.sum["b"])
}

func TestManager_CatchUpRespectsCheckpoint(t *testing.T) {
	src := newFakeSource()
	src.emit(t, "a", 1)
	src.emit(t, "a", 2)

	cps := NewInMemoryCheckpointStore[string]()
	// Pretend we already processed v1.
	require.NoError(t, cps.Save(context.Background(), "ping-sum", Checkpoint[string]{
		PerAggregate: map[string]int{"a": 1},
	}))

	proj := &recordingProjector{}
	mgr := NewManager[string](src, cps, proj, ManagerConfig{})
	require.NoError(t, mgr.CatchUp(context.Background()))

	assert.Equal(t, 2, proj.sum["a"], "only v2 should be applied")
}

func TestManager_LiveTail(t *testing.T) {
	src := newFakeSource()

	proj := &recordingProjector{}
	mgr := NewManager[string](src, NewInMemoryCheckpointStore[string](), proj, ManagerConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- mgr.RunLive(ctx) }()

	// Give Subscribe time to register.
	time.Sleep(20 * time.Millisecond)

	src.emit(t, "a", 5)
	src.emit(t, "a", 7)
	time.Sleep(20 * time.Millisecond)

	proj.mu.Lock()
	assert.Equal(t, 12, proj.sum["a"])
	proj.mu.Unlock()

	cancel()
	<-done
}

func TestManager_Idempotency(t *testing.T) {
	src := newFakeSource()
	src.emit(t, "a", 5)
	src.emit(t, "a", 6)

	cps := NewInMemoryCheckpointStore[string]()
	proj := &recordingProjector{}
	mgr := NewManager[string](src, cps, proj, ManagerConfig{})

	require.NoError(t, mgr.CatchUp(context.Background()))
	require.NoError(t, mgr.CatchUp(context.Background())) // second pass should be a no-op

	assert.Equal(t, 11, proj.sum["a"])
}
