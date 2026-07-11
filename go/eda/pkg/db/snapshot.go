package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// Snapshot is the on-disk shape of an aggregate snapshot. State is a
// JSON-serializable value provided by the caller — typically a flat DTO
// representing the aggregate's current state.
type Snapshot[ID comparable] struct {
	AggregateID      ID              `json:"aggregate_id"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateVersion int             `json:"aggregate_version"`
	State            json.RawMessage `json:"state"`
}

// ErrSnapshotNotFound is returned when no snapshot exists for the requested
// aggregate.
var ErrSnapshotNotFound = errors.New("eventstore: snapshot not found")

// SnapshotStore persists and retrieves aggregate snapshots. Implementations
// must be goroutine-safe.
type SnapshotStore[ID comparable] interface {
	// Save writes the snapshot, overwriting any previous one for the same
	// aggregate.
	Save(ctx context.Context, snap Snapshot[ID]) error
	// Load returns the latest snapshot for an aggregate, or
	// ErrSnapshotNotFound.
	Load(ctx context.Context, aggregateID ID) (Snapshot[ID], error)
	// Delete removes the snapshot. Idempotent.
	Delete(ctx context.Context, aggregateID ID) error
}

// ----------------------------------------------------------------------------
// In-memory implementation
// ----------------------------------------------------------------------------

// InMemorySnapshotStore is a goroutine-safe in-memory SnapshotStore.
type InMemorySnapshotStore[ID comparable] struct {
	mu   sync.RWMutex
	data map[ID]Snapshot[ID]
}

// NewInMemorySnapshotStore returns an empty store.
func NewInMemorySnapshotStore[ID comparable]() *InMemorySnapshotStore[ID] {
	return &InMemorySnapshotStore[ID]{data: make(map[ID]Snapshot[ID])}
}

func (s *InMemorySnapshotStore[ID]) Save(_ context.Context, snap Snapshot[ID]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[snap.AggregateID] = snap
	return nil
}

func (s *InMemorySnapshotStore[ID]) Load(_ context.Context, id ID) (Snapshot[ID], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.data[id]
	if !ok {
		return Snapshot[ID]{}, fmt.Errorf("%w: %v", ErrSnapshotNotFound, id)
	}
	return snap, nil
}

func (s *InMemorySnapshotStore[ID]) Delete(_ context.Context, id ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, id)
	return nil
}

// ----------------------------------------------------------------------------
// JetStream KV implementation
// ----------------------------------------------------------------------------

// KVSnapshotStore stores snapshots in a NATS JetStream key/value bucket.
// Keys are derived as AggregateType + "." + Encode(aggregateID).
type KVSnapshotStore[ID comparable] struct {
	kv    jetstream.KeyValue
	codec IDCodec[ID]
	atype string
}

// NewKVSnapshotStore wires the store, creating the bucket if missing.
func NewKVSnapshotStore[ID comparable](
	ctx context.Context,
	js jetstream.JetStream,
	bucket, aggregateType string,
	codec IDCodec[ID],
) (*KVSnapshotStore[ID], error) {
	if bucket == "" || aggregateType == "" {
		return nil, errors.New("eventstore: bucket and aggregateType required")
	}
	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:      bucket,
			Description: fmt.Sprintf("aggregate snapshots for %s", aggregateType),
			History:     1,
		})
		if err != nil {
			return nil, fmt.Errorf("eventstore: open kv bucket %s: %w", bucket, err)
		}
	}
	return &KVSnapshotStore[ID]{kv: kv, codec: codec, atype: aggregateType}, nil
}

func (s *KVSnapshotStore[ID]) key(id ID) string {
	return s.atype + "." + s.codec.Encode(id)
}

func (s *KVSnapshotStore[ID]) Save(ctx context.Context, snap Snapshot[ID]) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("eventstore: marshal snapshot: %w", err)
	}
	if _, err := s.kv.Put(ctx, s.key(snap.AggregateID), data); err != nil {
		return fmt.Errorf("eventstore: kv put: %w", err)
	}
	return nil
}

func (s *KVSnapshotStore[ID]) Load(ctx context.Context, id ID) (Snapshot[ID], error) {
	entry, err := s.kv.Get(ctx, s.key(id))
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return Snapshot[ID]{}, fmt.Errorf("%w: %v", ErrSnapshotNotFound, id)
		}
		return Snapshot[ID]{}, fmt.Errorf("eventstore: kv get: %w", err)
	}
	var snap Snapshot[ID]
	if err := json.Unmarshal(entry.Value(), &snap); err != nil {
		return Snapshot[ID]{}, fmt.Errorf("eventstore: unmarshal snapshot: %w", err)
	}
	return snap, nil
}

func (s *KVSnapshotStore[ID]) Delete(ctx context.Context, id ID) error {
	if err := s.kv.Delete(ctx, s.key(id)); err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil
		}
		return fmt.Errorf("eventstore: kv delete: %w", err)
	}
	return nil
}
