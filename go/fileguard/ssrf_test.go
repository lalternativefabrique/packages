package fileguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestValidateFetchURL_RejectsSSRFVectors(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:4000/internal",
		"http://localhost/x",
		"http://10.0.0.5/feed.xml",
		"http://172.16.0.1/feed.xml",
		"http://192.168.1.1/feed.xml",
		"http://[::1]/feed.xml",
		"http://0.0.0.0/",
		"http://0.1.2.3/",
		"http://224.0.0.1/",
		"file:///etc/passwd",
		"ftp://example.com/feed.xml",
		"gopher://example.com/",
		"javascript:alert(1)",
		"",
		"not-a-url",
	} {
		if err := ValidateFetchURL(u); err == nil {
			t.Errorf("ValidateFetchURL(%q) = nil, want rejection", u)
		}
	}
}

func TestValidateFetchURL_AllowsPublicHTTP(t *testing.T) {
	for _, u := range []string{
		"https://1.1.1.1/feed.xml",
		"http://8.8.8.8/feed.xml",
		"  https://1.1.1.1/a.mp3  ",
	} {
		if err := ValidateFetchURL(u); err != nil {
			t.Errorf("ValidateFetchURL(%q) = %v, want nil", u, err)
		}
	}
}

// The message reaches callers that may be unauthenticated, so it must not
// report which address a host resolved to: that would make the endpoint a
// probe of the internal network.
func TestValidateFetchURL_ErrorNamesNoAddress(t *testing.T) {
	err := ValidateFetchURL("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("metadata endpoint accepted")
	}
	if strings.Contains(err.Error(), "169.254") {
		t.Errorf("error echoes the resolved address: %v", err)
	}
}

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", "169.254.169.254", "169.254.1.1",
		"10.1.2.3", "172.20.0.1", "192.168.0.1",
		"0.0.0.0", "0.1.2.3", "fe80::1", "224.0.0.1", "ff02::1",
	}
	for _, s := range blocked {
		if !IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("IsBlockedIP(%s) = false, want true", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700::1111"} {
		if IsBlockedIP(net.ParseIP(s)) {
			t.Errorf("IsBlockedIP(%s) = true, want false", s)
		}
	}
	if !IsBlockedIP(nil) {
		t.Error("IsBlockedIP(nil) = false, want true")
	}
}

// The transport is what closes rebinding: a host that passed ValidateFetchURL
// can resolve elsewhere by connection time, and only the dial can catch that.
func TestSafeHTTPClient_RefusesInternalAtDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// srv.URL is loopback — here it stands for a public name whose DNS answer
	// changed after validation.
	if _, err := SafeHTTPClient(5 * time.Second).Get(srv.URL); err == nil {
		t.Fatal("connection to a loopback address succeeded")
	}
}

func TestSafeHTTPClient_BoundsRedirects(t *testing.T) {
	c := SafeHTTPClient(7 * time.Second)
	if c.Timeout != 7*time.Second {
		t.Errorf("Timeout = %v, want 7s", c.Timeout)
	}
	if c.CheckRedirect == nil {
		t.Fatal("CheckRedirect is nil: redirect chains are unbounded")
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err := c.CheckRedirect(req, make([]*http.Request, maxFetchRedirects)); err == nil {
		t.Error("redirect chain past the cap was allowed")
	}

	ftp := httptest.NewRequest(http.MethodGet, "https://example.com/", nil)
	ftp.URL.Scheme = "ftp"
	if err := c.CheckRedirect(ftp, nil); err == nil {
		t.Error("redirect to a non-http scheme was allowed")
	}
}

// A caller wanting its own client settings must still get the dialing
// guarantee, or it silently loses the only control that closes rebinding.
func TestSafeTransport_GuardsItsOwnClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := &http.Client{Timeout: 5 * time.Second, Transport: SafeTransport()}
	if _, err := c.Get(srv.URL); err == nil {
		t.Fatal("connection to a loopback address succeeded")
	}
}
