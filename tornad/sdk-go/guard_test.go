package sdk

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlainHTTPOffClusterIsRefused(t *testing.T) {
	for _, base := range []string{"http://tornad.example.com", "ftp://tornad.internal", "tornad.internal"} {
		_, err := New(base, "k").Search(context.Background(), SearchQuery{Query: "x"})
		if !errors.Is(err, ErrInsecureBaseURL) {
			t.Errorf("%s: err = %v, want ErrInsecureBaseURL", base, err)
		}
	}
}

func TestPlainHTTPInClusterIsAccepted(t *testing.T) {
	for _, base := range []string{
		"http://127.0.0.1:8080", "http://localhost", "http://tornad",
		"http://app.tornad-production.internal", "http://tornad.ns.svc.cluster.local", "http://10.0.0.4",
		"https://tornad.example.com",
	} {
		if err := New(base, "k").ready(); err != nil {
			t.Errorf("%s: %v", base, err)
		}
	}
	if err := New("http://tornad.example.com", "k", WithInsecureHTTP()).ready(); err != nil {
		t.Errorf("WithInsecureHTTP: %v", err)
	}
}

func TestResponseLargerThanTheCeilingFails(t *testing.T) {
	big := `{"html":"` + strings.Repeat("a", 4096) + `"}`
	for name, announce := range map[string]bool{"content-length": true, "chunked": false} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if !announce {
				w.(http.Flusher).Flush()
			}
			w.Write([]byte(big))
		}))
		_, err := New(srv.URL, "k", WithMaxResponseBytes(1024)).Render(context.Background(), RenderRequest{URL: "https://e.com"})
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Errorf("%s: err = %v, want ErrResponseTooLarge", name, err)
		}
		srv.Close()
	}
}
