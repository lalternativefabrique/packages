//go:build !integration

package composition_test

import (
	"testing"

	"github.com/cucumber/godog"

	"github.com/lalternative/packages/go/eda/pkg/db"

	"github.com/lalternative/packages/go/composition"
)

// TestFeatures runs the scenarios against the in-memory event store, which
// enforces the same contiguous-version OCC as JetStream does. The identical
// feature file runs against a real server under -tags=integration, so the two
// are held to one set of expectations rather than to a fake's own.
func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			registerSteps(sc, func() composition.Store {
				return db.NewInMemoryStore[composition.ID]()
			})
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"composition.feature"},
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("feature tests failed")
	}
}
