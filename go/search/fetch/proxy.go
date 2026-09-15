package fetch

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// UseProxy makes proxyURL, a "scheme://user:pass@host:port" residential
// endpoint, the fallback exit for page fetches.
//
// Publishers behind bot management refuse a datacenter address whatever
// headers it carries — a browser User-Agent from a cloud IP is refused
// exactly like an honest one — so the exit IP is the only thing that decides
// whether such a page can be read at all. Most of the web is not like that,
// and a residential exit is slow and metered, so a fetch goes direct first
// and through the proxy only once its host has refused the direct egress;
// the refusal is remembered per host for proxyMemory.
//
// An empty proxyURL clears it. An unparseable one is refused, so a typo in a
// deployment's configuration is loud at boot rather than silently direct.
func UseProxy(proxyURL string) error {
	raw := strings.TrimSpace(proxyURL)
	refused.reset()
	if raw == "" {
		proxy.set(nil)
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return &InvalidProxyError{}
	}
	proxy.set(parsed)
	return nil
}

// InvalidProxyError reports a proxy URL that could not be parsed. It carries
// no detail: the value it came from holds credentials.
type InvalidProxyError struct{}

func (*InvalidProxyError) Error() string {
	return "proxy URL is not parseable"
}

// ProxyState reports whether a proxy is configured, as scheme://host with no
// port and no credentials — enough to tell "unset" from "set but ineffective"
// in a log that must never carry the secret.
func ProxyState() string {
	u := proxy.get()
	if u == nil {
		return "unset"
	}
	return "set(" + u.Scheme + "://" + u.Hostname() + ")"
}

// RedactProxySecrets strips userinfo credentials out of text on its way to a
// log. An upstream error routinely quotes the proxy URL it failed to reach,
// credentials included.
func RedactProxySecrets(text string) string {
	if text == "" {
		return ""
	}
	redacted := userinfoPattern.ReplaceAllString(text, "${1}***:***@")

	if u := proxy.get(); u != nil && u.User != nil {
		if pass, ok := u.User.Password(); ok && len(pass) >= 6 {
			redacted = strings.ReplaceAll(redacted, pass, "***")
		}
		if name := u.User.Username(); len(name) >= 6 {
			redacted = strings.ReplaceAll(redacted, name, "***")
		}
	}
	return redacted
}

// userinfoPattern matches the "user:pass@" segment of a URL. The password is
// deliberately permissive (any non-@, non-whitespace run) because proxy
// passwords routinely contain punctuation.
var userinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://)[^:/@\s]+:[^@\s]+@`)

// proxy holds the configured endpoint. It is read on every fetch and written
// once at startup, so the mutex costs nothing and rules out the race a plain
// package variable would leave.
var proxy proxyConfig

type proxyConfig struct {
	mu  sync.RWMutex
	url *url.URL
}

func (p *proxyConfig) set(u *url.URL) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.url = u
}

func (p *proxyConfig) get() *url.URL {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.url
}

// httpClient returns the client one fetch runs on: direct, or through the
// proxy when viaProxy asks for it and one is configured.
func httpClient(timeout time.Duration, viaProxy bool) *http.Client {
	client := &http.Client{Timeout: timeout}
	if u := proxy.get(); u != nil && viaProxy {
		client.Transport = &http.Transport{Proxy: http.ProxyURL(u)}
	}
	return client
}
