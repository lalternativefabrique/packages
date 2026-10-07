package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchStaticRefusesInternalAddressByDefault(t *testing.T) {
	UseHTTPClient(nil)
	t.Cleanup(func() { UseHTTPClient(&http.Client{Timeout: fetchTimeout}) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the default client reached a loopback server")
	}))
	defer srv.Close()

	if _, err := FetchStatic(context.Background(), srv.URL, 6000, nil); err == nil {
		t.Fatal("FetchStatic reached a loopback address")
	}
}

func TestFetchStaticRefusesRedirectToInternalAddressByDefault(t *testing.T) {
	UseHTTPClient(nil)
	t.Cleanup(func() { UseHTTPClient(&http.Client{Timeout: fetchTimeout}) })

	req, _ := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", nil)
	if err := direct.get().CheckRedirect(req, []*http.Request{{}}); err == nil {
		t.Fatal("redirect to the metadata endpoint was allowed")
	}
}

func TestUseHTTPClientIsUsedForDirectFetches(t *testing.T) {
	t.Cleanup(func() { UseHTTPClient(&http.Client{Timeout: fetchTimeout}) })
	c := &http.Client{}
	UseHTTPClient(c)
	if httpClient(fetchTimeout, false) != c {
		t.Fatal("injected client not used for a direct fetch")
	}
}
