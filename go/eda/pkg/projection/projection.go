// Package projection drives event-store-backed read models with two
// phases under one API: catch-up (replay history from a checkpoint) then
// live tail (subscribe to new events). State persistence is done by the
// projector itself; only the checkpoint (last seen position) lives in a
// CheckpointStore.
//
// The Manager is event-store-agnostic. Plug any store that satisfies the
// small EventSource interface — pkg/db.InMemoryStore and JetStreamStore
// both qualify with thin adapters.
package projection

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Projector applies envelopes to its read model. Implementations must be
// idempotent w.r.t. AggregateVersion — replays must produce the same state.
type Projector[ID comparable] interface {
	// Name uniquely identifies this projector. The Manager uses it as the
	// checkpoint key.
	Name() string
	// Apply mutates the read model for a single envelope.
	Apply(ctx context.Context, env ddd.EventEnvelope[ID]) error
}

// Checkpoint records the last-seen aggregate version per aggregate ID. The
// Manager uses it to resume after restarts.
type Checkpoint[ID comparable] struct {
	// PerAggregate maps aggregate IDs to their last-applied version.
	PerAggregate map[ID]int
}

// CheckpointStore persists Checkpoints. Implementations must be
// goroutine-safe.
type CheckpointStore[ID comparable] interface {
	Load(ctx context.Context, projectorName string) (Checkpoint[ID], error)
	Save(ctx context.Context, projectorName string, cp Checkpoint[ID]) error
}

// EventSource is the minimal contract on the event store. Catch-up calls
// LoadFromVersion per aggregate; live tail uses Subscribe.
type EventSource[ID comparable] interface {
	// AllAggregateIDs returns the IDs to consider during catch-up. In
	// production this typically comes from a side index (the store does
	// not expose iteration). For tests / in-memory, see SimpleSource below.
	AllAggregateIDs(ctx context.Context) ([]ID, error)
	// LoadFromVersion returns envelopes with AggregateVersion > fromVersion.
	LoadFromVersion(ctx context.Context, aggregateID ID, fromVersion int) ([]ddd.EventEnvelope[ID], error)
	// Subscribe streams envelopes published after the call returns.
	Subscribe(ctx context.Context, handler func(context.Context, ddd.EventEnvelope[ID]) error) error
}

// ManagerConfig configures Manager.
type ManagerConfig struct {
	// CheckpointEvery saves the checkpoint after this many applied events.
	// Zero means checkpoint after every event (safest, slowest).
	CheckpointEvery int
	// OnError is invoked for every Apply error. If nil, the Manager stops
	// at the first error.
	OnError func(error)
}

// Manager drives a single Projector through catch-up then live phases.
type Manager[ID comparable] struct {
	src        EventSource[ID]
	cps        CheckpointStore[ID]
	projector  Projector[ID]
	cfg        ManagerConfig

	mu       sync.Mutex
	cp       Checkpoint[ID]
	sinceCkp int
}

// NewManager wires a Manager.
func NewManager[ID comparable](
	src EventSource[ID],
	cps CheckpointStore[ID],
	projector Projector[ID],
	cfg ManagerConfig,
) *Manager[ID] {
	return &Manager[ID]{
		src:       src,
		cps:       cps,
		projector: projector,
		cfg:       cfg,
		cp:        Checkpoint[ID]{PerAggregate: make(map[ID]int)},
	}
}

// CatchUp replays history for every known aggregate from its last
// checkpoint. Safe to call multiple times.
func (m *Manager[ID]) CatchUp(ctx context.Context) error {
	cp, err := m.cps.Load(ctx, m.projector.Name())
	if err != nil && !errors.Is(err, ErrCheckpointNotFound) {
		return fmt.Errorf("projection: load checkpoint: %w", err)
	}
	if cp.PerAggregate == nil {
		cp.PerAggregate = make(map[ID]int)
	}
	m.mu.Lock()
	m.cp = cp
	m.mu.Unlock()

	ids, err := m.src.AllAggregateIDs(ctx)
	if err != nil {
		return fmt.Errorf("projection: list aggregates: %w", err)
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		from := m.cp.PerAggregate[id]
		envs, err := m.src.LoadFromVersion(ctx, id, from)
		if err != nil {
			return fmt.Errorf("projection: load %v from v%d: %w", id, from, err)
		}
		for _, env := range envs {
			if err := m.applyOne(ctx, env); err != nil {
				return err
			}
		}
	}
	return m.saveCheckpoint(ctx)
}

// RunLive subscribes to new events and applies them. Returns when ctx is
// canceled. Call CatchUp before to seed state.
func (m *Manager[ID]) RunLive(ctx context.Context) error {
	return m.src.Subscribe(ctx, func(ctx context.Context, env ddd.EventEnvelope[ID]) error {
		return m.applyOne(ctx, env)
	})
}

func (m *Manager[ID]) applyOne(ctx context.Context, env ddd.EventEnvelope[ID]) error {
	m.mu.Lock()
	already := m.cp.PerAggregate[env.AggregateID]
	m.mu.Unlock()
	// Idempotent guard — never apply an envelope we already saw.
	if env.AggregateVersion <= already {
		return nil
	}

	if err := m.projector.Apply(ctx, env); err != nil {
		if m.cfg.OnError != nil {
			m.cfg.OnError(err)
			return nil
		}
		return err
	}

	m.mu.Lock()
	m.cp.PerAggregate[env.AggregateID] = env.AggregateVersion
	m.sinceCkp++
	due := m.cfg.CheckpointEvery == 0 || m.sinceCkp >= m.cfg.CheckpointEvery
	m.mu.Unlock()

	if due {
		return m.saveCheckpoint(ctx)
	}
	return nil
}

func (m *Manager[ID]) saveCheckpoint(ctx context.Context) error {
	m.mu.Lock()
	cp := Checkpoint[ID]{PerAggregate: make(map[ID]int, len(m.cp.PerAggregate))}
	for k, v := range m.cp.PerAggregate {
		cp.PerAggregate[k] = v
	}
	m.sinceCkp = 0
	m.mu.Unlock()
	return m.cps.Save(ctx, m.projector.Name(), cp)
}

// ----------------------------------------------------------------------------
// In-memory CheckpointStore
// ----------------------------------------------------------------------------

// ErrCheckpointNotFound is returned by CheckpointStore.Load when no
// checkpoint has been saved yet.
var ErrCheckpointNotFound = errors.New("projection: checkpoint not found")

// InMemoryCheckpointStore is a goroutine-safe in-memory store.
type InMemoryCheckpointStore[ID comparable] struct {
	mu   sync.RWMutex
	data map[string]Checkpoint[ID]
}

// NewInMemoryCheckpointStore returns an empty store.
func NewInMemoryCheckpointStore[ID comparable]() *InMemoryCheckpointStore[ID] {
	return &InMemoryCheckpointStore[ID]{data: make(map[string]Checkpoint[ID])}
}

func (s *InMemoryCheckpointStore[ID]) Load(_ context.Context, name string) (Checkpoint[ID], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.data[name]
	if !ok {
		return Checkpoint[ID]{}, ErrCheckpointNotFound
	}
	// Return a defensive copy.
	out := Checkpoint[ID]{PerAggregate: make(map[ID]int, len(cp.PerAggregate))}
	for k, v := range cp.PerAggregate {
		out.PerAggregate[k] = v
	}
	return out, nil
}

func (s *InMemoryCheckpointStore[ID]) Save(_ context.Context, name string, cp Checkpoint[ID]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := Checkpoint[ID]{PerAggregate: make(map[ID]int, len(cp.PerAggregate))}
	for k, v := range cp.PerAggregate {
		stored.PerAggregate[k] = v
	}
	s.data[name] = stored
	return nil
}
