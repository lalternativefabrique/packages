package sdk

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/lalternative/packages/vvaves/sdk-go/signed"
)

// DefaultSignTTL is how long a handed-out URL stays valid: long enough to
// press play on a reply left on screen, short enough that a link copied out
// of the network tab stops working.
const DefaultSignTTL = 30 * time.Minute

// ErrNoIssuer is a local signature asked for with no issuer configured.
var ErrNoIssuer = errors.New("vvaves: signing locally needs WithSigning's issuer")

type signing struct {
	issuer    string
	publicURL string
	ttl       time.Duration
}

// WithSigning configures the URLs handed to browsers. publicURL is the origin
// the browser reaches vvaves on, defaulting to the base URL; issuer names
// this application to vvaves and is only needed for a key vvaves stores. A
// customer key is signed by vvaves itself.
func WithSigning(issuer, publicURL string, ttl time.Duration) Option {
	return func(c *Client) {
		c.signing = signing{issuer: issuer, publicURL: strings.TrimRight(publicURL, "/"), ttl: ttl}
	}
}

// SignedURL is where a browser may fetch one reading, and until when.
type SignedURL struct {
	URL       string
	ExpiresAt time.Time
}

// Sign authorises a browser to play exactly this reading: the signature
// covers the text, so the URL cannot be spent on any other.
func (c *Client) Sign(ctx context.Context, r Reading) (SignedURL, error) {
	if err := c.ready(); err != nil {
		return SignedURL{}, err
	}
	if c.authorize == nil && strings.HasPrefix(c.key, CustomerKeyPrefix) {
		return c.signRemote(ctx, r)
	}
	if c.signing.issuer == "" {
		return SignedURL{}, ErrNoIssuer
	}
	ttl := c.signing.ttl
	if ttl <= 0 {
		ttl = DefaultSignTTL
	}
	b := c.body(r, false)
	expires := time.Now().Add(ttl)
	q, err := signed.Sign(c.signing.issuer, c.key, signed.Params{
		Scope: deref(b.Scope), ID: deref(b.Id), TextHash: signed.HashText(r.Text), Expires: expires,
	})
	if err != nil {
		return SignedURL{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	base := c.signing.publicURL
	if base == "" {
		base = c.baseURL
	}
	return SignedURL{URL: base + "/speak?" + q.Encode(), ExpiresAt: time.Unix(expires.Unix(), 0)}, nil
}

func (c *Client) signRemote(ctx context.Context, r Reading) (SignedURL, error) {
	b := c.body(r, false)
	if err := (signed.Params{Scope: deref(b.Scope), ID: deref(b.Id)}).Validate(); err != nil {
		return SignedURL{}, fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	res, err := c.wire.SignSpeakWithResponse(ctx, b)
	if err != nil {
		return SignedURL{}, wrap(err)
	}
	if res.JSON200 == nil {
		return SignedURL{}, statusFrom(res.StatusCode(), res.Body)
	}
	u := deref(res.JSON200.Url)
	if u == "" {
		return SignedURL{}, fmt.Errorf("%w: vvaves answered no url", ErrUnavailable)
	}
	if err := checkBaseURL(u, c.insecureHTTP); err != nil {
		return SignedURL{}, fmt.Errorf("%w: vvaves signed an insecure url: %w", ErrUnavailable, err)
	}
	expires, _ := time.Parse(time.RFC3339, deref(res.JSON200.ExpiresAt))
	return SignedURL{URL: u, ExpiresAt: expires}, nil
}

// PublicOrigin is where browsers fetch audio, for a CSP's connect-src.
func (c *Client) PublicOrigin() string {
	base := c.signing.publicURL
	if base == "" {
		base = c.baseURL
	}
	if u, err := url.Parse(base); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return base
}
