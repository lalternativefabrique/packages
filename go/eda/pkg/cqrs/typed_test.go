package cqrs

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

type createUserCmd struct {
	Name string
}
type createUserRes struct {
	ID string
}

type getUserQry struct{ ID string }
type getUserRes struct{ Name string }

func TestExecute_DispatchesByType(t *testing.T) {
	r := NewCommandRouter()
	RegisterCommandHandler[createUserCmd, createUserRes](r,
		TypedCommandHandlerFunc[createUserCmd, createUserRes](
			func(_ context.Context, c createUserCmd) (createUserRes, error) {
				return createUserRes{ID: "u-" + c.Name}, nil
			},
		),
	)

	got, err := Execute[createUserCmd, createUserRes](context.Background(), r, createUserCmd{Name: "alice"})
	require.NoError(t, err)
	assert.Equal(t, "u-alice", got.ID)
}

func TestExecute_NoHandler(t *testing.T) {
	r := NewCommandRouter()
	_, err := Execute[createUserCmd, createUserRes](context.Background(), r, createUserCmd{})
	assert.ErrorIs(t, err, ErrNoHandler)
}

func TestAsk_QueryDispatch(t *testing.T) {
	r := NewQueryRouter()
	RegisterQueryHandler[getUserQry, getUserRes](r,
		TypedQueryHandlerFunc[getUserQry, getUserRes](
			func(_ context.Context, q getUserQry) (getUserRes, error) {
				return getUserRes{Name: q.ID + "-name"}, nil
			},
		),
	)

	got, err := Ask[getUserQry, getUserRes](context.Background(), r, getUserQry{ID: "42"})
	require.NoError(t, err)
	assert.Equal(t, "42-name", got.Name)
}

func TestRecoveryMiddleware_TurnsPanicsIntoErrors(t *testing.T) {
	mw := RecoveryMiddleware[createUserCmd, createUserRes]()
	wrapped := mw(func(_ context.Context, _ createUserCmd) (createUserRes, error) {
		panic("kaboom")
	})

	_, err := wrapped(context.Background(), createUserCmd{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrHandlerPanic)
}

func TestTimeoutMiddleware_FiresOnSlowHandler(t *testing.T) {
	mw := TimeoutMiddleware[createUserCmd, createUserRes](20 * time.Millisecond)
	wrapped := mw(func(ctx context.Context, _ createUserCmd) (createUserRes, error) {
		select {
		case <-ctx.Done():
			return createUserRes{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return createUserRes{ID: "late"}, nil
		}
	})

	_, err := wrapped(context.Background(), createUserCmd{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTimeout)
}

func TestRetryMiddleware_RetriesUntilSuccess(t *testing.T) {
	var calls int32
	mw := RetryMiddleware[createUserCmd, createUserRes](3, time.Millisecond, func(_ error) bool { return true })
	wrapped := mw(func(_ context.Context, _ createUserCmd) (createUserRes, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return createUserRes{}, errors.New("transient")
		}
		return createUserRes{ID: "ok"}, nil
	})

	got, err := wrapped(context.Background(), createUserCmd{})
	require.NoError(t, err)
	assert.Equal(t, "ok", got.ID)
	assert.Equal(t, int32(3), atomic.LoadInt32(&calls))
}

func TestChain_Order(t *testing.T) {
	var order []string
	outer := func(name string) Middleware[createUserCmd, createUserRes] {
		return func(next func(context.Context, createUserCmd) (createUserRes, error)) func(context.Context, createUserCmd) (createUserRes, error) {
			return func(ctx context.Context, in createUserCmd) (createUserRes, error) {
				order = append(order, name+":before")
				res, err := next(ctx, in)
				order = append(order, name+":after")
				return res, err
			}
		}
	}

	build := Chain(outer("A"), outer("B"))
	h := build(func(_ context.Context, _ createUserCmd) (createUserRes, error) {
		order = append(order, "handler")
		return createUserRes{}, nil
	})
	_, err := h(context.Background(), createUserCmd{})
	require.NoError(t, err)
	assert.Equal(t, []string{"A:before", "B:before", "handler", "B:after", "A:after"}, order)
}

type pinged struct{ Count int }

func (pinged) EventKind() string { return "pinged" }

func TestTypedEventBus_PublishDelivers(t *testing.T) {
	bus := NewTypedEventBus[string](TypedEventBusConfig{Workers: 2, QueueSize: 8})
	defer bus.Close()

	var received int32
	bus.Subscribe("pinged", TypedEventHandlerFunc[string](func(_ context.Context, env ddd.EventEnvelope[string]) error {
		atomic.AddInt32(&received, int32(env.Payload.(pinged).Count))
		return nil
	}))

	env := ddd.NewEnvelope[string](nil, "Ping", "p-1", 1, pinged{Count: 3})
	require.NoError(t, bus.Publish(context.Background(), env))
	require.NoError(t, bus.Publish(context.Background(), env))

	// Allow workers to drain.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&received) == 6 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected 6, got %d", atomic.LoadInt32(&received))
}
