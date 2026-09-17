package sdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestEveryCallerFacingRouteIsGenerated asserts the deny list against the
// contract itself.
//
// This is the test the hand-written client this package replaces did not have.
// That one covered /search alone and stayed unaware of /map and /crawl for as
// long as they existed, because nothing compared it to what vvaves serves. A
// route added upstream now fails here rather than being quietly absent.
func TestEveryCallerFacingRouteIsGenerated(t *testing.T) {
	raw, err := os.ReadFile("openapi/vvaves.json")
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string   `json:"operationId"`
			Tags        []string `json:"tags"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the contract is not JSON: %v", err)
	}

	gen, err := os.ReadFile("internal/wire/wire.gen.go")
	if err != nil {
		t.Fatalf("reading the generated transport: %v", err)
	}

	for path, methods := range doc.Paths {
		for method, op := range methods {
			// The admin surface is authenticated by an operator's JWT, not by
			// an application key: an application has nothing to call there.
			if hasTag(op.Tags, "admin") {
				continue
			}
			if op.OperationID == "" {
				t.Errorf("%s %s has no operationId, so nothing can be generated for it", method, path)
				continue
			}
			want := strings.ToUpper(op.OperationID[:1]) + op.OperationID[1:]
			if !strings.Contains(string(gen), "func (c *Client) "+want) {
				t.Errorf("%s %s (%s) is in the contract but not in the transport — run go generate ./...", method, path, op.OperationID)
			}
		}
	}
}

// The admin surface must stay out: it is the one thing the deny list excludes,
// and a tag renamed upstream would silently pull it in.
func TestAdminRoutesAreNotGenerated(t *testing.T) {
	gen, err := os.ReadFile("internal/wire/wire.gen.go")
	if err != nil {
		t.Fatalf("reading the generated transport: %v", err)
	}
	if strings.Contains(strings.ToLower(string(gen)), "/admin") {
		t.Error("an admin route reached the transport; it is authenticated by an operator JWT, not an app key")
	}
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
