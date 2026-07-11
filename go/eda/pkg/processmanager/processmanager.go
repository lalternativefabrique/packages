// Package processmanager implements a generic, event-driven process
// manager (a.k.a. orchestration saga) — a state machine that reacts to
// inbound events by mutating its persisted state and emitting outbound
// commands or events.
//
// Design:
//
//   - State[S] is the user's flat, JSON-serializable struct.
//   - Definition[ID,S] describes how to derive a process instance ID
//     from an inbound envelope, the initial state for a new instance,
//     and the transitions: which event kinds trigger which Handler.
//   - StateStore[ID,S] persists state with optimistic concurrency.
//   - Dispatcher feeds inbound events to the engine; each handler can
//     emit commands/events through Effect functions.
//
// Inspiration: synthiz/apps/core/integration-events.
package processmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Effect is something a handler asks the engine to do after a successful
// state transition. The engine executes effects in order, after the new
// state is persisted, so partial failures retry safely.
type Effect interface {
	Run(ctx context.Context) error
}

// EffectFunc adapts a function.
type EffectFunc func(ctx context.Context) error

func (f EffectFunc) Run(ctx context.Context) error { return f(ctx) }

// HandlerInput is what handlers receive: current state plus the inbound
// envelope. Handlers return the new state and a list of effects.
type HandlerInput[ID comparable, S any] struct {
	State    S
	Envelope ddd.EventEnvelope[ID]
}

// HandlerOutput is what handlers return: the next state, effects to run,
// and a Done flag that signals the process instance is finished (the
// engine then garbage-collects it via Terminate, if implemented).
type HandlerOutput[S any] struct {
	State   S
	Effects []Effect
	Done    bool
}

// Handler reacts to a single inbound event kind.
type Handler[ID comparable, S any] func(ctx context.Context, in HandlerInput[ID, S]) (HandlerOutput[S], error)

// Definition wires the process manager.
type Definition[ID comparable, S any] struct {
	// Name is the unique identifier of this process manager — used as the
	// state store partition.
	Name string
	// InstanceID derives the process instance ID from an inbound envelope.
	// Typically returns env.CorrelationID or a domain-specific key.
	InstanceID func(env ddd.EventEnvelope[ID]) (string, error)
	// Initial returns the zero state for a fresh process instance.
	Initial func() S
	// Handlers is the routing table: inbound EventKind -> handler.
	Handlers map[string]Handler[ID, S]
}

// Stored is what the StateStore persists for one process instance.
type Stored[S any] struct {
	// Version is the optimistic concurrency token. Starts at 0 for a new
	// instance and increments on every successful save.
	Version int
	// State is the user state.
	State S
	// LastEventID is the EventID of the last applied inbound envelope —
	// used for idempotency.
	LastEventID string
	// Done indicates a terminated instance. The engine will refuse further
	// transitions on Done=true.
	Done bool
}

// StateStore persists Stored[S] per (definitionName, instanceID).
type StateStore[S any] interface {
	// Load returns the stored state. Returns ErrInstanceNotFound on miss.
	Load(ctx context.Context, defName, instanceID string) (Stored[S], error)
	// Save persists the new state. Implementations must enforce that the
	// stored Version equals expectedVersion; otherwise return
	// ErrConcurrencyConflict.
	Save(ctx context.Context, defName, instanceID string, expectedVersion int, next Stored[S]) error
}

// Errors.
var (
	ErrInstanceNotFound    = errors.New("processmanager: instance not found")
	ErrConcurrencyConflict = errors.New("processmanager: concurrency conflict")
	ErrAlreadyDone         = errors.New("processmanager: instance already terminated")
	ErrNoHandler           = errors.New("processmanager: no handler for event kind")
)

// Engine drives the definition.
type Engine[ID comparable, S any] struct {
	def   Definition[ID, S]
	store StateStore[S]
}

// New returns an Engine.
func New[ID comparable, S any](def Definition[ID, S], store StateStore[S]) (*Engine[ID, S], error) {
	if def.Name == "" {
		return nil, errors.New("processmanager: definition Name required")
	}
	if def.InstanceID == nil {
		return nil, errors.New("processmanager: InstanceID required")
	}
	if def.Initial == nil {
		return nil, errors.New("processmanager: Initial required")
	}
	if len(def.Handlers) == 0 {
		return nil, errors.New("processmanager: at least one Handler required")
	}
	return &Engine[ID, S]{def: def, store: store}, nil
}

// Handle drives one envelope through the engine. It loads (or initializes)
// the process instance, applies the handler, persists with OCC and runs
// the returned effects.
//
// Idempotency: if env.EventID equals the stored LastEventID, the call is
// a no-op (the handler already saw this event).
func (e *Engine[ID, S]) Handle(ctx context.Context, env ddd.EventEnvelope[ID]) error {
	handler, ok := e.def.Handlers[env.EventType]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoHandler, env.EventType)
	}
	instanceID, err := e.def.InstanceID(env)
	if err != nil {
		return fmt.Errorf("processmanager: instance id: %w", err)
	}

	stored, err := e.store.Load(ctx, e.def.Name, instanceID)
	expectedVersion := 0
	if err != nil {
		if !errors.Is(err, ErrInstanceNotFound) {
			return fmt.Errorf("processmanager: load: %w", err)
		}
		stored = Stored[S]{State: e.def.Initial()}
	} else {
		expectedVersion = stored.Version
		if stored.Done {
			return ErrAlreadyDone
		}
		if env.EventID != "" && stored.LastEventID == env.EventID {
			return nil // idempotent replay
		}
	}

	out, err := handler(ctx, HandlerInput[ID, S]{State: stored.State, Envelope: env})
	if err != nil {
		return fmt.Errorf("processmanager: handler %s: %w", env.EventType, err)
	}

	next := Stored[S]{
		Version:     stored.Version + 1,
		State:       out.State,
		LastEventID: env.EventID,
		Done:        out.Done,
	}
	if err := e.store.Save(ctx, e.def.Name, instanceID, expectedVersion, next); err != nil {
		return err
	}

	for _, ef := range out.Effects {
		if err := ef.Run(ctx); err != nil {
			return fmt.Errorf("processmanager: effect: %w", err)
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// In-memory StateStore
// ----------------------------------------------------------------------------

// InMemoryStateStore is a goroutine-safe in-memory StateStore.
type InMemoryStateStore[S any] struct {
	mu   sync.Mutex
	data map[string]Stored[S] // key: defName + "/" + instanceID
}

// NewInMemoryStateStore returns an empty store.
func NewInMemoryStateStore[S any]() *InMemoryStateStore[S] {
	return &InMemoryStateStore[S]{data: make(map[string]Stored[S])}
}

func (s *InMemoryStateStore[S]) key(defName, instanceID string) string {
	return defName + "/" + instanceID
}

func (s *InMemoryStateStore[S]) Load(_ context.Context, defName, instanceID string) (Stored[S], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[s.key(defName, instanceID)]
	if !ok {
		return Stored[S]{}, ErrInstanceNotFound
	}
	return v, nil
}

func (s *InMemoryStateStore[S]) Save(_ context.Context, defName, instanceID string, expectedVersion int, next Stored[S]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := s.key(defName, instanceID)
	cur, exists := s.data[key]
	curVersion := 0
	if exists {
		curVersion = cur.Version
	}
	if expectedVersion != curVersion {
		return fmt.Errorf("%w: expected v%d, got v%d", ErrConcurrencyConflict, expectedVersion, curVersion)
	}
	s.data[key] = next
	return nil
}
