package di

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ifaceLogger interface {
	Log(string)
}

type stubLogger struct {
	prefix  string
	started int32
	stopped int32
}

func (l *stubLogger) Log(string) {}

func (l *stubLogger) OnStart(_ context.Context) error {
	atomic.StoreInt32(&l.started, 1)
	return nil
}

func (l *stubLogger) OnStop(_ context.Context) error {
	atomic.StoreInt32(&l.stopped, 1)
	return nil
}

type userService struct {
	log ifaceLogger
}

func TestProvideAndResolve_Singleton(t *testing.T) {
	r := New()

	var calls int32
	Provide(r, func(_ *Resolver) (ifaceLogger, error) {
		atomic.AddInt32(&calls, 1)
		return &stubLogger{prefix: "X"}, nil
	})

	l1, err := Resolve[ifaceLogger](r)
	require.NoError(t, err)
	l2, err := Resolve[ifaceLogger](r)
	require.NoError(t, err)

	assert.Same(t, l1.(*stubLogger), l2.(*stubLogger), "singletons must return the same instance")
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "factory must run exactly once")
}

func TestProvideAndResolve_Transient(t *testing.T) {
	r := New()
	var calls int32
	ProvideTransient(r, func(_ *Resolver) (*stubLogger, error) {
		atomic.AddInt32(&calls, 1)
		return &stubLogger{}, nil
	})

	a, err := Resolve[*stubLogger](r)
	require.NoError(t, err)
	b, err := Resolve[*stubLogger](r)
	require.NoError(t, err)

	assert.NotSame(t, a, b, "transients must produce distinct instances")
	assert.Equal(t, int32(2), atomic.LoadInt32(&calls))
}

func TestNestedResolveAndDependencyOrder(t *testing.T) {
	r := New()
	Provide(r, func(_ *Resolver) (ifaceLogger, error) { return &stubLogger{prefix: "log"}, nil })
	Provide(r, func(rv *Resolver) (*userService, error) {
		log, err := From[ifaceLogger](rv)
		if err != nil {
			return nil, err
		}
		return &userService{log: log}, nil
	})

	svc, err := Resolve[*userService](r)
	require.NoError(t, err)
	assert.NotNil(t, svc.log)
}

func TestNotFound(t *testing.T) {
	r := New()
	_, err := Resolve[ifaceLogger](r)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestCycleDetection(t *testing.T) {
	type A struct{ _ any }
	type B struct{ _ any }
	r := New()

	Provide(r, func(rv *Resolver) (*A, error) {
		_, err := From[*B](rv)
		if err != nil {
			return nil, err
		}
		return &A{}, nil
	})
	Provide(r, func(rv *Resolver) (*B, error) {
		_, err := From[*A](rv)
		if err != nil {
			return nil, err
		}
		return &B{}, nil
	})

	_, err := Resolve[*A](r)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCycle)
}

func TestTagged(t *testing.T) {
	r := New()
	ProvideTagged(r, "primary", func(_ *Resolver) (ifaceLogger, error) {
		return &stubLogger{prefix: "p"}, nil
	})
	ProvideTagged(r, "audit", func(_ *Resolver) (ifaceLogger, error) {
		return &stubLogger{prefix: "a"}, nil
	})

	p, err := ResolveTagged[ifaceLogger](r, "primary")
	require.NoError(t, err)
	a, err := ResolveTagged[ifaceLogger](r, "audit")
	require.NoError(t, err)

	assert.Equal(t, "p", p.(*stubLogger).prefix)
	assert.Equal(t, "a", a.(*stubLogger).prefix)
}

func TestScoped(t *testing.T) {
	r := New()
	ProvideScoped(r, func(_ *Resolver) (*stubLogger, error) {
		return &stubLogger{prefix: "scoped"}, nil
	})

	// Outside any scope -> error.
	_, err := Resolve[*stubLogger](r)
	assert.ErrorIs(t, err, ErrScopedOutside)

	s1 := r.NewScope()
	a1, err := ResolveInScope[*stubLogger](s1)
	require.NoError(t, err)
	a2, err := ResolveInScope[*stubLogger](s1)
	require.NoError(t, err)
	assert.Same(t, a1, a2, "same scope must reuse instance")

	s2 := r.NewScope()
	b, err := ResolveInScope[*stubLogger](s2)
	require.NoError(t, err)
	assert.NotSame(t, a1, b, "different scopes must produce different instances")
}

func TestLifecycleStartStop(t *testing.T) {
	r := New()
	Provide(r, func(_ *Resolver) (*stubLogger, error) { return &stubLogger{prefix: "L"}, nil })

	ctx := context.Background()
	require.NoError(t, r.Start(ctx))

	l := MustResolve[*stubLogger](r)
	assert.Equal(t, int32(1), atomic.LoadInt32(&l.started))

	require.NoError(t, r.Stop(ctx))
	assert.Equal(t, int32(1), atomic.LoadInt32(&l.stopped))
}

func TestStartReturnsFactoryError(t *testing.T) {
	r := New()
	boom := errors.New("boom")
	Provide(r, func(_ *Resolver) (*stubLogger, error) { return nil, boom })

	err := r.Start(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestProvideValue(t *testing.T) {
	r := New()
	want := &stubLogger{prefix: "v"}
	ProvideValue[*stubLogger](r, want)

	got, err := Resolve[*stubLogger](r)
	require.NoError(t, err)
	assert.Same(t, want, got)
}
