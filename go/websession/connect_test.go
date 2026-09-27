package websession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"
)

var lungorHeld = []Resource{
	{ID: "lungor:tenant:acme", Type: "tenant", Label: "Acme"},
	{ID: "lungor:app:crm", Type: "app", Label: "app-crm", Parent: "lungor:tenant:acme", URL: "https://crm.acme.fr"},
	{ID: "lungor:tenant:globex", Type: "tenant", Label: "Globex"},
	{ID: "lungor:app:shop", Type: "app", Label: "shop", Parent: "lungor:tenant:globex"},
}

func connectCfg(asked *string) ConnectConfig {
	return ConnectConfig{
		Scopes: []Scope{{Name: "lungor:read", Label: "Lire ta facturation"}, {Name: "lungor:write", Label: "Gérer tes abonnements"}},
		Resources: func(_ context.Context, id string) ([]Resource, error) {
			*asked = id
			return lungorHeld, nil
		},
	}
}

func get(h http.Handler, path, raw string) (int, grantable) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if raw != "" {
		req.Header.Set("Authorization", "Bearer "+raw)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body grantable
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestGrantableAnswersUrbangateOnly(t *testing.T) {
	var asked string
	g := NewWith(stub{claims: svcauth.Claims{Subject: ConnectorClient, ClientID: ConnectorClient}}, "lungor")
	code, body := get(g.Connect(connectCfg(&asked)), "/connect/v1/grantable?subject=8f3a", token(t, map[string]any{}))
	if code != http.StatusOK || asked != "8f3a" || len(body.Resources) != 4 || len(body.Scopes) != 2 {
		t.Fatalf("code=%d asked=%q body=%+v", code, asked, body)
	}
	for name, claims := range map[string]svcauth.Claims{
		"an app's grant":    {Subject: "8f3a", ClientID: "nakoda-connect"},
		"another service":   {Subject: "lalter-core", ClientID: "lalter-core"},
		"the product's own": {Subject: "8f3a", ClientID: "lungor-admin"},
	} {
		g := NewWith(stub{claims: claims}, "lungor")
		if code, _ := get(g.Connect(connectCfg(&asked)), "/connect/v1/grantable?subject=8f3a", token(t, map[string]any{})); code != http.StatusForbidden {
			t.Fatalf("%s: code=%d", name, code)
		}
	}
	if code, _ := get(g.Connect(connectCfg(&asked)), "/connect/v1/grantable", token(t, map[string]any{})); code != http.StatusBadRequest {
		t.Fatalf("no subject: code=%d", code)
	}
}

func TestResourcesAnswersWhatTheGrantReachesAndThePersonStillHolds(t *testing.T) {
	var asked string
	g := NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", ClientID: "nakoda-connect", Scopes: []string{"lungor:read"}}}, "lungor")
	raw := token(t, map[string]any{"resources": []string{"lungor:tenant:acme", "lungor:tenant:gone"}})
	code, body := get(g.Connect(connectCfg(&asked)), "/connect/v1/resources", raw)
	if code != http.StatusOK || asked != "8f3a" {
		t.Fatalf("code=%d asked=%q", code, asked)
	}
	var ids []string
	for _, r := range body.Resources {
		ids = append(ids, r.ID)
	}
	if fmt.Sprint(ids) != "[lungor:tenant:acme lungor:app:crm]" {
		t.Fatalf("resources = %v", ids)
	}
	if len(body.Scopes) != 1 || body.Scopes[0].Name != "lungor:read" {
		t.Fatalf("scopes = %+v", body.Scopes)
	}
	own := NewWith(stub{claims: svcauth.Claims{Subject: "8f3a", ClientID: "lungor-admin"}}, "lungor")
	if code, _ := get(own.Connect(connectCfg(&asked)), "/connect/v1/resources", raw); code != http.StatusUnauthorized {
		t.Fatalf("a session is not a grant: code=%d", code)
	}
}

func TestConnectUnavailable(t *testing.T) {
	g := NewWith(stub{claims: svcauth.Claims{Subject: ConnectorClient, ClientID: ConnectorClient}}, "lungor")
	cfg := ConnectConfig{Resources: func(context.Context, string) ([]Resource, error) {
		return nil, fmt.Errorf("db: %w", ErrUnavailable)
	}}
	if code, _ := get(g.Connect(cfg), "/connect/v1/grantable?subject=8f3a", token(t, map[string]any{})); code != http.StatusServiceUnavailable {
		t.Fatalf("code=%d", code)
	}
	cfg.Resources = func(context.Context, string) ([]Resource, error) { return nil, errors.New("boom") }
	if code, _ := get(g.Connect(cfg), "/connect/v1/grantable?subject=8f3a", token(t, map[string]any{})); code != http.StatusInternalServerError {
		t.Fatalf("code=%d", code)
	}
}

func TestChainSurvivesACycle(t *testing.T) {
	chain := Chain("a", map[string]string{"a": "b", "b": "a"})
	if len(chain) > 3 {
		t.Fatalf("chain = %v", chain)
	}
}
