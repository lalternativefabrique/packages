package botguard

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientAddr trusts X-Forwarded-For only when the socket peer is a trusted
// proxy, and then walks it from the right past every trusted hop, so a client
// cannot pick its own address by sending the header.
func clientAddr(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := socketAddr(r.RemoteAddr)
	if !isTrusted(peer, trusted) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer
		}
		a = a.Unmap()
		if !isTrusted(a, trusted) {
			return a
		}
		peer = a
	}
	return peer
}

func socketAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// network groups the addresses one household or one host usually holds:
// a /24 for IPv4, a /64 for IPv6.
func network(a netip.Addr) string {
	if !a.IsValid() {
		return "unknown"
	}
	bits := 64
	if a.Is4() {
		bits = 24
	}
	p, _ := a.Prefix(bits)
	return p.String()
}

// AddrFrom adapts a resolver returning the address as text, such as Echo's
// IPExtractor, to Config.ClientAddr.
func AddrFrom(extract func(*http.Request) string) func(*http.Request) netip.Addr {
	return func(r *http.Request) netip.Addr {
		a, err := netip.ParseAddr(extract(r))
		if err != nil {
			return netip.Addr{}
		}
		return a.Unmap()
	}
}
