package composition

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/lalternative/packages/go/eda/pkg/db"
	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

const StreamName = "COMPOSITION_EVENTS"

func Payloads() *db.PayloadRegistry {
	r := db.NewPayloadRegistry()
	r.Register(KindSourceUpdated, func() ddd.EventPayload { return &SourceUpdated{} })
	r.Register(KindVariantRequested, func() ddd.EventPayload { return &VariantRequested{} })
	r.Register(KindVariantGenerated, func() ddd.EventPayload { return &VariantGenerated{} })
	r.Register(KindVariantGenerationFailed, func() ddd.EventPayload { return &VariantGenerationFailed{} })
	r.Register(KindVariantEdited, func() ddd.EventPayload { return &VariantEdited{} })
	return r
}

// Store is the event log a Repository reads and writes. JetStreamStore is the
// production implementation; tests use db.InMemoryStore.
type Store interface {
	Load(ctx context.Context, id ID) ([]ddd.EventEnvelope[ID], error)
	LoadFromVersion(ctx context.Context, id ID, fromVersion int) ([]ddd.EventEnvelope[ID], error)
	Save(ctx context.Context, id ID, expectedVersion int, events []ddd.EventEnvelope[ID]) error
}

func NewJetStreamStore(ctx context.Context, nc *nats.Conn) (*db.JetStreamStore[ID], error) {
	return db.NewJetStreamStore[ID](ctx, nc, db.JetStreamStoreConfig[ID]{
		StreamName:            StreamName,
		SubjectPrefix:         "events",
		AggregateType:         AggregateType,
		Payloads:              Payloads(),
		IDs:                   db.StringIDCodec{},
		CreateStreamIfMissing: true,
	})
}

// SnapshotBucket holds one stored state per composition. The events stay the
// record of what happened; this only spares a load from replaying all of them.
const SnapshotBucket = "COMPOSITION_SNAPSHOTS"

// NewJetStreamSnapshots opens the bucket compositions are snapshotted into.
func NewJetStreamSnapshots(ctx context.Context, nc *nats.Conn) (ddd.SnapshotStore[ID], error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("composition snapshots: %w", err)
	}
	store, err := db.NewKVSnapshotStore[ID](ctx, js, SnapshotBucket, AggregateType, db.StringIDCodec{})
	if err != nil {
		return nil, err
	}
	return db.ForAggregates[ID](store), nil
}

type Repository struct {
	store     Store
	snapshots *ddd.SnapshotLoader[ID]
}

func NewRepository(store Store) *Repository {
	return &Repository{store: store}
}

// WithSnapshots makes the repository load through snapshots instead of the full
// stream. A repository without them is still correct, only slower on a text
// that has been written to for a long time, so this is wiring rather than a
// requirement — bootstrap adds it when there is somewhere to keep them.
func (r *Repository) WithSnapshots(store ddd.SnapshotStore[ID]) *Repository {
	r.snapshots = ddd.NewSnapshotLoader[ID](r.store, store, SnapshotInterval)
	return r
}

// Load returns an empty composition rather than an error when the stream holds
// nothing yet: a document that has never been written to is a document with no
// history, not a missing one.
func (r *Repository) Load(ctx context.Context, id ID) (*Composition, error) {
	c := New(id)
	loader := r.snapshots
	if loader == nil {
		loader = ddd.NewSnapshotLoader[ID](r.store, nil, SnapshotInterval)
	}
	if err := ddd.Load(ctx, loader, c, &c.BaseAggregateRoot, id); err != nil {
		if errors.Is(err, db.ErrAggregateNotFound) {
			return New(id), nil
		}
		return nil, err
	}
	return c, nil
}

// History returns the raw event log, which is what Replay, SourceVersions and
// VariantVersions read.
func (r *Repository) History(ctx context.Context, id ID) ([]ddd.EventEnvelope[ID], error) {
	return r.store.Load(ctx, id)
}

func (r *Repository) Save(ctx context.Context, c *Composition) error {
	pending := c.Uncommitted()
	if len(pending) == 0 {
		return nil
	}
	expected := c.Version() - len(pending)
	if err := r.store.Save(ctx, c.ID(), expected, pending); err != nil {
		return fmt.Errorf("save composition: %w", err)
	}
	c.MarkCommitted()
	// The events are committed, so a snapshot that does not get written costs
	// the next load a longer replay and nothing else. Failing the save over it
	// would turn an optimisation into a way to lose an author's text.
	if r.snapshots != nil {
		if err := ddd.MaybeSave(ctx, r.snapshots, c); err != nil {
			slog.Warn("composition: snapshot", "aggregate", c.ID(), "error", err)
		}
	}
	return nil
}
