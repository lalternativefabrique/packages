// Package di provides a type-safe dependency injection container with
// support for lifetimes (Singleton / Transient / Scoped), tagged bindings,
// dependency cycles detection and lifecycle hooks (OnStart / OnStop).
//
// Two APIs coexist in this package:
//
//   - The legacy string-keyed Container (container.go) — kept for
//     backwards compatibility, slated for removal in a future PR.
//   - The typed Registry below, which is the preferred API.
//
// Quick usage:
//
//	r := di.New()
//	di.Provide(r, func(_ *di.Resolver) (Logger, error) { return newLogger(), nil })
//	di.Provide(r, func(rv *di.Resolver) (*UserService, error) {
//	    log, err := di.From[Logger](rv)
//	    if err != nil { return nil, err }
//	    return &UserService{log: log}, nil
//	})
//
//	svc := di.MustResolve[*UserService](r)
package di

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

// Lifetime controls how often a factory is invoked.
type Lifetime int

const (
	// Singleton: factory invoked at most once per registry; cached.
	Singleton Lifetime = iota
	// Transient: factory invoked on every Resolve.
	Transient
	// Scoped: factory invoked at most once per Scope; cached on the scope.
	Scoped
)

// Lifecycle is implemented by services that need start/stop hooks.
// Start runs from leaves to roots (build order). Stop runs in reverse.
type Lifecycle interface {
	OnStart(ctx context.Context) error
	OnStop(ctx context.Context) error
}

var (
	ErrNotFound       = errors.New("di: service not found")
	ErrCycle          = errors.New("di: dependency cycle detected")
	ErrTypeMismatch   = errors.New("di: type mismatch")
	ErrAlreadyStarted = errors.New("di: registry already started")
	ErrNotStarted     = errors.New("di: registry not started")
	ErrScopedOutside  = errors.New("di: scoped service resolved outside any Scope")
)

// providerKey identifies a binding: (type, qualifier).
type providerKey struct {
	typ       reflect.Type
	qualifier string
}

func (k providerKey) String() string {
	if k.qualifier == "" {
		return k.typ.String()
	}
	return k.typ.String() + "#" + k.qualifier
}

type provider struct {
	key      providerKey
	lifetime Lifetime
	build    func(*Resolver) (any, error)

	mu       sync.Mutex
	instance any
	resolved bool
}

// Registry holds bindings.
type Registry struct {
	mu        sync.RWMutex
	providers map[providerKey]*provider

	started        bool
	hookBuildOrder []*provider // singletons in the order they finished building
}

// Resolver is the per-resolution context handed to factories. It carries
// the dependency stack (for cycle detection) and the active Scope.
type Resolver struct {
	reg   *Registry
	scope *Scope
	stack []providerKey
}

// Registry returns the underlying registry. Useful for advanced cases.
func (rv *Resolver) Registry() *Registry { return rv.reg }

// New returns a new empty Registry.
func New() *Registry {
	return &Registry{providers: make(map[providerKey]*provider)}
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// Provide registers a Singleton factory for type T.
func Provide[T any](r *Registry, factory func(*Resolver) (T, error)) {
	provideWith[T](r, "", Singleton, factory)
}

// ProvideTagged registers a Singleton factory for type T with a qualifier.
// Multiple bindings of the same type may coexist if their qualifiers differ.
func ProvideTagged[T any](r *Registry, qualifier string, factory func(*Resolver) (T, error)) {
	provideWith[T](r, qualifier, Singleton, factory)
}

// ProvideTransient registers a Transient (per-resolve) factory for type T.
func ProvideTransient[T any](r *Registry, factory func(*Resolver) (T, error)) {
	provideWith[T](r, "", Transient, factory)
}

// ProvideScoped registers a Scoped factory for type T. The factory is
// invoked once per Scope.
func ProvideScoped[T any](r *Registry, factory func(*Resolver) (T, error)) {
	provideWith[T](r, "", Scoped, factory)
}

// ProvideValue registers an already-constructed value of type T.
func ProvideValue[T any](r *Registry, value T) {
	provideWith[T](r, "", Singleton, func(_ *Resolver) (T, error) { return value, nil })
}

func provideWith[T any](r *Registry, qualifier string, lt Lifetime, factory func(*Resolver) (T, error)) {
	var zero T
	typ := reflect.TypeOf(&zero).Elem()
	key := providerKey{typ: typ, qualifier: qualifier}

	p := &provider{
		key:      key,
		lifetime: lt,
		build: func(rv *Resolver) (any, error) {
			v, err := factory(rv)
			if err != nil {
				return nil, err
			}
			return v, nil
		},
	}
	r.mu.Lock()
	r.providers[key] = p
	r.mu.Unlock()
}

// ----------------------------------------------------------------------------
// Resolution
// ----------------------------------------------------------------------------

// Resolve resolves a value of type T from r (no scope).
func Resolve[T any](r *Registry) (T, error) {
	rv := &Resolver{reg: r}
	return resolveTyped[T](rv, "")
}

// ResolveTagged resolves a value of type T with the given qualifier.
func ResolveTagged[T any](r *Registry, qualifier string) (T, error) {
	rv := &Resolver{reg: r}
	return resolveTyped[T](rv, qualifier)
}

// MustResolve resolves or panics.
func MustResolve[T any](r *Registry) T {
	v, err := Resolve[T](r)
	if err != nil {
		panic(err)
	}
	return v
}

// From is called from inside a factory to resolve a nested dependency.
// Cycle detection and scope are propagated through the *Resolver.
func From[T any](rv *Resolver) (T, error) {
	return resolveTyped[T](rv, "")
}

// FromTagged is the qualified variant of From.
func FromTagged[T any](rv *Resolver, qualifier string) (T, error) {
	return resolveTyped[T](rv, qualifier)
}

// MustFrom panics on error.
func MustFrom[T any](rv *Resolver) T {
	v, err := From[T](rv)
	if err != nil {
		panic(err)
	}
	return v
}

func resolveTyped[T any](rv *Resolver, qualifier string) (T, error) {
	var zero T
	typ := reflect.TypeOf(&zero).Elem()
	key := providerKey{typ: typ, qualifier: qualifier}

	r := rv.reg
	r.mu.RLock()
	p, ok := r.providers[key]
	r.mu.RUnlock()
	if !ok {
		return zero, fmt.Errorf("%w: %s", ErrNotFound, key)
	}

	// Cycle detection.
	for _, k := range rv.stack {
		if k == key {
			return zero, fmt.Errorf("%w: %s -> %s", ErrCycle, formatStack(rv.stack), key)
		}
	}

	switch p.lifetime {
	case Scoped:
		if rv.scope == nil {
			return zero, fmt.Errorf("%w: %s", ErrScopedOutside, key)
		}
		if v, ok := rv.scope.get(key); ok {
			return castOrErr[T](v, key)
		}
	case Singleton:
		p.mu.Lock()
		if p.resolved {
			v := p.instance
			p.mu.Unlock()
			return castOrErr[T](v, key)
		}
		p.mu.Unlock()
	}

	// Build: push key, recurse, pop.
	child := &Resolver{
		reg:   r,
		scope: rv.scope,
		stack: append(append([]providerKey(nil), rv.stack...), key),
	}
	v, err := p.build(child)
	if err != nil {
		return zero, fmt.Errorf("di: build %s: %w", key, err)
	}

	switch p.lifetime {
	case Singleton:
		p.mu.Lock()
		if !p.resolved {
			p.instance = v
			p.resolved = true
			r.mu.Lock()
			r.hookBuildOrder = append(r.hookBuildOrder, p)
			r.mu.Unlock()
		} else {
			// Lost a race; prefer the existing instance.
			v = p.instance
		}
		p.mu.Unlock()
	case Scoped:
		rv.scope.set(key, v)
	}

	return castOrErr[T](v, key)
}

func castOrErr[T any](v any, key providerKey) (T, error) {
	out, ok := v.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: have %T want %s", ErrTypeMismatch, v, key)
	}
	return out, nil
}

func formatStack(s []providerKey) string {
	parts := make([]string, len(s))
	for i, k := range s {
		parts[i] = k.String()
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " -> "
		}
		out += p
	}
	return out
}

// ----------------------------------------------------------------------------
// Scopes
// ----------------------------------------------------------------------------

// Scope is a child resolution context with its own cache for Scoped
// bindings (e.g. per-request, per-tenant).
type Scope struct {
	parent *Registry
	mu     sync.Mutex
	values map[providerKey]any
}

// NewScope creates a new Scope rooted at this registry.
func (r *Registry) NewScope() *Scope {
	return &Scope{parent: r, values: make(map[providerKey]any)}
}

func (s *Scope) get(k providerKey) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[k]
	return v, ok
}

func (s *Scope) set(k providerKey, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[k] = v
}

// ResolveInScope resolves T using this scope (Scoped bindings cached here).
func ResolveInScope[T any](s *Scope) (T, error) {
	rv := &Resolver{reg: s.parent, scope: s}
	return resolveTyped[T](rv, "")
}

// ResolveTaggedInScope is the qualified variant.
func ResolveTaggedInScope[T any](s *Scope, qualifier string) (T, error) {
	rv := &Resolver{reg: s.parent, scope: s}
	return resolveTyped[T](rv, qualifier)
}

// ----------------------------------------------------------------------------
// Lifecycle: Start / Stop
// ----------------------------------------------------------------------------

// Start eagerly instantiates all Singleton bindings, then invokes OnStart on
// each resolved value implementing Lifecycle, in build order (leaves first).
// Build order is captured naturally by nested From[T](rv) calls inside
// factories.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return ErrAlreadyStarted
	}
	r.mu.Unlock()

	r.mu.RLock()
	keys := make([]providerKey, 0, len(r.providers))
	for k, p := range r.providers {
		if p.lifetime == Singleton {
			keys = append(keys, k)
		}
	}
	r.mu.RUnlock()
	// Deterministic order for stable build sequence.
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })

	for _, k := range keys {
		rv := &Resolver{reg: r}
		if _, err := resolveAny(rv, k); err != nil {
			return err
		}
	}

	r.mu.Lock()
	r.started = true
	order := append([]*provider(nil), r.hookBuildOrder...)
	r.mu.Unlock()

	for _, p := range order {
		if lc, ok := p.instance.(Lifecycle); ok {
			if err := lc.OnStart(ctx); err != nil {
				return fmt.Errorf("di: OnStart %s: %w", p.key, err)
			}
		}
	}
	return nil
}

// Stop invokes OnStop in reverse build order. Returns the first error.
func (r *Registry) Stop(ctx context.Context) error {
	r.mu.Lock()
	if !r.started {
		r.mu.Unlock()
		return ErrNotStarted
	}
	order := append([]*provider(nil), r.hookBuildOrder...)
	r.started = false
	r.mu.Unlock()

	var firstErr error
	for i := len(order) - 1; i >= 0; i-- {
		p := order[i]
		if lc, ok := p.instance.(Lifecycle); ok {
			if err := lc.OnStop(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("di: OnStop %s: %w", p.key, err)
			}
		}
	}
	return firstErr
}

// resolveAny is the type-erased path used by Start to build singletons by key.
func resolveAny(rv *Resolver, key providerKey) (any, error) {
	r := rv.reg
	r.mu.RLock()
	p, ok := r.providers[key]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}

	for _, k := range rv.stack {
		if k == key {
			return nil, fmt.Errorf("%w: %s -> %s", ErrCycle, formatStack(rv.stack), key)
		}
	}

	if p.lifetime == Singleton {
		p.mu.Lock()
		if p.resolved {
			v := p.instance
			p.mu.Unlock()
			return v, nil
		}
		p.mu.Unlock()
	}

	child := &Resolver{
		reg:   r,
		scope: rv.scope,
		stack: append(append([]providerKey(nil), rv.stack...), key),
	}
	v, err := p.build(child)
	if err != nil {
		return nil, fmt.Errorf("di: build %s: %w", key, err)
	}
	if p.lifetime == Singleton {
		p.mu.Lock()
		if !p.resolved {
			p.instance = v
			p.resolved = true
			r.mu.Lock()
			r.hookBuildOrder = append(r.hookBuildOrder, p)
			r.mu.Unlock()
		} else {
			v = p.instance
		}
		p.mu.Unlock()
	}
	return v, nil
}

// ----------------------------------------------------------------------------
// Introspection
// ----------------------------------------------------------------------------

// Keys returns the list of registered keys, sorted, for debugging.
func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k.String())
	}
	sort.Strings(out)
	return out
}
