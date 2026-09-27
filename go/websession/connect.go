package websession

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/lalternative/packages/go/svcauth"
)

// ConnectorClient is urbangate's client, the only caller that may ask a
// product what a person could share, while it draws the consent screen.
const ConnectorClient = "urbangate-connect"

// Scope is a right the product lets a person grant an app, labelled for the
// consent screen: "Lire tes campagnes", not "partage:read".
type Scope struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// Resource is something of the person's the product can share, named
// "<product>:<type>:<id>". Parent makes a tree: granting a parent grants its
// children, present and future. URL lets the app match it with its own data.
type Resource struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Label  string `json:"label"`
	Parent string `json:"parent,omitempty"`
	URL    string `json:"url,omitempty"`
}

// ConnectConfig is what a product declares to join ADR 0012.
type ConnectConfig struct {
	Scopes []Scope
	// Resources lists what the person holds now, with the right to share it.
	// Returning an error wrapping ErrUnavailable answers 503.
	Resources func(ctx context.Context, identityID string) ([]Resource, error)
}

// Connect serves the two routes of ADR 0012 under /connect/v1/:
//
//	GET grantable?subject=<identity>  urbangate-connect's token: all the
//	                                  person may share
//	GET resources                     an app's grant: what it was given,
//	                                  still held by the person
//
// Mount it on "/connect/v1/" outside Require.
func (g *Guard) Connect(cfg ConnectConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /connect/v1/grantable", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := svcauth.BearerToken(r)
		claims, err := g.verifier.Verify(r.Context(), raw)
		if errors.Is(err, ErrUnavailable) {
			unavailable(w)
			return
		}
		if err != nil || raw == "" {
			unauthenticated(w)
			return
		}
		if claims.ClientID != ConnectorClient || claims.Subject != ConnectorClient {
			forbidden(w)
			return
		}
		subject := r.URL.Query().Get("subject")
		if subject == "" {
			http.Error(w, `{"error":"subject_required"}`, http.StatusBadRequest)
			return
		}
		resources, ok := list(w, r, cfg, subject)
		if !ok {
			return
		}
		writeJSON(w, grantable{Scopes: nonNil(cfg.Scopes), Resources: resources})
	})
	mux.HandleFunc("GET /connect/v1/resources", func(w http.ResponseWriter, r *http.Request) {
		raw := g.tokenFrom(r)
		gr, err := g.ResolveDelegated(r.Context(), raw)
		if errors.Is(err, ErrUnavailable) {
			unavailable(w)
			return
		}
		if err != nil || raw == "" {
			unauthenticated(w)
			return
		}
		held, ok := list(w, r, cfg, gr.Subject)
		if !ok {
			return
		}
		scopes := []Scope{}
		for _, s := range cfg.Scopes {
			if gr.HasScope(s.Name) {
				scopes = append(scopes, s)
			}
		}
		writeJSON(w, grantable{Scopes: scopes, Resources: Granted(held, gr)})
	})
	return mux
}

type grantable struct {
	Scopes    []Scope    `json:"scopes"`
	Resources []Resource `json:"resources"`
}

func list(w http.ResponseWriter, r *http.Request, cfg ConnectConfig, subject string) ([]Resource, bool) {
	resources, err := cfg.Resources(r.Context(), subject)
	if errors.Is(err, ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		unavailable(w)
		return nil, false
	}
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"resources_unavailable"}`))
		return nil, false
	}
	return nonNil(resources), true
}

// Granted keeps the resources a grant reaches among those the person still
// holds, walking each one's parents through the same list.
func Granted(held []Resource, gr Grant) []Resource {
	parent := make(map[string]string, len(held))
	for _, r := range held {
		parent[r.ID] = r.Parent
	}
	out := []Resource{}
	for _, r := range held {
		if gr.Allows(Chain(r.ID, parent)...) {
			out = append(out, r)
		}
	}
	return out
}

// Chain is a resource's id followed by its ancestors', bounded so a cycle in
// the product's data cannot loop.
func Chain(id string, parent map[string]string) []string {
	chain := []string{id}
	for p := parent[id]; p != "" && len(chain) <= len(parent); p = parent[p] {
		chain = append(chain, p)
	}
	return chain
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
