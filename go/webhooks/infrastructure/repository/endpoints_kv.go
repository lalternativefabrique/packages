package repository

import (
	"context"
	"errors"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	edakv "github.com/lalternative/packages/go/eda/pkg/kvstore"

	"github.com/lalternative/packages/go/webhooks/domain/repository"
)

const KVBucket = "WEBHOOK_ENDPOINTS"

// kvEntry is the wire shape stored in the KV bucket. Keyed by endpoint id so
// the projector can update by id; the dispatcher loads the full set and filters
// by tenant + event type.
type kvEntry struct {
	ID         string   `json:"id"`
	TenantID   string   `json:"tenantId"`
	URL        string   `json:"url"`
	Secret     string   `json:"secret"`
	EventTypes []string `json:"eventTypes"`
	Status     string   `json:"status"` // "active" | "disabled" | "deleted"
}

// EndpointsKV is both the projector target (via Put/Delete) and the dispatcher
// lookup (via ActiveByTenant). The KV is a fast cache fed from WEBHOOK_EVENTS —
// the event stream remains the source of truth.
//
// It is a thin adapter over the shared eda kvstore.Store, which owns the KV
// bucket lifecycle, JSON encoding and the scan/filter primitives.
type EndpointsKV struct {
	store *edakv.Store[string, kvEntry]
}

// NewEndpointsKV opens (or creates) the endpoints KV bucket via the shared
// eda kvstore. It takes a v2 jetstream.JetStream (the lib's API).
func NewEndpointsKV(ctx context.Context, js jetstream.JetStream) (*EndpointsKV, error) {
	store, err := edakv.Open[string, kvEntry](ctx, js, KVBucket, jetstream.FileStorage)
	if err != nil {
		return nil, err
	}
	return &EndpointsKV{store: store}, nil
}

func (e *EndpointsKV) Put(ctx context.Context, entry kvEntry) error {
	return e.store.Put(ctx, entry.ID, entry)
}

func (e *EndpointsKV) Delete(ctx context.Context, id string) error {
	return e.store.Delete(ctx, id)
}

func (e *EndpointsKV) Get(ctx context.Context, id string) (*kvEntry, error) {
	v, err := e.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, edakv.ErrNotFound) {
			return nil, repository.ErrNotFound
		}
		return nil, err
	}
	return &v, nil
}

// ActiveByTenant returns the active endpoints for a tenant. It filters the
// bucket by tenant + status via the shared store's Filter primitive.
func (e *EndpointsKV) ActiveByTenant(ctx context.Context, tenantID string) ([]repository.ActiveEndpoint, error) {
	entries, err := e.store.Filter(ctx, func(v kvEntry) bool {
		return v.TenantID == tenantID && strings.EqualFold(v.Status, "active")
	})
	if err != nil {
		return nil, err
	}
	out := make([]repository.ActiveEndpoint, 0, len(entries))
	for _, v := range entries {
		out = append(out, repository.ActiveEndpoint{
			ID:         v.ID,
			TenantID:   v.TenantID,
			URL:        v.URL,
			Secret:     v.Secret,
			EventTypes: append([]string(nil), v.EventTypes...),
		})
	}
	return out, nil
}

// PutEntry is the projector-facing helper to upsert an endpoint snapshot.
func (e *EndpointsKV) PutEntry(ctx context.Context, id, tenantID, url, secret string, eventTypes []string, status string) error {
	return e.Put(ctx, kvEntry{
		ID:         id,
		TenantID:   tenantID,
		URL:        url,
		Secret:     secret,
		EventTypes: append([]string(nil), eventTypes...),
		Status:     status,
	})
}

// UpdateSecret rotates the stored HMAC secret while preserving the rest of the
// entry. Returns ErrNotFound if the endpoint is unknown.
func (e *EndpointsKV) UpdateSecret(ctx context.Context, id, secret string) error {
	prev, err := e.Get(ctx, id)
	if err != nil {
		return err
	}
	prev.Secret = secret
	return e.Put(ctx, *prev)
}

// guard
var _ repository.EndpointActiveLookup = (*EndpointsKV)(nil)
