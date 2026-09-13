package fileguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxFetchRedirects bounds redirect chains. Each hop is dialed through the same
// guarded transport, so this caps effort rather than exposure.
const maxFetchRedirects = 5

// ValidateFetchURL rejects a caller-supplied URL whose scheme is not http(s) or
// whose host is, or resolves to, an address a server must never be made to
// reach.
//
// It is a pre-check, and on its own it does not close DNS rebinding: the answer
// can change between this lookup and the connection. Pair it with
// SafeHTTPClient, which dials only the address it validated.
//
// Use it ALONE — and know what you are getting — when the fetch leaves Go: a
// request through an HTTP proxy connects to the proxy, not to the target, so
// the transport guard inspects the wrong endpoint; a headless browser resolves
// names itself and never sees this package at all. In both cases this function
// bounds what a caller may ask for, not what the fetcher reaches, and the rest
// has to come from the network layer.
func ValidateFetchURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}

	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("could not resolve host")
		}
		ips = resolved
	}

	for _, ip := range ips {
		if IsBlockedIP(ip) {
			return fmt.Errorf("url resolves to a disallowed address")
		}
	}
	return nil
}

// IsBlockedIP reports whether an address is one a caller must never be able to
// make the server reach: loopback, link-local (including the cloud metadata
// endpoint), multicast, unspecified, and the RFC1918 ranges.
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	// Explicit for readers: the cloud metadata endpoint, already covered by
	// IsLinkLocalUnicast.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	// 0.0.0.0/8 is "this network": not caught by IsUnspecified, which only
	// matches the bare 0.0.0.0, yet routed to the local host by some stacks.
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 0 {
		return true
	}
	return false
}

// SafeHTTPClient returns a client that resolves each host itself, refuses every
// address it must not reach, and then connects to the very address it checked.
//
// Dialing the validated IP rather than re-resolving the name is what closes DNS
// rebinding: a second lookup could answer differently, and a check performed on
// the first answer would then guard a connection made to the second. Redirects
// go through the same transport, so a 302 to an internal host fails at dial
// time even though the original URL was public.
//
// A proxy defeats this by design — see ValidateFetchURL.
func SafeHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: checkRedirect,
		Transport:     SafeTransport(),
	}
}

// SafeTransport is SafeHTTPClient's transport, for a caller that needs its own
// client settings but the same dialing guarantee.
func SafeTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				if IsBlockedIP(ip) {
					return nil, fmt.Errorf("connection to disallowed address %s", host)
				}
				return dialer.DialContext(ctx, network, addr)
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("dns: %w", err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no ip for %s", host)
			}
			for _, ip := range ips {
				if IsBlockedIP(ip) {
					return nil, fmt.Errorf("connection to disallowed address %s", ip)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxFetchRedirects {
		return fmt.Errorf("too many redirects")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("redirect to disallowed scheme %q", req.URL.Scheme)
	}
	return nil
}
