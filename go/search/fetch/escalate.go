package fetch

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// proxyMemory is how long a host that refused the direct egress is sent
// through the proxy without another direct try. A publisher's bot policy
// does not change by the minute, and a wasted direct try costs a second.
const proxyMemory = time.Hour

// PreferProxy records that host refuses this deployment's own egress, so
// its next fetches and renders go through the proxy at once. Callers use it
// when a page came back as a bot-management interstitial, which this
// package cannot tell from an article; a refusal by status is recorded
// here without their help.
func PreferProxy(host string) {
	if proxy.get() == nil || host == "" {
		return
	}
	refused.mark(strings.ToLower(host))
}

// ProxyPreferred reports whether host is currently sent through the proxy.
// A renderer asks it to pick the browser that matches the fetch.
func ProxyPreferred(host string) bool {
	return proxy.get() != nil && refused.marked(strings.ToLower(host))
}

var refused = refusedHosts{until: map[string]time.Time{}}

type refusedHosts struct {
	mu    sync.Mutex
	until map[string]time.Time
}

func (r *refusedHosts) mark(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.until[host] = time.Now().Add(proxyMemory)
}

func (r *refusedHosts) marked(host string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	until, ok := r.until[host]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(r.until, host)
		return false
	}
	return true
}

func (r *refusedHosts) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.until = map[string]time.Time{}
}

// refusedDirect reports whether a direct attempt failed in a way the proxy
// is the answer to: a status a bot policy answers with, or a connection the
// origin would not hold. A 404 or a 500 is the page's own affair and comes
// back the same through any exit.
func refusedDirect(resp *http.Response, err error) bool {
	if err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return false
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			var opErr *net.OpError
			return errors.As(urlErr.Err, &opErr)
		}
		return false
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	}
	return false
}
