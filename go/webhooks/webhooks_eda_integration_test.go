//go:build integration

// Integration net for the webhooks EDA seam, written BEFORE the eda-lib refactor
// so it pins the observable contract: create/update/delete an endpoint through
// the real Service (event store → NATS), and assert both read models converge —
// the KV bucket (dispatcher lookup) and the Postgres webhook_endpoints table
// (list/get API). Everything is real (NATS JetStream + Postgres); tests SKIP
// when a backend is unreachable.
//
// Run: DATABASE_URL=... NATS_URL=... go test -tags=integration ./webhooks/...
package webhooks_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/lalternative/packages/go/webhooks"
	"github.com/lalternative/packages/go/webhooks/applications/create_endpoint"
	"github.com/lalternative/packages/go/webhooks/applications/get_endpoint"
	"github.com/lalternative/packages/go/webhooks/applications/update_endpoint"
	"github.com/lalternative/packages/go/webhooks/domain/events"
)

func backendsOrSkip(t *testing.T) (*nats.Conn, *pgxpool.Pool) {
	t.Helper()
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	nc, err := nats.Connect(natsURL, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("nats unreachable at %s: %v", natsURL, err)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		nc.Close()
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		nc.Close()
		t.Skipf("postgres unreachable: %v", err)
	}
	return nc, pool
}

// waitFor polls cond until true or the deadline, so we don't race the async
// projectors.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

func TestIntegration_Webhooks_CreateProjectsToBothReadModels(t *testing.T) {
	nc, pool := backendsOrSkip(t)
	defer nc.Close()
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Clean slate: the WEBHOOK_EVENTS stream, the KV bucket, and the PG table.
	resetWebhookState(t, nc, pool)

	svc, err := webhooks.NewService(webhooks.ServiceDeps{NC: nc, Pool: pool, Catalog: events.Catalog{"email.sent"}})
	require.NoError(t, err)
	svc.StartBackground(ctx)

	// Create an endpoint through the real command handler → emits EndpointCreated
	// into the event store, which both projectors consume.
	res, err := svc.Create.Handle(ctx, create_endpoint.Command{
		TenantID:   "tenant-A",
		URL:        "https://example.test/hook",
		EventTypes: []string{"email.sent"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, res.ID)

	// PG read model: the endpoint appears in the list for its tenant.
	require.True(t, waitFor(t, 5*time.Second, func() bool {
		got, err := svc.Get.Handle(ctx, get_endpoint.Query{ID: res.ID, TenantID: "tenant-A"})
		return err == nil && got != nil && got.Status == "active"
	}), "endpoint never reached the PG read model as active")

	// KV read model: the endpoint is an active lookup target for the tenant.
	require.True(t, waitFor(t, 5*time.Second, func() bool {
		return kvHasActiveEndpoint(t, nc, "tenant-A", res.ID)
	}), "endpoint never reached the KV read model as active")
}

func TestIntegration_Webhooks_UpdateDisablesInBothReadModels(t *testing.T) {
	nc, pool := backendsOrSkip(t)
	defer nc.Close()
	defer pool.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resetWebhookState(t, nc, pool)
	svc, err := webhooks.NewService(webhooks.ServiceDeps{NC: nc, Pool: pool, Catalog: events.Catalog{"email.sent"}})
	require.NoError(t, err)
	svc.StartBackground(ctx)

	res, err := svc.Create.Handle(ctx, create_endpoint.Command{
		TenantID: "tenant-B", URL: "https://b.test/hook", EventTypes: []string{"email.sent"},
	})
	require.NoError(t, err)
	require.True(t, waitFor(t, 5*time.Second, func() bool {
		got, err := svc.Get.Handle(ctx, get_endpoint.Query{ID: res.ID, TenantID: "tenant-B"})
		return err == nil && got != nil
	}))

	// Disable it.
	err = svc.Update.Handle(ctx, update_endpoint.Command{
		ID: res.ID, TenantID: "tenant-B", URL: "https://b.test/hook",
		EventTypes: []string{"email.sent"}, Disabled: true,
	})
	require.NoError(t, err)

	// PG read model reflects disabled.
	require.True(t, waitFor(t, 5*time.Second, func() bool {
		got, err := svc.Get.Handle(ctx, get_endpoint.Query{ID: res.ID, TenantID: "tenant-B"})
		return err == nil && got != nil && got.Status == "disabled"
	}), "disable never propagated to PG read model")

	// KV read model no longer lists it as active.
	require.True(t, waitFor(t, 5*time.Second, func() bool {
		return !kvHasActiveEndpoint(t, nc, "tenant-B", res.ID)
	}), "disabled endpoint still active in KV read model")
}
