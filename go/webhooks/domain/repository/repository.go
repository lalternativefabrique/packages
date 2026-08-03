package repository

import (
	"context"
	"errors"
	"time"

	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
)

// EndpointRepository persists Endpoint aggregates to the event stream.
type EndpointRepository interface {
	Save(ctx context.Context, e *aggregate.Endpoint) error
	Load(ctx context.Context, id string) (*aggregate.Endpoint, error)
}

// ErrNotFound is returned by Load when no events exist for the aggregate id.
var ErrNotFound = errors.New("endpoint not found")

// EndpointView is the read-side projection (Postgres + KV).
type EndpointView struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenantId"`
	URL         string    `json:"url"`
	EventTypes  []string  `json:"eventTypes"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// EndpointReader is the read-side port. The Postgres implementation lives
// under infrastructure/.
type EndpointReader interface {
	List(ctx context.Context, tenantID string, limit, offset int) ([]EndpointView, error)
	Get(ctx context.Context, tenantID, id string) (*EndpointView, error)
}

// ActiveEndpoint is the minimal projection the dispatcher needs to fan out
// upstream events to subscribers. Secret is included so the worker can sign
// without re-reading the aggregate.
type ActiveEndpoint struct {
	ID         string
	TenantID   string
	URL        string
	Secret     string
	EventTypes []string
}

// EndpointActiveLookup gives the dispatcher the active endpoints for a
// tenant. Implementations are typically backed by a NATS KV bucket.
type EndpointActiveLookup interface {
	ActiveByTenant(ctx context.Context, tenantID string) ([]ActiveEndpoint, error)
}
