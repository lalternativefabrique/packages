//go:build integration

// Run: go test -tags=integration ./...
//
// Needs a NATS server with JetStream, >= 2.11 for the atomic OCC the store
// defaults to. Point NATS_URL at a non-default one.
package composition_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"github.com/nats-io/nats.go"

	"github.com/lalternative/packages/go/composition"
)

func connectOrSkip(t *testing.T) *nats.Conn {
	t.Helper()
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}
	nc, err := nats.Connect(url, nats.Timeout(2*time.Second))
	if err != nil {
		t.Skipf("NATS not reachable at %s: %v", url, err)
	}
	t.Cleanup(nc.Close)
	return nc
}

// TestFeatures runs the very same scenarios as the in-memory suite against a
// real JetStream stream: what passes here passed there, or the fake was
// lying.
func TestFeatures(t *testing.T) {
	nc := connectOrSkip(t)
	store, err := composition.NewJetStreamStore(context.Background(), nc)
	if err != nil {
		t.Fatalf("event store: %v", err)
	}

	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			// One store for the whole suite: each scenario writes under a
			// fresh aggregate id, so they never read each other's events.
			registerSteps(sc, func() composition.Store { return store })
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"composition.feature"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature tests failed against a real NATS server")
	}
}
