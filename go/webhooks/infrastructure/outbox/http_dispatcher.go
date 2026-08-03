package outbox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lalternative/packages/go/webhooks/domain/providers"
)

// DefaultBrand names the headers when a caller does not pick one. It is
// deliberately neutral: a product that forgets to set Brand ships working
// webhooks under a generic name rather than under another product's.
const DefaultBrand = "Webhook"

// HTTPDispatcher posts a single delivery job to the subscriber URL with an
// HMAC-SHA256 signature derived from the timestamp + raw body.
//
// The signature scheme is fixed across every product using this package —
// HMAC-SHA256 over "<timestamp>.<body>", sent as "v1=<hex>". Only the header
// NAMES carry the brand, so there is exactly one signing implementation to
// audit no matter how many products dispatch webhooks.
type HTTPDispatcher struct {
	client *http.Client
	brand  string
}

// NewHTTPDispatcher builds a dispatcher whose headers are named after brand
// (e.g. "Spore" yields Spore-Signature). An empty brand falls back to
// DefaultBrand.
func NewHTTPDispatcher(timeout time.Duration, brand string) *HTTPDispatcher {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if brand == "" {
		brand = DefaultBrand
	}
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("dns: %w", err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no ip for %s", host)
			}
			for _, ip := range ips {
				if isBlockedIP(ip) {
					return nil, fmt.Errorf("refusing to connect to private address %s", ip)
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("stopped after %d redirects", 3)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-http scheme: %s", req.URL.Scheme)
			}
			return nil
		},
	}
	return &HTTPDispatcher{client: client, brand: brand}
}

func (d *HTTPDispatcher) Dispatch(ctx context.Context, job providers.DeliveryJob) providers.HTTPResult {
	start := time.Now()
	ts := strconv.FormatInt(start.Unix(), 10)
	sig := sign(job.Secret, ts, job.Payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.URL, bytes.NewReader(job.Payload))
	if err != nil {
		return providers.HTTPResult{Err: err, DurationMS: time.Since(start).Milliseconds()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", d.brand+"-Webhooks/1.0")
	req.Header.Set(d.brand+"-Timestamp", ts)
	req.Header.Set(d.brand+"-Signature", "v1="+sig)
	req.Header.Set(d.brand+"-Event", job.EventType)
	req.Header.Set(d.brand+"-Delivery-ID", job.DeliveryID)
	req.Header.Set(d.brand+"-Source-Event-ID", job.SourceEventID)

	resp, err := d.client.Do(req)
	if err != nil {
		return providers.HTTPResult{Err: err, DurationMS: time.Since(start).Milliseconds()}
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)

	res := providers.HTTPResult{
		StatusCode: resp.StatusCode,
		DurationMS: time.Since(start).Milliseconds(),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		res.Err = fmt.Errorf("http %d", resp.StatusCode)
	}
	return res
}

// Sign computes the delivery signature: HMAC-SHA256 over "<timestamp>.<body>",
// hex-encoded, sent as "v1=<hex>" in the <Brand>-Signature header.
//
// Exported because a receiver has to reproduce it exactly, and the only
// alternative to publishing it is every consumer SDK re-deriving it from this
// file — where a subtly wrong copy rejects every delivery it is given, or
// accepts a forged one. Consumers verify against this function rather than
// against their reading of it.
func Sign(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func sign(secret, timestamp string, body []byte) string {
	return Sign(secret, timestamp, body)
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
		return true
	}
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}
	if strings.HasPrefix(ip.String(), "0.") {
		return true
	}
	return false
}
