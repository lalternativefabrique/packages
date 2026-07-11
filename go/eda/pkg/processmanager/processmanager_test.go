package processmanager

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

// --- a tiny "order shipping" process manager ----------------------------

type orderPlaced struct {
	OrderID string
}

func (orderPlaced) EventKind() string { return "order.placed" }

type paymentCaptured struct {
	OrderID string
}

func (paymentCaptured) EventKind() string { return "payment.captured" }

// state of the shipping process per order.
type shipState struct {
	OrderID string
	Placed  bool
	Paid    bool
}

func newDef(emittedCount *int32) Definition[string, shipState] {
	return Definition[string, shipState]{
		Name: "ship",
		InstanceID: func(env ddd.EventEnvelope[string]) (string, error) {
			// correlate by aggregate id, which is the order id here.
			return env.AggregateID, nil
		},
		Initial: func() shipState { return shipState{} },
		Handlers: map[string]Handler[string, shipState]{
			"order.placed": func(_ context.Context, in HandlerInput[string, shipState]) (HandlerOutput[shipState], error) {
				st := in.State
				st.OrderID = in.Envelope.AggregateID
				st.Placed = true
				return HandlerOutput[shipState]{State: st}, nil
			},
			"payment.captured": func(_ context.Context, in HandlerInput[string, shipState]) (HandlerOutput[shipState], error) {
				st := in.State
				st.Paid = true
				effects := []Effect{
					EffectFunc(func(_ context.Context) error {
						atomic.AddInt32(emittedCount, 1)
						return nil
					}),
				}
				return HandlerOutput[shipState]{State: st, Effects: effects, Done: true}, nil
			},
		},
	}
}

func mkEnv(t *testing.T, agg, kind string, version int, payload ddd.EventPayload) ddd.EventEnvelope[string] {
	t.Helper()
	return ddd.NewEnvelope[string](
		ddd.FixedClock{T: time.Unix(1700000000, 0).UTC()},
		"Order", agg, version, payload,
	)
}

func TestEngine_TransitionsAndEffect(t *testing.T) {
	var emitted int32
	def := newDef(&emitted)
	store := NewInMemoryStateStore[shipState]()
	eng, err := New[string, shipState](def, store)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, eng.Handle(ctx, mkEnv(t, "ord-1", "order.placed", 1, orderPlaced{OrderID: "ord-1"})))
	require.NoError(t, eng.Handle(ctx, mkEnv(t, "ord-1", "payment.captured", 1, paymentCaptured{OrderID: "ord-1"})))

	stored, err := store.Load(ctx, "ship", "ord-1")
	require.NoError(t, err)
	assert.True(t, stored.State.Placed)
	assert.True(t, stored.State.Paid)
	assert.True(t, stored.Done)
	assert.Equal(t, 2, stored.Version)
	assert.Equal(t, int32(1), atomic.LoadInt32(&emitted))
}

func TestEngine_Idempotency(t *testing.T) {
	var emitted int32
	def := newDef(&emitted)
	store := NewInMemoryStateStore[shipState]()
	eng, err := New[string, shipState](def, store)
	require.NoError(t, err)

	ctx := context.Background()
	env := mkEnv(t, "ord-2", "order.placed", 1, orderPlaced{OrderID: "ord-2"})
	require.NoError(t, eng.Handle(ctx, env))
	require.NoError(t, eng.Handle(ctx, env)) // replay

	stored, err := store.Load(ctx, "ship", "ord-2")
	require.NoError(t, err)
	assert.Equal(t, 1, stored.Version, "replay must not bump version")
}

func TestEngine_NoHandler(t *testing.T) {
	def := newDef(new(int32))
	store := NewInMemoryStateStore[shipState]()
	eng, _ := New[string, shipState](def, store)

	type unknown struct{}
	type unknownPayload struct{}
	// dummy payload with an unmatched kind
	env := ddd.NewEnvelope[string](nil, "Order", "ord-3", 1, weirdPayload{})
	err := eng.Handle(context.Background(), env)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoHandler)
}

type weirdPayload struct{}

func (weirdPayload) EventKind() string { return "weird.kind" }

func TestEngine_RejectsAfterDone(t *testing.T) {
	def := newDef(new(int32))
	store := NewInMemoryStateStore[shipState]()
	eng, _ := New[string, shipState](def, store)
	ctx := context.Background()

	require.NoError(t, eng.Handle(ctx, mkEnv(t, "ord-4", "order.placed", 1, orderPlaced{})))
	require.NoError(t, eng.Handle(ctx, mkEnv(t, "ord-4", "payment.captured", 1, paymentCaptured{})))
	// Done — any further event should be rejected.
	err := eng.Handle(ctx, mkEnv(t, "ord-4", "order.placed", 2, orderPlaced{}))
	assert.ErrorIs(t, err, ErrAlreadyDone)
}

func TestEngine_HandlerErrorPropagates(t *testing.T) {
	store := NewInMemoryStateStore[shipState]()
	def := Definition[string, shipState]{
		Name: "ship",
		InstanceID: func(env ddd.EventEnvelope[string]) (string, error) { return env.AggregateID, nil },
		Initial:    func() shipState { return shipState{} },
		Handlers: map[string]Handler[string, shipState]{
			"order.placed": func(_ context.Context, _ HandlerInput[string, shipState]) (HandlerOutput[shipState], error) {
				return HandlerOutput[shipState]{}, errors.New("boom")
			},
		},
	}
	eng, _ := New[string, shipState](def, store)
	err := eng.Handle(context.Background(), mkEnv(t, "ord-5", "order.placed", 1, orderPlaced{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
