// Package kvstore provides a generic, JSON-encoded entity repository backed by
// a NATS JetStream KV bucket. It is the read-model / entity-cache primitive
// that the projection and processmanager packages do not cover: those store a
// projector checkpoint or a single process-manager instance, whereas kvstore
// holds many entities keyed by id and lets you scan/filter by a secondary
// attribute.
//
// Typical use is a read model fed by a durable consumer: the projector upserts
// entities with Put and deletes them with Delete, while a query path loads one
// with Get or the active subset with Filter. The KV bucket is a fast cache; the
// event stream it is projected from remains the source of truth.
//
// Bucket sizing note: List and Filter do a full key scan, which is fine for
// small-to-moderate buckets (thousands of keys). For large buckets, maintain a
// secondary-index bucket instead of scanning.
package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// ErrNotFound is returned by Get and Update when the key is absent.
var ErrNotFound = errors.New("kvstore: key not found")

// ErrConflict is returned by Update when the compare-and-set revision no longer
// matches — another writer updated the key first. Callers retry by re-reading.
var ErrConflict = errors.New("kvstore: revision conflict")

// Store is a generic entity repository over a KV bucket. K is the key type
// (rendered via fmt to a KV key string); V is the JSON-encoded value type.
type Store[K comparable, V any] struct {
	kv jetstream.KeyValue
}

// Open opens (or creates) the bucket and returns a Store. storage selects the
// bucket storage type; pass jetstream.FileStorage for durability or
// jetstream.MemoryStorage for ephemeral caches (matching a pre-existing
// bucket's storage type — JetStream rejects a change).
func Open[K comparable, V any](
	ctx context.Context,
	js jetstream.JetStream,
	bucket string,
	storage jetstream.StorageType,
) (*Store[K, V], error) {
	if bucket == "" {
		return nil, errors.New("kvstore: bucket required")
	}
	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:  bucket,
			Storage: storage,
		})
		if err != nil {
			return nil, fmt.Errorf("kvstore: open kv %s: %w", bucket, err)
		}
	}
	return &Store[K, V]{kv: kv}, nil
}

// key renders K to a KV key string. Strings pass through; everything else is
// formatted with %v so numeric/uuid-like ids work without a codec.
func key[K comparable](k K) string {
	if s, ok := any(k).(string); ok {
		return s
	}
	return fmt.Sprintf("%v", k)
}

// Put upserts value under k. It overwrites any existing entry.
func (s *Store[K, V]) Put(ctx context.Context, k K, value V) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("kvstore: marshal: %w", err)
	}
	if _, err := s.kv.Put(ctx, key(k), data); err != nil {
		return fmt.Errorf("kvstore: put %v: %w", k, err)
	}
	return nil
}

// Get loads the entity under k. It returns ErrNotFound if absent.
func (s *Store[K, V]) Get(ctx context.Context, k K) (V, error) {
	var zero V
	entry, err := s.kv.Get(ctx, key(k))
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return zero, ErrNotFound
		}
		return zero, fmt.Errorf("kvstore: get %v: %w", k, err)
	}
	var v V
	if err := json.Unmarshal(entry.Value(), &v); err != nil {
		return zero, fmt.Errorf("kvstore: unmarshal %v: %w", k, err)
	}
	return v, nil
}

// Delete removes k. Deleting a missing key is a no-op (no error).
func (s *Store[K, V]) Delete(ctx context.Context, k K) error {
	if err := s.kv.Delete(ctx, key(k)); err != nil && !errors.Is(err, jetstream.ErrKeyNotFound) {
		return fmt.Errorf("kvstore: delete %v: %w", k, err)
	}
	return nil
}

// List returns every value in the bucket. It scans all keys; see the package
// note on bucket sizing. Order is unspecified.
func (s *Store[K, V]) List(ctx context.Context) ([]V, error) {
	return s.Filter(ctx, func(V) bool { return true })
}

// Filter returns the values for which keep returns true. It scans all keys and
// decodes each value once; a key that fails to decode is skipped (a partial
// write or a schema drift never fails the whole query). Order is unspecified.
func (s *Store[K, V]) Filter(ctx context.Context, keep func(V) bool) ([]V, error) {
	keys, err := s.kv.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("kvstore: keys: %w", err)
	}
	out := make([]V, 0, len(keys))
	for _, k := range keys {
		entry, err := s.kv.Get(ctx, k)
		if err != nil {
			if errors.Is(err, jetstream.ErrKeyNotFound) {
				continue // deleted between Keys() and Get() — skip
			}
			return nil, fmt.Errorf("kvstore: get %s during scan: %w", k, err)
		}
		var v V
		if err := json.Unmarshal(entry.Value(), &v); err != nil {
			continue // undecodable entry — skip rather than fail the scan
		}
		if keep(v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// Update performs a read-modify-write with optimistic concurrency: it loads k,
// applies mutate, and writes the result back only if the entry was not changed
// concurrently. On a concurrent change it returns ErrConflict; the caller
// retries. Returns ErrNotFound if k is absent.
func (s *Store[K, V]) Update(ctx context.Context, k K, mutate func(V) (V, error)) error {
	entry, err := s.kv.Get(ctx, key(k))
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return ErrNotFound
		}
		return fmt.Errorf("kvstore: get %v: %w", k, err)
	}
	var v V
	if err := json.Unmarshal(entry.Value(), &v); err != nil {
		return fmt.Errorf("kvstore: unmarshal %v: %w", k, err)
	}
	next, err := mutate(v)
	if err != nil {
		return err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return fmt.Errorf("kvstore: marshal: %w", err)
	}
	// Compare-and-set against the revision we read, so a racing writer is
	// detected instead of silently overwritten.
	if _, err := s.kv.Update(ctx, key(k), data, entry.Revision()); err != nil {
		if isWrongLastRev(err) {
			return ErrConflict
		}
		return fmt.Errorf("kvstore: update %v: %w", k, err)
	}
	return nil
}

// isWrongLastRev reports whether err is JetStream's CAS rejection, surfaced
// when Update's expected revision no longer matches. Matched by string rather
// than by internal error code, consistent with pkg/processmanager — the nats KV
// client wraps the condition in an APIError whose numeric code is not part of
// its stable API.
func isWrongLastRev(err error) bool {
	return err != nil && (containsCI(err.Error(), "wrong last sequence") || containsCI(err.Error(), "cas mismatch"))
}

// containsCI reports whether sub appears in s, case-insensitively (ASCII).
func containsCI(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	if sub == "" {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
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
