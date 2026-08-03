// Package memstore holds in-memory implementations of the webhooks ports.
//
// They exist so the delivery logic — retry classification, attempt recording,
// terminal outcomes — can be tested without a NATS server. That logic is the
// part of this package most worth covering and the part an integration test
// covers most expensively, so the doubles are shipped rather than duplicated in
// every consumer's test tree.
//
// Not for production: state lives in a map and dies with the process.
package memstore

import (
	"context"
	"sync"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
	"github.com/lalternative/packages/go/webhooks/domain/events"
	"github.com/lalternative/packages/go/webhooks/domain/providers"
	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

// EventStore is an in-memory repository.EndpointRepository.
//
// It keeps the event log per aggregate and replays it on Load, exactly as the
// JetStream store does — so a test exercises real rehydration rather than a
// snapshot the production path never takes.
type EventStore struct {
	mu     sync.Mutex
	events map[string][]events.EventWithMetadata
}

func NewEventStore() *EventStore {
	return &EventStore{events: map[string][]events.EventWithMetadata{}}
}

// Save appends the aggregate's uncommitted events to its log.
func (s *EventStore) Save(_ context.Context, e *aggregate.Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := e.UncommittedEvents()
	if len(pending) == 0 {
		return nil
	}
	s.events[e.ID] = append(s.events[e.ID], pending...)
	e.MarkCommitted()
	return nil
}

// Load rebuilds the aggregate from its events. An unknown id yields a
// zero-version aggregate rather than an error, matching what the callers of
// this port already branch on (Version == 0 means "gone").
func (s *EventStore) Load(_ context.Context, id string) (*aggregate.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := aggregate.NewEndpoint(id)
	e.Replay(s.events[id])
	return e, nil
}

// Events returns a copy of one aggregate's log, for assertions on what was
// recorded.
func (s *EventStore) Events(id string) []events.EventWithMetadata {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]events.EventWithMetadata(nil), s.events[id]...)
}

// KindsFor returns the event kinds recorded for an aggregate, in order — the
// readable form of an assertion on a delivery's history.
func (s *EventStore) KindsFor(id string) []string {
	out := []string{}
	for _, ev := range s.Events(id) {
		out = append(out, ev.EventKind())
	}
	return out
}

var _ repository.EndpointRepository = (*EventStore)(nil)

// Dispatcher is a providers.HTTPDispatcher that returns canned results and
// records what it was asked to send.
type Dispatcher struct {
	mu sync.Mutex
	// Results are returned in order; the last one repeats once exhausted, so a
	// test that retries N times declares only the outcomes it cares about.
	Results []providers.HTTPResult
	calls   []providers.DeliveryJob
}

func NewDispatcher(results ...providers.HTTPResult) *Dispatcher {
	return &Dispatcher{Results: results}
}

func (d *Dispatcher) Dispatch(_ context.Context, job providers.DeliveryJob) providers.HTTPResult {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, job)
	if len(d.Results) == 0 {
		return providers.HTTPResult{StatusCode: 200}
	}
	i := len(d.calls) - 1
	if i >= len(d.Results) {
		i = len(d.Results) - 1
	}
	return d.Results[i]
}

// Calls returns the jobs dispatched so far, in order.
func (d *Dispatcher) Calls() []providers.DeliveryJob {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]providers.DeliveryJob(nil), d.calls...)
}

var _ providers.HTTPDispatcher = (*Dispatcher)(nil)

// Outbox is a providers.OutboxPublisher that collects jobs instead of
// publishing them, so a dispatcher test can assert the fan-out.
type Outbox struct {
	mu   sync.Mutex
	jobs []providers.DeliveryJob
}

func NewOutbox() *Outbox { return &Outbox{} }

func (o *Outbox) Publish(_ context.Context, job providers.DeliveryJob) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.jobs = append(o.jobs, job)
	return nil
}

// Jobs returns the published jobs, in order.
func (o *Outbox) Jobs() []providers.DeliveryJob {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]providers.DeliveryJob(nil), o.jobs...)
}

var _ providers.OutboxPublisher = (*Outbox)(nil)

// Lookup is an in-memory repository.EndpointActiveLookup.
type Lookup struct {
	ByTenant map[string][]repository.ActiveEndpoint
}

func NewLookup() *Lookup {
	return &Lookup{ByTenant: map[string][]repository.ActiveEndpoint{}}
}

func (l *Lookup) Add(ep repository.ActiveEndpoint) {
	l.ByTenant[ep.TenantID] = append(l.ByTenant[ep.TenantID], ep)
}

func (l *Lookup) ActiveByTenant(_ context.Context, tenantID string) ([]repository.ActiveEndpoint, error) {
	return l.ByTenant[tenantID], nil
}

var _ repository.EndpointActiveLookup = (*Lookup)(nil)
