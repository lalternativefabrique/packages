package connect

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCallRelaysAWriteAndRetriesOnceAfterARefresh(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") == "Bearer a1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"p1","echo":` + string(body) + `}`))
	}))
	defer srv.Close()
	h := newHarness(Config{Products: Products{{ID: "partage", CoreURL: srv.URL}}}, HandlerConfig{})
	h.grants.client = ProductClient{HTTP: srv.Client()}
	h.connect("partage", "a1", h.now.Add(time.Hour))
	h.tokens.refresh = func(string) (Tokens, error) {
		return Tokens{Access: "a2", Refresh: "r2", ExpiresAt: h.now.Add(time.Hour)}, nil
	}
	var out struct {
		ID   string         `json:"id"`
		Echo map[string]any `json:"echo"`
	}
	err := h.grants.Call(context.Background(), owner, "partage", http.MethodPost, "/api/v1/posts", map[string]string{"text": "hi"}, &out)
	if err != nil || out.ID != "p1" || out.Echo["text"] != "hi" {
		t.Fatalf("%v %+v", err, out)
	}
	if len(seen) != 2 || seen[1] != "POST /api/v1/posts Bearer a2" {
		t.Fatalf("calls %v", seen)
	}
}

func TestAProductRefusalCarriesItsStatusAndCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"scope_missing","message":"partage:publish"}}`))
	}))
	defer srv.Close()
	err := ProductClient{HTTP: srv.Client()}.Do(context.Background(), Product{ID: "partage", CoreURL: srv.URL}, "a", http.MethodPost, "/x", nil, nil)
	var pe *ProductError
	if !errors.As(err, &pe) || pe.Status != 403 || pe.Code != "scope_missing" || pe.Message != "partage:publish" {
		t.Fatalf("%#v", err)
	}
}

func TestAProductOutageIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := ProductClient{HTTP: srv.Client()}.Resources(context.Background(), Product{ID: "partage", CoreURL: srv.URL}, "a")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("%v", err)
	}
}
