package membership_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/membership"
)

func TestIdentifierHandler(t *testing.T) {
	svc, store := newService(t, nil)
	_, _ = store.Insert(context.Background(), membership.Member{IdentityID: "x", LocalID: "x", Address: "taken@messag.test", State: membership.StateReady}, nil)
	h := svc.IdentifierHandler()
	cases := map[string]int{
		"/api/v1/machine/identifiers/free":       http.StatusNoContent,
		"/api/v1/machine/identifiers/taken":      http.StatusConflict,
		"/api/v1/machine/identifiers/postmaster": http.StatusBadRequest,
		"/api/v1/machine/identifiers/bad..name":  http.StatusBadRequest,
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Errorf("GET %s = %d, want %d: %s", path, rec.Code, want, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/machine/identifiers/free", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rec.Code)
	}
}

func TestMeHandler(t *testing.T) {
	svc, _ := newService(t, nil)
	identity := func(r *http.Request) (membership.Identity, bool) {
		id := r.Header.Get("X-Test-Identity")
		return membership.Identity{ID: id, Email: id + "@messag.test"}, id != ""
	}
	h := svc.MeHandler(identity)
	call := func(method, id string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/me", nil)
		if id != "" {
			req.Header.Set("X-Test-Identity", id)
		}
		h.ServeHTTP(rec, req)
		return rec
	}
	if rec := call(http.MethodGet, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", rec.Code)
	}
	rec := call(http.MethodGet, "ana")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"provisioning"`) || !strings.Contains(rec.Body.String(), `"address":"ana@messag.test"`) {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if _, err := svc.WorkOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec := call(http.MethodGet, "ana"); !strings.Contains(rec.Body.String(), `"state":"ready"`) {
		t.Fatalf("GET after work = %s", rec.Body)
	}
	if rec := call(http.MethodDelete, "ana"); rec.Code != http.StatusAccepted {
		t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodGet, "ana"); rec.Code != http.StatusForbidden {
		t.Fatalf("GET after delete = %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodGet, "postmaster"); rec.Code != http.StatusConflict {
		t.Fatalf("GET reserved = %d %s", rec.Code, rec.Body)
	}
}
