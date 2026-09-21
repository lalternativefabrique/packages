package appkeys

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

// fakeUrbangate stands in for the machine API. It records what it was sent, so
// a test can assert the relay asked the right thing and not only that it
// answered well.
type fakeUrbangate struct {
	server *httptest.Server
	seen   []*http.Request
	auth   []string

	status int
	body   string
}

func newFakeUrbangate(t *testing.T, handler http.HandlerFunc) *fakeUrbangate {
	t.Helper()
	f := &fakeUrbangate{status: http.StatusOK}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen = append(f.seen, r)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))
	}))
	t.Cleanup(f.server.Close)
	return f
}

func testKeys(t *testing.T, urbangate string, owner string) *Keys {
	t.Helper()
	k, err := New(Config{
		Product:     "tornad",
		Urbangate:   urbangate,
		Provisioner: staticToken("machine-token"),
		OwnerOf: func(*http.Request) (string, bool) {
			return owner, owner != ""
		},
		DefaultScopes: []string{"search"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return k
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRelayListsTheOwnersKeys(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	fake.body = `{"keys":[{"id":"ak_1","label":"prod","audience":["tornad"],"scopes":["tornad:search"],"createdAt":"2026-09-20T08:00:00Z","expiresAt":"2027-09-20T08:00:00Z"}]}`
	k := testKeys(t, fake.server.URL, "8f3a-1c2d")

	rec := do(t, k.Relay(), http.MethodGet, "/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var answer struct {
		Keys []keyRow `json:"keys"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatalf("body is not the front's shape: %v", err)
	}
	if len(answer.Keys) != 1 || answer.Keys[0].ID != "ak_1" {
		t.Fatalf("keys = %+v", answer.Keys)
	}
	if answer.Keys[0].ExpiresAt == nil || *answer.Keys[0].ExpiresAt == "" {
		t.Error("expiresAt was dropped on the way through")
	}
	if got := fake.seen[0].URL.Query().Get("owner"); got != "8f3a-1c2d" {
		t.Errorf("owner asked for = %q", got)
	}
}

func TestRelayAnswersAnEmptyListAsAnArray(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	fake.body = `{}`
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodGet, "/", "")
	if body := strings.TrimSpace(rec.Body.String()); body != `{"keys":[]}` {
		t.Errorf("body = %s, want an empty array rather than null", body)
	}
}

func TestRelayCreatesAndReturnsTheKeyOnce(t *testing.T) {
	fake := newFakeUrbangate(t, func(w http.ResponseWriter, r *http.Request) {
		var sent map[string]any
		_ = json.NewDecoder(r.Body).Decode(&sent)
		if sent["owner"] != "8f3a" {
			t.Errorf("owner sent = %v", sent["owner"])
		}
		if aud, _ := sent["audience"].([]any); len(aud) != 1 || aud[0] != "tornad" {
			t.Errorf("audience sent = %v, want the product", sent["audience"])
		}
		_, _ = w.Write([]byte(`{"client_id":"ak_7f3a","key":"tornad_key_eyJhbGc"}`))
	})
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodPost, "/", `{"label":"prod"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var created struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID != "ak_7f3a" || created.Secret != "tornad_key_eyJhbGc" {
		t.Errorf("created = %+v, want urbangate's key under the front's name", created)
	}
}

// The default scopes belong to the product; the vocabulary they name belongs
// to urbangate, which is what refuses an unknown one.
func TestRelayFallsBackToTheProductsDefaultScopes(t *testing.T) {
	var sent map[string]any
	fake := newFakeUrbangate(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"client_id":"ak_1","key":"tornad_key_x"}`))
	})
	k := testKeys(t, fake.server.URL, "8f3a")

	do(t, k.Relay(), http.MethodPost, "/", `{"label":"prod"}`)
	scopes, _ := sent["scopes"].([]any)
	if len(scopes) != 1 || scopes[0] != "search" {
		t.Errorf("scopes sent = %v, want the configured default", sent["scopes"])
	}
}

// A creation whose answer carries no key is a failure, not a key: the value is
// never fetched again, so accepting the row would leave one nobody can use.
func TestRelayRefusesACreationWithoutAKey(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	fake.body = `{"client_id":"ak_1"}`
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodPost, "/", `{"label":"prod"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestRelayRevokes(t *testing.T) {
	fake := newFakeUrbangate(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodDelete, "/ak_7f3a", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if path := fake.seen[0].URL.Path; path != "/api/machine/keys/ak_7f3a" {
		t.Errorf("path = %q", path)
	}
}

// urbangate records a revocation before it deletes, so a 503 means nothing was
// deleted. Turning it into a success would tell someone a live key is gone.
func TestRelayNeverTurnsAFailedRevocationIntoASuccess(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	fake.status = http.StatusServiceUnavailable
	fake.body = `{"error":"revocation_not_recorded"}`
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodDelete, "/ak_7f3a", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "revocation_not_recorded") {
		t.Errorf("body = %s, want urbangate's own reason so a product can log it", rec.Body)
	}
}

// The three 503s are three repairs in three places. The relay keeps the name
// so a product's logs say which one, even though its front shows one message.
func TestRelayKeepsUrbangatesReason(t *testing.T) {
	for _, reason := range []string{"no_vocabulary", "no_signing_key"} {
		fake := newFakeUrbangate(t, nil)
		fake.status = http.StatusServiceUnavailable
		fake.body = `{"error":"` + reason + `"}`
		k := testKeys(t, fake.server.URL, "8f3a")

		rec := do(t, k.Relay(), http.MethodPost, "/", `{"label":"prod"}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", reason, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), reason) {
			t.Errorf("%s: body = %s", reason, rec.Body)
		}
	}
}

func TestRelayPassesTheCallersOwnRefusals(t *testing.T) {
	for _, tc := range []struct{ status int }{
		{http.StatusUnauthorized}, {http.StatusForbidden}, {http.StatusUnprocessableEntity},
	} {
		fake := newFakeUrbangate(t, nil)
		fake.status = tc.status
		fake.body = `{"error":"scope"}`
		k := testKeys(t, fake.server.URL, "8f3a")

		rec := do(t, k.Relay(), http.MethodPost, "/", `{"label":"prod"}`)
		if rec.Code != tc.status {
			t.Errorf("status = %d, want %d passed through", rec.Code, tc.status)
		}
	}
}

func TestRelayRefusesWithoutASession(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	k := testKeys(t, fake.server.URL, "")

	rec := do(t, k.Relay(), http.MethodGet, "/", "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if len(fake.seen) != 0 {
		t.Error("urbangate was called for a request with no signed-in person")
	}
}

func TestRelayRefusesAnEmptyLabel(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodPost, "/", `{"label":"  "}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
	if len(fake.seen) != 0 {
		t.Error("urbangate was called for a key with no name")
	}
}

// The machine credential is the one thing that must never reach a browser.
func TestRelayAuthenticatesToUrbangateAndLeaksNothing(t *testing.T) {
	fake := newFakeUrbangate(t, nil)
	fake.body = `{"keys":[]}`
	k := testKeys(t, fake.server.URL, "8f3a")

	rec := do(t, k.Relay(), http.MethodGet, "/", "")
	if fake.auth[0] != "Bearer machine-token" {
		t.Errorf("authorization sent = %q", fake.auth[0])
	}
	if strings.Contains(rec.Body.String(), "machine-token") {
		t.Error("the provisioner token reached the response")
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no product":     {Urbangate: "https://id", Provisioner: staticToken("t"), OwnerOf: func(*http.Request) (string, bool) { return "o", true }},
		"no urbangate":   {Product: "tornad", Provisioner: staticToken("t"), OwnerOf: func(*http.Request) (string, bool) { return "o", true }},
		"no provisioner": {Product: "tornad", Urbangate: "https://id", OwnerOf: func(*http.Request) (string, bool) { return "o", true }},
		"no owner":       {Product: "tornad", Urbangate: "https://id", Provisioner: staticToken("t")},
	} {
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: New accepted a config that cannot serve", name)
		}
	}
}
