package appkeys

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type keyRow struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Audience  []string `json:"audience"`
	Scopes    []string `json:"scopes"`
	CreatedAt *string  `json:"createdAt"`
	ExpiresAt *string  `json:"expiresAt"`
}

type createRequest struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

func writeError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// Relay serves what <AppKeys/> calls: GET, POST and DELETE on the product's
// own route. The provisioner credential is used here and never travels further
// than this process.
//
// Mount it under the path the front is given, stripped of that prefix:
//
//	mux.Handle("/api/keys", http.StripPrefix("/api/keys", keys.Relay()))
//	mux.Handle("/api/keys/", http.StripPrefix("/api/keys", keys.Relay()))
func (k *Keys) Relay() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, ok := k.ownerOf(r)
		if !ok || owner == "" {
			writeError(w, http.StatusUnauthorized, "sign_in_required")
			return
		}
		id := strings.Trim(r.URL.Path, "/")
		switch {
		case r.Method == http.MethodGet && id == "":
			k.listKeys(w, r, owner)
		case r.Method == http.MethodPost && id == "":
			k.create(w, r, owner)
		case r.Method == http.MethodDelete && id != "":
			k.revoke(w, r, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method")
		}
	})
}

// call sends one authenticated request to urbangate. The status and the body
// come back untouched: the caller decides what each status means, because the
// three 503s urbangate answers are three different repairs and squashing them
// here would lose the only thing that tells them apart.
func (k *Keys) call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, k.urbangate+path, reader)
	if err != nil {
		return 0, nil, err
	}
	token, err := k.provisioner.Token(ctx)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := k.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, answer, nil
}

// reasonOf reads urbangate's own error name, so a product logs which of the
// three 503s it hit: no_vocabulary is a missing client, no_signing_key a
// deployment without a key, revocation_not_recorded a core that could not
// write. They are repaired in three different places.
func reasonOf(body []byte) string {
	var parsed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && parsed.Error != "" {
		return parsed.Error
	}
	return "unavailable"
}

// relayFailure passes urbangate's answer through. A 503 stays a 503: the
// caller retries, and nothing it sent is in question.
func relayFailure(w http.ResponseWriter, status int, body []byte) {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusUnprocessableEntity:
		writeError(w, status, reasonOf(body))
	default:
		writeError(w, http.StatusServiceUnavailable, reasonOf(body))
	}
}

func (k *Keys) listKeys(w http.ResponseWriter, r *http.Request, owner string) {
	path := "/api/machine/keys?owner=" + url.QueryEscape(owner)
	status, body, err := k.call(r.Context(), http.MethodGet, path, nil)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if status != http.StatusOK {
		relayFailure(w, status, body)
		return
	}
	var answer struct {
		Keys []keyRow `json:"keys"`
	}
	if json.Unmarshal(body, &answer) != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if answer.Keys == nil {
		answer.Keys = []keyRow{}
	}
	writeJSON(w, http.StatusOK, answer)
}

func (k *Keys) create(w http.ResponseWriter, r *http.Request, owner string) {
	var input createRequest
	if json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&input) != nil {
		writeError(w, http.StatusUnprocessableEntity, "label")
		return
	}
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" {
		writeError(w, http.StatusUnprocessableEntity, "label")
		return
	}
	scopes := input.Scopes
	if len(scopes) == 0 {
		scopes = k.defaults
	}
	status, body, err := k.call(r.Context(), http.MethodPost, "/api/machine/keys", map[string]any{
		"owner":    owner,
		"label":    input.Label,
		"audience": []string{k.product},
		"scopes":   scopes,
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if status != http.StatusOK && status != http.StatusCreated {
		relayFailure(w, status, body)
		return
	}
	var issued struct {
		ClientID string `json:"client_id"`
		Key      string `json:"key"`
	}
	// The key exists in this response and nowhere else. An answer without one
	// is a failure, not a key: accepting it would leave a row nobody can use
	// and no way to notice.
	if json.Unmarshal(body, &issued) != nil || issued.Key == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        issued.ClientID,
		"label":     input.Label,
		"audience":  []string{k.product},
		"scopes":    scopes,
		"createdAt": nil,
		"expiresAt": nil,
		"secret":    issued.Key,
	})
}

func (k *Keys) revoke(w http.ResponseWriter, r *http.Request, id string) {
	path := "/api/machine/keys/" + url.PathEscape(id)
	status, body, err := k.call(r.Context(), http.MethodDelete, path, nil)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// A 503 here means urbangate could not record the revocation and therefore
	// deleted nothing. Answering anything but a failure would tell the person
	// a live key is gone.
	if status != http.StatusNoContent && status != http.StatusOK {
		relayFailure(w, status, body)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
