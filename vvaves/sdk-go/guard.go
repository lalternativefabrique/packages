package sdk

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ErrInsecureBaseURL is a base URL that would send the application's key in
// clear text across a network the cluster does not own.
var ErrInsecureBaseURL = errors.New("insecure base url: plain http is only accepted for an in-cluster or loopback host")

// ErrResponseTooLarge is a response past the client's byte ceiling. The read
// stops there instead of buffering whatever the server keeps sending.
var ErrResponseTooLarge = errors.New("response exceeds the client's size limit")

// WithMaxResponseBytes replaces the response ceiling. Zero or less keeps the
// default; there is no way to remove the ceiling.
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithInsecureHTTP accepts a plain-http base URL on any host. For a test
// against a server on a routable address; never for production.
func WithInsecureHTTP() Option {
	return func(c *Client) { c.insecureHTTP = true }
}

func checkBaseURL(raw string, allowInsecure bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%w: %q is not an absolute url", ErrInsecureBaseURL, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecure || inClusterHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrInsecureBaseURL, u.Host)
	}
	return fmt.Errorf("%w: scheme %q", ErrInsecureBaseURL, u.Scheme)
}

func inClusterHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".localhost", ".internal", ".local", ".svc", ".cluster.local"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

type boundedDoer struct {
	next *http.Client
	max  int64
}

func (d boundedDoer) Do(req *http.Request) (*http.Response, error) {
	resp, err := d.next.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > d.max {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: %d bytes announced, limit %d", ErrResponseTooLarge, resp.ContentLength, d.max)
	}
	resp.Body = &boundedBody{rc: resp.Body, left: d.max}
	return resp, nil
}

type boundedBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		var probe [1]byte
		n, err := b.rc.Read(probe[:])
		if n > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *boundedBody) Close() error { return b.rc.Close() }
