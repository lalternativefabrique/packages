package svcauth_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lalternative/packages/go/svcauth"
)

func callee(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestTransportAuthorizesEveryRequest(t *testing.T) {
	issuer, hits, _ := tokenEndpoint(t, 900)
	target, seen := callee(t)
	cc := &svcauth.ClientCredentials{TokenURL: issuer.URL, ClientID: "lalter-core", ClientSecret: "s3cret", Audience: []string{"tornade"}}

	client := &http.Client{Transport: svcauth.Transport(cc, nil)}
	for i := 0; i < 2; i++ {
		resp, err := client.Get(target.URL + "/fetch")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	for _, got := range *seen {
		if got != "Bearer tok-tornade" {
			t.Fatalf("Authorization = %q", got)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("token endpoint hit %d times, want 1", hits.Load())
	}
}

func TestTransportLeavesTheCallersRequestAlone(t *testing.T) {
	issuer, _, _ := tokenEndpoint(t, 900)
	target, _ := callee(t)
	cc := &svcauth.ClientCredentials{TokenURL: issuer.URL, ClientID: "lalter-core", ClientSecret: "s3cret"}

	req, err := http.NewRequest(http.MethodGet, target.URL+"/fetch", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: svcauth.Transport(cc, nil)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("the caller's request was modified: Authorization = %q", got)
	}
}

func TestTransportFailsTheRequestWhenTheIssuerRefuses(t *testing.T) {
	issuer, _, _ := tokenEndpoint(t, 900)
	target, seen := callee(t)
	cc := &svcauth.ClientCredentials{TokenURL: issuer.URL, ClientID: "lalter-core", ClientSecret: "wrong"}

	_, err := (&http.Client{Transport: svcauth.Transport(cc, nil)}).Get(target.URL + "/fetch")
	if !errors.Is(err, svcauth.ErrTokenRefused) {
		t.Fatalf("err = %v", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("the request was sent unauthenticated: %v", *seen)
	}
}

func TestTransportKeepsTheBaseRoundTripper(t *testing.T) {
	issuer, _, _ := tokenEndpoint(t, 900)
	target, _ := callee(t)
	cc := &svcauth.ClientCredentials{TokenURL: issuer.URL, ClientID: "lalter-core", ClientSecret: "s3cret"}

	var wrapped bool
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		wrapped = true
		return http.DefaultTransport.RoundTrip(req)
	})
	resp, err := (&http.Client{Transport: svcauth.Transport(cc, base)}).Get(target.URL + "/fetch")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if !wrapped {
		t.Fatal("the base round tripper was bypassed")
	}
}

func TestHTTPClientAuthorizes(t *testing.T) {
	issuer, _, _ := tokenEndpoint(t, 900)
	target, seen := callee(t)
	cc := &svcauth.ClientCredentials{TokenURL: issuer.URL, ClientID: "lalter-core", ClientSecret: "s3cret", Audience: []string{"tornade"}}

	resp, err := cc.HTTPClient().Get(target.URL + "/fetch")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(*seen) != 1 || (*seen)[0] != "Bearer tok-tornade" {
		t.Fatalf("seen = %v", *seen)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
