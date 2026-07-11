package processmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go/jetstream"
)

// KVStateStore persists Stored[S] in a NATS JetStream KV bucket with
// optimistic concurrency provided by the KV revision number.
//
// Keys are derived as "<defName>.<instanceID>". The KV revision serves as
// the OCC token: it is captured on Load and passed back on Save through
// the KV.Update operation, which fails with jetstream.ErrKeyWrongLast
// when the revision has changed.
type KVStateStore[S any] struct {
	kv jetstream.KeyValue
}

// NewKVStateStore opens (or creates) a KV bucket and wires the store.
func NewKVStateStore[S any](
	ctx context.Context,
	js jetstream.JetStream,
	bucket string,
) (*KVStateStore[S], error) {
	if bucket == "" {
		return nil, errors.New("processmanager: bucket required")
	}
	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:      bucket,
			Description: "process manager state",
			History:     1,
		})
		if err != nil {
			return nil, fmt.Errorf("processmanager: open kv %s: %w", bucket, err)
		}
	}
	return &KVStateStore[S]{kv: kv}, nil
}

func (s *KVStateStore[S]) key(defName, instanceID string) string {
	return defName + "." + instanceID
}

// kvStored is the on-disk shape; it carries Version both inside the value
// (for application-level OCC checks) and via the KV revision (for
// server-side atomic CAS).
type kvStored[S any] struct {
	Version     int    `json:"version"`
	State       S      `json:"state"`
	LastEventID string `json:"last_event_id"`
	Done        bool   `json:"done"`
	// KVRevision is set on Load and used by Save; not persisted.
	KVRevision uint64 `json:"-"`
}

func (s *KVStateStore[S]) Load(ctx context.Context, defName, instanceID string) (Stored[S], error) {
	entry, err := s.kv.Get(ctx, s.key(defName, instanceID))
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return Stored[S]{}, ErrInstanceNotFound
		}
		return Stored[S]{}, fmt.Errorf("processmanager: kv get: %w", err)
	}
	var v kvStored[S]
	if err := json.Unmarshal(entry.Value(), &v); err != nil {
		return Stored[S]{}, fmt.Errorf("processmanager: unmarshal: %w", err)
	}
	return Stored[S]{
		Version:     v.Version,
		State:       v.State,
		LastEventID: v.LastEventID,
		Done:        v.Done,
		// Encode the KV revision into the application Version's high bits
		// so Save can recover it. We use a side-channel map keyed by
		// (defName,instanceID) instead — simpler and safer.
	}, s.rememberRevision(defName, instanceID, entry.Revision())
}

func (s *KVStateStore[S]) Save(ctx context.Context, defName, instanceID string, expectedVersion int, next Stored[S]) error {
	key := s.key(defName, instanceID)
	v := kvStored[S]{
		Version:     next.Version,
		State:       next.State,
		LastEventID: next.LastEventID,
		Done:        next.Done,
	}
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("processmanager: marshal: %w", err)
	}

	rev := s.recallRevision(defName, instanceID)
	if rev == 0 {
		// First write: Create fails if the key already exists.
		if _, err := s.kv.Create(ctx, key, data); err != nil {
			if isKeyExists(err) {
				return fmt.Errorf("%w: key %s already exists", ErrConcurrencyConflict, key)
			}
			return fmt.Errorf("processmanager: kv create: %w", err)
		}
		return nil
	}
	if _, err := s.kv.Update(ctx, key, data, rev); err != nil {
		if isWrongLastRev(err) {
			return fmt.Errorf("%w: kv revision mismatch", ErrConcurrencyConflict)
		}
		return fmt.Errorf("processmanager: kv update: %w", err)
	}
	return nil
}

// We track per-key KV revisions in-process. This is sufficient as long as
// a single engine instance handles a given process instance at a time
// (the OCC on the KV side guarantees safety across instances anyway —
// Update fails when a stale revision is used).
var kvRevisions = newRevisionMap()

func (s *KVStateStore[S]) rememberRevision(defName, instanceID string, rev uint64) error {
	kvRevisions.set(s.key(defName, instanceID), rev)
	return nil
}

func (s *KVStateStore[S]) recallRevision(defName, instanceID string) uint64 {
	return kvRevisions.get(s.key(defName, instanceID))
}

// revisionMap is a tiny goroutine-safe map[string]uint64.
type revisionMap struct {
	mu sync.RWMutex
	m  map[string]uint64
}

func newRevisionMap() *revisionMap { return &revisionMap{m: make(map[string]uint64)} }

func (r *revisionMap) get(k string) uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[k]
}

func (r *revisionMap) set(k string, v uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[k] = v
}

func isKeyExists(err error) bool {
	// nats KV returns a wrapped APIError; we match by string to avoid
	// importing internal error codes.
	return err != nil && (containsCI(err.Error(), "key exists") || containsCI(err.Error(), "wrong last sequence: 0"))
}

func isWrongLastRev(err error) bool {
	return err != nil && (containsCI(err.Error(), "wrong last sequence") || containsCI(err.Error(), "cas mismatch"))
}

func containsCI(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	if sub == "" {
		return true
	}
	// case-insensitive ASCII contains
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a := s[i+j]
			b := sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

