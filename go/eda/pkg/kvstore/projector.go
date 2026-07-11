// projector.go bridges a KV entity Store to the durable consumer, so a read
// model living in a KV bucket can be maintained from an event stream without
// hand-rolling a pull loop.
//
// The mechanics — durable pull, ack/nak, staged backoff, DLQ, heartbeat,
// reconnect — come from pkg/consumer. The application supplies only the
// event→mutation decision via a Project function; everything JetStream is owned
// by the consumer. Wire the returned handler with consumer.Run.
package kvstore

import (
	"context"

	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/eda/pkg/consumer"
)

// Mutation is the effect an event has on the KV read model: upsert an entity,
// delete one by key, or do nothing. Construct one with Put, Delete, or Noop.
type Mutation[K comparable, V any] struct {
	op    mutationOp
	key   K
	value V
}

type mutationOp int

const (
	opNoop mutationOp = iota
	opPut
	opDelete
)

// Put upserts value under k.
func Put[K comparable, V any](k K, value V) Mutation[K, V] {
	return Mutation[K, V]{op: opPut, key: k, value: value}
}

// Delete removes k.
func Delete[K comparable, V any](k K) Mutation[K, V] {
	return Mutation[K, V]{op: opDelete, key: k}
}

// Noop applies nothing — for events the read model does not care about.
func Noop[K comparable, V any]() Mutation[K, V] {
	return Mutation[K, V]{op: opNoop}
}

// Projector is a consumer.EventHandler that applies each event to a KV Store.
// The Project func decodes a message and returns the resulting Mutation; the
// Projector applies it against the store. Because it satisfies EventHandler, it
// inherits the consumer's redelivery semantics: a Project or apply error is
// returned to the consumer, which naks and retries (or dead-letters after
// MaxDeliver). Idempotency: Put/Delete are naturally idempotent, so a redelivery
// re-applies the same mutation harmlessly.
type Projector[K comparable, V any] struct {
	store    *Store[K, V]
	name     string
	subject  string
	durable  string
	maxDeliv int
	project  func(ctx context.Context, msg *nats.Msg) (Mutation[K, V], error)
}

// ProjectorConfig configures a Projector.
type ProjectorConfig[K comparable, V any] struct {
	// Name identifies the projector in logs/metrics. Required.
	Name string
	// Subject is the NATS subject filter the consumer binds to. Required.
	Subject string
	// Durable is the durable consumer name (projection scope). Required.
	Durable string
	// MaxDeliver bounds delivery attempts before dead-lettering. Default: 5.
	MaxDeliver int
	// Project decodes one message into the mutation to apply. Return a Noop for
	// events the read model ignores. Wrap consumer.Permanent for undecodable
	// payloads so they dead-letter instead of retrying forever.
	Project func(ctx context.Context, msg *nats.Msg) (Mutation[K, V], error)
}

// NewProjector builds a Projector over store. Wire it with consumer.Run:
//
//	p := kvstore.NewProjector(store, cfg)
//	go consumer.Run(ctx, nc, p, consumer.Config{StreamName: "...", ...})
func NewProjector[K comparable, V any](store *Store[K, V], cfg ProjectorConfig[K, V]) *Projector[K, V] {
	maxDeliver := cfg.MaxDeliver
	if maxDeliver <= 0 {
		maxDeliver = 5
	}
	return &Projector[K, V]{
		store:    store,
		name:     cfg.Name,
		subject:  cfg.Subject,
		durable:  cfg.Durable,
		maxDeliv: maxDeliver,
		project:  cfg.Project,
	}
}

func (p *Projector[K, V]) Name() string        { return p.name }
func (p *Projector[K, V]) Subject() string     { return p.subject }
func (p *Projector[K, V]) DurableName() string { return p.durable }
func (p *Projector[K, V]) MaxDeliver() int     { return p.maxDeliv }

// Handle decodes the message via Project and applies the mutation to the store.
func (p *Projector[K, V]) Handle(ctx context.Context, msg *nats.Msg) error {
	m, err := p.project(ctx, msg)
	if err != nil {
		return err
	}
	switch m.op {
	case opPut:
		return p.store.Put(ctx, m.key, m.value)
	case opDelete:
		return p.store.Delete(ctx, m.key)
	default: // opNoop
		return nil
	}
}

var _ consumer.EventHandler = (*Projector[string, struct{}])(nil)
