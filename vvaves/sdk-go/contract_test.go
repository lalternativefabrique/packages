package sdk

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A route added upstream fails here rather than being quietly absent.
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
			if hasTag(op.Tags, "admin") || hasTag(op.Tags, "keys") {
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

// admin and keys are operator and console surfaces, never an app key's.
func TestOperatorRoutesAreNotGenerated(t *testing.T) {
	gen, err := os.ReadFile("internal/wire/wire.gen.go")
	if err != nil {
		t.Fatalf("reading the generated transport: %v", err)
	}
	if strings.Contains(strings.ToLower(string(gen)), "/api/") {
		t.Error("an /api/ route reached the transport; it takes an operator or console credential, not an app key")
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
