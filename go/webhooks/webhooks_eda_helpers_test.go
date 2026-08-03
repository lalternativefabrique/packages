//go:build integration

package webhooks_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	idempotencypgx "github.com/lalternative/packages/go/eda/pkg/idempotency/pgx"
	"github.com/lalternative/packages/go/webhooks/domain/aggregate"
)

// ensureSchema creates the tables the read models write to.
//
// webhook_endpoints ships with this library, in migrations/. event_idempotency
// belongs to eda's pgprojector, which this package consumes — its DDL is taken
// from the source that owns it rather than copied, so the two cannot drift.
func ensureSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), idempotencypgx.Schema)
	require.NoError(t, err)
}

// resetWebhookState clears the event stream, the KV bucket and the PG read-model
// table so each test starts clean. Missing resources are ignored.
func resetWebhookState(t *testing.T, nc *nats.Conn, pool *pgxpool.Pool) {
	t.Helper()
	js, err := nc.JetStream()
	require.NoError(t, err)

	_ = js.PurgeStream("WEBHOOK_EVENTS")
	if kv, err := js.KeyValue("WEBHOOK_ENDPOINTS"); err == nil {
		if keys, err := kv.Keys(); err == nil {
			for _, k := range keys {
				_ = kv.Delete(k)
			}
		}
	}
	ensureSchema(t, pool)
	_, err = pool.Exec(context.Background(), `TRUNCATE webhook_endpoints, event_idempotency`)
	require.NoError(t, err)
}

// kvEntry mirrors the shape stored in the WEBHOOK_ENDPOINTS bucket.
type kvEntry struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	Status   string `json:"status"`
}

// kvHasActiveEndpoint reports whether the KV read model holds id as an active
// endpoint for tenantID — the exact lookup the dispatcher relies on.
func kvHasActiveEndpoint(t *testing.T, nc *nats.Conn, tenantID, id string) bool {
	t.Helper()
	js, err := nc.JetStream()
	require.NoError(t, err)
	kv, err := js.KeyValue("WEBHOOK_ENDPOINTS")
	if err != nil {
		return false
	}
	entry, err := kv.Get(id)
	if err != nil {
		return false
	}
	var v kvEntry
	if err := json.Unmarshal(entry.Value(), &v); err != nil {
		return false
	}
	return v.TenantID == tenantID && strings.EqualFold(v.Status, "active")
}

// --- outbox worker test helpers ---

// mustJSv1 returns the legacy v1 JetStreamContext (what EnsureStream / the
// publisher still take during the incremental migration).
func mustJSv1(t *testing.T, nc *nats.Conn) nats.JetStreamContext {
	t.Helper()
	js, err := nc.JetStream()
	require.NoError(t, err)
	return js
}

// noopRepo is an EndpointRepository whose Load always returns an empty aggregate
// (Version 0 → "endpoint vanished"), so the worker records nothing and just
// dispatches. Enough to exercise the delivery path without seeding the store.
type noopRepo struct{}

func newNoopRepo() *noopRepo { return &noopRepo{} }

func (*noopRepo) Save(context.Context, *aggregate.Endpoint) error { return nil }
func (*noopRepo) Load(_ context.Context, id string) (*aggregate.Endpoint, error) {
	return aggregate.NewEndpoint(id), nil
}

func workQueueRetention() jetstream.RetentionPolicy { return jetstream.WorkQueuePolicy }
