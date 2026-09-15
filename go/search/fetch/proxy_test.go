package fetch

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// resetProxy keeps one test's configuration from leaking into the next: the
// proxy is deployment-wide state by design.
func resetProxy(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { UseProxy("") })
}

const proxiedArticle = `<html><body><article><p>Le corps de l'article, assez long pour que readability le garde comme contenu principal de la page.</p></article></body></html>`

// refusingOrigin answers 403 to a direct request and serves the article to
// a proxied one, the way a publisher behind bot management treats a
// datacenter address and a residential one.
func refusingOrigin(t *testing.T) (origin, proxy *httptest.Server, direct, proxied *atomic.Int32) {
	t.Helper()
	direct, proxied = &atomic.Int32{}, &atomic.Int32{}
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Via-Proxy") == "" {
			direct.Add(1)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		proxied.Add(1)
		w.Write([]byte(proxiedArticle))
	}))
	t.Cleanup(origin.Close)
	proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() {
			t.Errorf("proxy got %q, want an absolute-URI request", r.URL)
		}
		req, _ := http.NewRequest(http.MethodGet, r.URL.String(), nil)
		req.Header.Set("X-Via-Proxy", "1")
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	return origin, proxy, direct, proxied
}

func TestFetchEscalatesToTheProxyWhenDirectIsRefused(t *testing.T) {
	resetProxy(t)
	origin, proxy, direct, proxied := refusingOrigin(t)
	if err := UseProxy(proxy.URL); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}

	page, err := FetchStatic(context.Background(), origin.URL+"/a", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if direct.Load() != 1 || proxied.Load() != 1 {
		t.Fatalf("direct %d proxied %d, want one try each", direct.Load(), proxied.Load())
	}
	if !strings.Contains(page.Text, "Le corps de l'article") {
		t.Errorf("got text %q, want the proxied content", page.Text)
	}

	if _, err := FetchStatic(context.Background(), origin.URL+"/b", 6000, nil); err != nil {
		t.Fatalf("second FetchStatic: %v", err)
	}
	if direct.Load() != 1 || proxied.Load() != 2 {
		t.Errorf("direct %d proxied %d, want the host remembered as refusing", direct.Load(), proxied.Load())
	}
}

func TestFetchStaysDirectOnAnOpenHost(t *testing.T) {
	resetProxy(t)
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		http.Error(w, "should not be used", http.StatusBadGateway)
	}))
	defer proxy.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(proxiedArticle))
	}))
	defer origin.Close()
	if err := UseProxy(proxy.URL); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}

	page, err := FetchStatic(context.Background(), origin.URL+"/open", 6000, nil)
	if err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if proxied.Load() != 0 || !strings.Contains(page.Text, "Le corps") {
		t.Errorf("proxy hits = %d, text %q; an open host must be read direct", proxied.Load(), page.Text)
	}
}

func TestPreferProxySendsTheNextFetchThroughTheProxy(t *testing.T) {
	resetProxy(t)
	origin, proxy, direct, proxied := refusingOrigin(t)
	if err := UseProxy(proxy.URL); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}
	host := strings.TrimPrefix(origin.URL, "http://")
	if ProxyPreferred(host) {
		t.Fatal("a fresh host should not be marked")
	}
	PreferProxy(host)
	if !ProxyPreferred(host) {
		t.Fatal("PreferProxy should mark the host")
	}
	if _, err := FetchStatic(context.Background(), origin.URL+"/c", 6000, nil); err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if direct.Load() != 0 || proxied.Load() != 1 {
		t.Errorf("direct %d proxied %d, want the proxy at once", direct.Load(), proxied.Load())
	}
}

func TestARefusalWithoutAProxyIsAnError(t *testing.T) {
	resetProxy(t)
	origin, _, _, _ := refusingOrigin(t)
	if _, err := FetchStatic(context.Background(), origin.URL+"/a", 6000, nil); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("err = %v, want the 403 surfaced", err)
	}
}

func TestNoProxyFetchesDirectly(t *testing.T) {
	resetProxy(t)

	var direct atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.IsAbs() {
			t.Errorf("origin got an absolute-URI request %q, want a direct one", r.URL)
		}
		direct.Add(1)
		w.Write([]byte(`<html><body><article><p>Le corps de l'article, assez long pour que readability le garde comme contenu principal de la page.</p></article></body></html>`))
	}))
	defer origin.Close()

	if err := UseProxy(""); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}

	if _, err := FetchStatic(context.Background(), origin.URL, 6000, nil); err != nil {
		t.Fatalf("FetchStatic: %v", err)
	}
	if direct.Load() != 1 {
		t.Errorf("direct hits = %d, want 1", direct.Load())
	}
}

// A typo in a deployment's configuration must be loud at boot rather than
// silently fetching direct and being refused by every publisher.
func TestUseProxyRefusesAnUnparseableURL(t *testing.T) {
	resetProxy(t)

	if err := UseProxy("://nope"); err == nil {
		t.Fatal("want an error for an unparseable proxy URL")
	}
	if got := ProxyState(); got != "unset" {
		t.Errorf("ProxyState = %q, want it left unset after a refused value", got)
	}
}

func TestProxyStateNeverLeaksCredentials(t *testing.T) {
	resetProxy(t)

	const secret = "sup3r-s3cret-passw0rd"
	if err := UseProxy("http://spuser:" + secret + "@gate.decodo.com:7000"); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}

	got := ProxyState()
	if strings.Contains(got, secret) || strings.Contains(got, "spuser") {
		t.Fatalf("ProxyState leaked credentials: %q", got)
	}
	if want := "set(http://gate.decodo.com)"; got != want {
		t.Errorf("ProxyState = %q, want %q", got, want)
	}
}

func TestRedactProxySecrets(t *testing.T) {
	resetProxy(t)

	const secret = "sup3r-s3cret-passw0rd"
	if err := UseProxy("http://spuser:" + secret + "@gate.decodo.com:7000"); err != nil {
		t.Fatalf("UseProxy: %v", err)
	}

	msg := "unable to connect to proxy http://spuser:" + secret + "@gate.decodo.com:7000 (407)"
	got := RedactProxySecrets(msg)
	if strings.Contains(got, secret) {
		t.Fatalf("password survived redaction: %q", got)
	}
	if !strings.Contains(got, "gate.decodo.com") {
		t.Errorf("host should survive so the message stays diagnosable: %q", got)
	}
}

func TestRedactProxySecretsLeavesCleanTextAlone(t *testing.T) {
	resetProxy(t)

	const clean = "fetch page: status 403"
	if got := RedactProxySecrets(clean); got != clean {
		t.Errorf("got %q, want %q", got, clean)
	}
}
