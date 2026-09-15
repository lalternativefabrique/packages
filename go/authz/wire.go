package authz

import "encoding/json"

// The AuthZEN Evaluation API shapes, as sent and received on the wire.

type wireSubject struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

type wireAction struct {
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties,omitempty"`
}

type wireResource struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

type wireRequest struct {
	Subject  wireSubject    `json:"subject"`
	Action   wireAction     `json:"action"`
	Resource wireResource   `json:"resource"`
	Context  map[string]any `json:"context,omitempty"`
}

type wireResponse struct {
	Decision bool           `json:"decision"`
	Context  map[string]any `json:"context,omitempty"`
}

func toWire(r Request) wireRequest {
	props := map[string]any{
		"owner":     r.Subject.Owner,
		"end_user":  r.Subject.EndUser,
		"client_id": r.Subject.ClientID,
		"roles":     stringsOrEmpty(r.Subject.Roles),
		"scopes":    stringsOrEmpty(r.Subject.Scopes),
	}
	return wireRequest{
		Subject:  wireSubject{Type: r.Subject.Type(), ID: r.Subject.ID(), Properties: props},
		Action:   wireAction{Name: r.Action.Name, Properties: r.Action.Properties},
		Resource: wireResource{Type: r.Resource.Type, ID: r.Resource.ID, Properties: r.Resource.Properties},
		Context:  r.Context,
	}
}

func fromWire(w wireRequest) Request {
	p := w.Subject.Properties
	return Request{
		Subject: Subject{
			Owner:    str(p["owner"]),
			EndUser:  str(p["end_user"]),
			ClientID: str(p["client_id"]),
			Roles:    strs(p["roles"]),
			Scopes:   strs(p["scopes"]),
		},
		Action:   Action{Name: w.Action.Name, Properties: w.Action.Properties},
		Resource: Resource{Type: w.Resource.Type, ID: w.Resource.ID, Properties: w.Resource.Properties},
		Context:  w.Context,
	}
}

func decisionToWire(d Decision) wireResponse {
	ctx := map[string]any{}
	for k, v := range d.Context {
		ctx[k] = v
	}
	if d.Reason != "" {
		ctx["reason"] = d.Reason
	}
	if len(ctx) == 0 {
		ctx = nil
	}
	return wireResponse{Decision: d.Allow, Context: ctx}
}

func decisionFromWire(w wireResponse) Decision {
	d := Decision{Allow: w.Decision, Context: w.Context}
	if reason, ok := w.Context["reason"].(string); ok {
		d.Reason = reason
	}
	return d
}

// inputDocument is the request as a policy sees it: the wire shape, decoded
// into plain maps so an engine needs no knowledge of this package's types.
func inputDocument(r Request) (map[string]any, error) {
	raw, err := json.Marshal(toWire(r))
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func stringsOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func strs(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
