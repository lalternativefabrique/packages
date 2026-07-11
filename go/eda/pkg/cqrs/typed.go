// Package cqrs: modern, typed CQRS primitives.
//
// This file complements the legacy buses in cqrs.go (string-keyed,
// interface{}-typed) with a generics-based API that adds:
//
//   - typed CommandHandler[C, R] and QueryHandler[Q, R]
//   - middleware chains (logging, tracing, recovery, retry, timeout, ...)
//   - typed domain errors (ErrConcurrencyConflict, ErrNotFound, ...)
//   - async event bus with bounded worker pool and back-pressure
//
// Handlers are dispatched by the static Go type of the command/query, so
// there is no string lookup at call sites in user code.
package cqrs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Typed domain errors. Wrap them with fmt.Errorf("%w: ...") to add context.
var (
	ErrNotFound            = errors.New("cqrs: not found")
	ErrConcurrencyConflict = errors.New("cqrs: concurrency conflict")
	ErrUnauthorized        = errors.New("cqrs: unauthorized")
	ErrValidation          = errors.New("cqrs: validation failed")
	ErrHandlerPanic        = errors.New("cqrs: handler panicked")
	ErrNoHandler           = errors.New("cqrs: no handler registered")
	ErrTimeout             = errors.New("cqrs: handler timeout")
)

// TypedCommandHandler handles a typed command and returns a typed result.
// It is the generics-based replacement for the legacy CommandHandler
// declared in cqrs.go (kept for backwards compatibility).
type TypedCommandHandler[C any, R any] interface {
	Handle(ctx context.Context, cmd C) (R, error)
}

// TypedCommandHandlerFunc adapts a plain function to TypedCommandHandler.
type TypedCommandHandlerFunc[C any, R any] func(context.Context, C) (R, error)

// Handle implements TypedCommandHandler.
func (f TypedCommandHandlerFunc[C, R]) Handle(ctx context.Context, cmd C) (R, error) {
	return f(ctx, cmd)
}

// TypedQueryHandler handles a typed query and returns a typed result.
type TypedQueryHandler[Q any, R any] interface {
	Handle(ctx context.Context, q Q) (R, error)
}

// TypedQueryHandlerFunc adapts a plain function to TypedQueryHandler.
type TypedQueryHandlerFunc[Q any, R any] func(context.Context, Q) (R, error)

// Handle implements TypedQueryHandler.
func (f TypedQueryHandlerFunc[Q, R]) Handle(ctx context.Context, q Q) (R, error) {
	return f(ctx, q)
}

// ----------------------------------------------------------------------------
// Middleware
// ----------------------------------------------------------------------------

// Middleware wraps a typed handler with cross-cutting behavior. It is the
// same signature for commands and queries thanks to generics.
type Middleware[In, Out any] func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error)

// Chain composes middlewares so the first one is the outermost wrapper.
//
//	final := Chain(LoggingMW, TracingMW, RecoveryMW)(handler)
func Chain[In, Out any](mws ...Middleware[In, Out]) func(func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
	return func(handler func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		for i := len(mws) - 1; i >= 0; i-- {
			handler = mws[i](handler)
		}
		return handler
	}
}

// RecoveryMiddleware turns panics into ErrHandlerPanic errors, capturing
// the stack trace as part of the wrapped error.
func RecoveryMiddleware[In, Out any]() Middleware[In, Out] {
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (out Out, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("%w: %v\n%s", ErrHandlerPanic, r, debug.Stack())
				}
			}()
			return next(ctx, in)
		}
	}
}

// TimeoutMiddleware enforces a deadline on the inner handler. If the
// deadline is hit, ErrTimeout is returned.
func TimeoutMiddleware[In, Out any](d time.Duration) Middleware[In, Out] {
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (Out, error) {
			cctx, cancel := context.WithTimeout(ctx, d)
			defer cancel()
			out, err := next(cctx, in)
			if cctx.Err() == context.DeadlineExceeded {
				var zero Out
				return zero, fmt.Errorf("%w: after %s", ErrTimeout, d)
			}
			return out, err
		}
	}
}

// RetryMiddleware retries the inner handler up to attempts-1 extra times
// when the returned error matches shouldRetry. Backoff is linear: base * i.
func RetryMiddleware[In, Out any](attempts int, base time.Duration, shouldRetry func(error) bool) Middleware[In, Out] {
	if attempts < 1 {
		attempts = 1
	}
	if shouldRetry == nil {
		shouldRetry = func(error) bool { return false }
	}
	return func(next func(context.Context, In) (Out, error)) func(context.Context, In) (Out, error) {
		return func(ctx context.Context, in In) (Out, error) {
			var last error
			for i := 1; i <= attempts; i++ {
				out, err := next(ctx, in)
				if err == nil {
					return out, nil
				}
				last = err
				if !shouldRetry(err) || i == attempts {
					return out, err
				}
				select {
				case <-ctx.Done():
					return out, ctx.Err()
				case <-time.After(base * time.Duration(i)):
				}
			}
			var zero Out
			return zero, last
		}
	}
}

// ----------------------------------------------------------------------------
// CommandRouter / QueryRouter
// ----------------------------------------------------------------------------

// CommandRouter dispatches a command to the handler bound to its concrete
// Go type. Register with RegisterCommandHandler.
type CommandRouter struct {
	mu       sync.RWMutex
	handlers map[reflect.Type]dispatchFunc
}

// QueryRouter is the query-side counterpart to CommandRouter.
type QueryRouter struct {
	mu       sync.RWMutex
	handlers map[reflect.Type]dispatchFunc
}

type dispatchFunc = func(context.Context, any) (any, error)

// NewCommandRouter returns an empty router.
func NewCommandRouter() *CommandRouter {
	return &CommandRouter{handlers: make(map[reflect.Type]dispatchFunc)}
}

// NewQueryRouter returns an empty router.
func NewQueryRouter() *QueryRouter {
	return &QueryRouter{handlers: make(map[reflect.Type]dispatchFunc)}
}

// RegisterCommandHandler binds a typed handler. Multiple registrations for
// the same C type panic to surface misconfiguration at boot.
func RegisterCommandHandler[C any, R any](r *CommandRouter, h TypedCommandHandler[C, R]) {
	var zero C
	typ := reflect.TypeOf(&zero).Elem()
	r.mu.Lock()
	if _, dup := r.handlers[typ]; dup {
		r.mu.Unlock()
		panic(fmt.Sprintf("cqrs: duplicate command handler for %s", typ))
	}
	r.handlers[typ] = func(ctx context.Context, in any) (any, error) {
		cmd, ok := in.(C)
		if !ok {
			return nil, fmt.Errorf("cqrs: command type mismatch: have %T want %s", in, typ)
		}
		return h.Handle(ctx, cmd)
	}
	r.mu.Unlock()
}

// RegisterQueryHandler binds a typed query handler.
func RegisterQueryHandler[Q any, R any](r *QueryRouter, h TypedQueryHandler[Q, R]) {
	var zero Q
	typ := reflect.TypeOf(&zero).Elem()
	r.mu.Lock()
	if _, dup := r.handlers[typ]; dup {
		r.mu.Unlock()
		panic(fmt.Sprintf("cqrs: duplicate query handler for %s", typ))
	}
	r.handlers[typ] = func(ctx context.Context, in any) (any, error) {
		q, ok := in.(Q)
		if !ok {
			return nil, fmt.Errorf("cqrs: query type mismatch: have %T want %s", in, typ)
		}
		return h.Handle(ctx, q)
	}
	r.mu.Unlock()
}

// Execute dispatches a command by its concrete Go type. R is the expected
// result type and is checked at runtime.
func Execute[C any, R any](ctx context.Context, r *CommandRouter, cmd C) (R, error) {
	var zeroR R
	typ := reflect.TypeOf(&cmd).Elem()
	r.mu.RLock()
	h, ok := r.handlers[typ]
	r.mu.RUnlock()
	if !ok {
		return zeroR, fmt.Errorf("%w: %s", ErrNoHandler, typ)
	}
	out, err := h(ctx, cmd)
	if err != nil {
		return zeroR, err
	}
	res, ok := out.(R)
	if !ok {
		return zeroR, fmt.Errorf("cqrs: result type mismatch: have %T want %T", out, zeroR)
	}
	return res, nil
}

// Ask dispatches a query and returns its typed result.
func Ask[Q any, R any](ctx context.Context, r *QueryRouter, q Q) (R, error) {
	var zeroR R
	typ := reflect.TypeOf(&q).Elem()
	r.mu.RLock()
	h, ok := r.handlers[typ]
	r.mu.RUnlock()
	if !ok {
		return zeroR, fmt.Errorf("%w: %s", ErrNoHandler, typ)
	}
	out, err := h(ctx, q)
	if err != nil {
		return zeroR, err
	}
	res, ok := out.(R)
	if !ok {
		return zeroR, fmt.Errorf("cqrs: result type mismatch: have %T want %T", out, zeroR)
	}
	return res, nil
}

// ----------------------------------------------------------------------------
// Typed event bus
// ----------------------------------------------------------------------------

// TypedEventHandler consumes a typed envelope payload.
type TypedEventHandler[ID comparable] interface {
	HandleEvent(ctx context.Context, env ddd.EventEnvelope[ID]) error
}

// TypedEventHandlerFunc adapts a function.
type TypedEventHandlerFunc[ID comparable] func(context.Context, ddd.EventEnvelope[ID]) error

func (f TypedEventHandlerFunc[ID]) HandleEvent(ctx context.Context, env ddd.EventEnvelope[ID]) error {
	return f(ctx, env)
}

// TypedEventBus dispatches envelopes to handlers subscribed by EventKind.
// Dispatch is asynchronous through a bounded worker pool, providing
// back-pressure via channel capacity.
type TypedEventBus[ID comparable] struct {
	mu       sync.RWMutex
	handlers map[string][]TypedEventHandler[ID]
	queue    chan dispatchItem[ID]
	stop     chan struct{}
	workers  int
	wg       sync.WaitGroup
	onError  func(error)
}

type dispatchItem[ID comparable] struct {
	ctx context.Context
	env ddd.EventEnvelope[ID]
	h   TypedEventHandler[ID]
}

// TypedEventBusConfig configures a TypedEventBus.
type TypedEventBusConfig struct {
	// Workers is the number of goroutines draining the queue.
	Workers int
	// QueueSize is the channel capacity (back-pressure when full).
	QueueSize int
	// OnError is invoked when a handler returns an error. If nil, errors
	// are dropped.
	OnError func(error)
}

// NewTypedEventBus starts the worker pool and returns the bus.
func NewTypedEventBus[ID comparable](cfg TypedEventBusConfig) *TypedEventBus[ID] {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 256
	}
	b := &TypedEventBus[ID]{
		handlers: make(map[string][]TypedEventHandler[ID]),
		queue:    make(chan dispatchItem[ID], cfg.QueueSize),
		stop:     make(chan struct{}),
		workers:  cfg.Workers,
		onError:  cfg.OnError,
	}
	for i := 0; i < cfg.Workers; i++ {
		b.wg.Add(1)
		go b.worker()
	}
	return b
}

func (b *TypedEventBus[ID]) worker() {
	defer b.wg.Done()
	for {
		select {
		case <-b.stop:
			return
		case it := <-b.queue:
			if err := it.h.HandleEvent(it.ctx, it.env); err != nil && b.onError != nil {
				b.onError(err)
			}
		}
	}
}

// Subscribe binds a handler to a kind. Kind must match Payload.EventKind().
func (b *TypedEventBus[ID]) Subscribe(kind string, h TypedEventHandler[ID]) {
	b.mu.Lock()
	b.handlers[kind] = append(b.handlers[kind], h)
	b.mu.Unlock()
}

// Publish enqueues the envelope for asynchronous delivery. Returns when
// either all handlers are enqueued or ctx is done.
func (b *TypedEventBus[ID]) Publish(ctx context.Context, env ddd.EventEnvelope[ID]) error {
	b.mu.RLock()
	hs := append([]TypedEventHandler[ID](nil), b.handlers[env.EventType]...)
	b.mu.RUnlock()

	for _, h := range hs {
		item := dispatchItem[ID]{ctx: ctx, env: env, h: h}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case b.queue <- item:
		}
	}
	return nil
}

// Close stops the workers after draining in-flight items.
func (b *TypedEventBus[ID]) Close() {
	close(b.stop)
	b.wg.Wait()
}
