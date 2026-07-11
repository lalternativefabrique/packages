package db

import (
	"context"
	"fmt"
	"sync"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// InMemoryStore is a goroutine-safe, typed event store useful for tests
// and local development. It mirrors the semantics of JetStreamStore for
// Save/Load (OCC, append-only) but lives entirely in memory.
type InMemoryStore[ID comparable] struct {
	mu     sync.Mutex
	events map[ID][]ddd.EventEnvelope[ID]
	subs   []func(context.Context, ddd.EventEnvelope[ID]) error
}

// NewInMemoryStore returns an empty store.
func NewInMemoryStore[ID comparable]() *InMemoryStore[ID] {
	return &InMemoryStore[ID]{events: make(map[ID][]ddd.EventEnvelope[ID])}
}

// Save appends envelopes for an aggregate. expectedVersion must match the
// current stored version (0 for a fresh aggregate). Envelope versions are
// expected contiguous starting at expectedVersion+1.
func (s *InMemoryStore[ID]) Save(_ context.Context, aggregateID ID, expectedVersion int, envs []ddd.EventEnvelope[ID]) error {
	if len(envs) == 0 {
		return nil
	}
	s.mu.Lock()
	history := s.events[aggregateID]
	current := 0
	if n := len(history); n > 0 {
		current = history[n-1].AggregateVersion
	}
	if expectedVersion != current {
		s.mu.Unlock()
		return fmt.Errorf("%w: aggregate %v expected v%d, got v%d",
			ErrConcurrencyConflict, aggregateID, expectedVersion, current)
	}
	for i, env := range envs {
		want := current + i + 1
		if env.AggregateVersion != want {
			s.mu.Unlock()
			return fmt.Errorf("eventstore: non-contiguous version: expected %d got %d", want, env.AggregateVersion)
		}
	}
	s.events[aggregateID] = append(history, envs...)
	subs := append([]func(context.Context, ddd.EventEnvelope[ID]) error(nil), s.subs...)
	s.mu.Unlock()

	for _, env := range envs {
		for _, h := range subs {
			if err := h(context.Background(), env); err != nil {
				return err
			}
		}
	}
	return nil
}

// Load returns the full history of an aggregate.
func (s *InMemoryStore[ID]) Load(_ context.Context, aggregateID ID) ([]ddd.EventEnvelope[ID], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.events[aggregateID]
	if len(history) == 0 {
		return nil, fmt.Errorf("%w: %v", ErrAggregateNotFound, aggregateID)
	}
	out := make([]ddd.EventEnvelope[ID], len(history))
	copy(out, history)
	return out, nil
}

// LoadFromVersion returns envelopes with AggregateVersion > fromVersion.
func (s *InMemoryStore[ID]) LoadFromVersion(_ context.Context, aggregateID ID, fromVersion int) ([]ddd.EventEnvelope[ID], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	history := s.events[aggregateID]
	if len(history) == 0 {
		return nil, fmt.Errorf("%w: %v", ErrAggregateNotFound, aggregateID)
	}
	out := make([]ddd.EventEnvelope[ID], 0, len(history))
	for _, env := range history {
		if env.AggregateVersion > fromVersion {
			out = append(out, env)
		}
	}
	return out, nil
}

// Subscribe registers a handler invoked synchronously for every envelope
// saved after this call. There is no replay of past events.
func (s *InMemoryStore[ID]) Subscribe(_ context.Context, handler func(context.Context, ddd.EventEnvelope[ID]) error) error {
	s.mu.Lock()
	s.subs = append(s.subs, handler)
	s.mu.Unlock()
	return nil
}
