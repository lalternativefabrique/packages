package projection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// KVCheckpointStore persists checkpoints in a NATS JetStream KV bucket.
// One key per projector: the key is the projector Name. The value is the
// JSON-encoded Checkpoint[ID].
//
// IDs must be JSON-marshalable as map keys. For non-string IDs, override
// the encoding by supplying a Codec.
type KVCheckpointStore[ID comparable] struct {
	kv    jetstream.KeyValue
	codec ckptCodec[ID]
}

type ckptCodec[ID comparable] interface {
	Marshal(Checkpoint[ID]) ([]byte, error)
	Unmarshal([]byte) (Checkpoint[ID], error)
}

type jsonCodec[ID comparable] struct{}

func (jsonCodec[ID]) Marshal(c Checkpoint[ID]) ([]byte, error)  { return json.Marshal(c) }
func (jsonCodec[ID]) Unmarshal(b []byte) (Checkpoint[ID], error) {
	var c Checkpoint[ID]
	if err := json.Unmarshal(b, &c); err != nil {
		return Checkpoint[ID]{}, err
	}
	if c.PerAggregate == nil {
		c.PerAggregate = map[ID]int{}
	}
	return c, nil
}

// NewKVCheckpointStore opens (or creates) a KV bucket and wires the store.
func NewKVCheckpointStore[ID comparable](
	ctx context.Context,
	js jetstream.JetStream,
	bucket string,
) (*KVCheckpointStore[ID], error) {
	if bucket == "" {
		return nil, errors.New("projection: bucket required")
	}
	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:      bucket,
			Description: "projection checkpoints",
			History:     1,
		})
		if err != nil {
			return nil, fmt.Errorf("projection: open kv %s: %w", bucket, err)
		}
	}
	return &KVCheckpointStore[ID]{kv: kv, codec: jsonCodec[ID]{}}, nil
}

func (s *KVCheckpointStore[ID]) Load(ctx context.Context, name string) (Checkpoint[ID], error) {
	entry, err := s.kv.Get(ctx, name)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return Checkpoint[ID]{}, ErrCheckpointNotFound
		}
		return Checkpoint[ID]{}, fmt.Errorf("projection: kv get %s: %w", name, err)
	}
	return s.codec.Unmarshal(entry.Value())
}

func (s *KVCheckpointStore[ID]) Save(ctx context.Context, name string, cp Checkpoint[ID]) error {
	data, err := s.codec.Marshal(cp)
	if err != nil {
		return fmt.Errorf("projection: marshal checkpoint: %w", err)
	}
	if _, err := s.kv.Put(ctx, name, data); err != nil {
		return fmt.Errorf("projection: kv put %s: %w", name, err)
	}
	return nil
}
