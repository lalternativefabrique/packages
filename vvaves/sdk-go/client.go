// Package sdk is the Go client for the vvaves API.
//
// internal/wire is generated from openapi/vvaves.json, vvaves's own contract,
// so no path or field is typed by hand here. Updating after an API change is
// ./refresh-contract.sh, then reconciling any compile error.
package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/vvaves/sdk-go/internal/wire"
)

// DefaultTimeout bounds a single call. A whole long reading synthesised on a
// miss takes minutes; a context is how a caller asks for less.
const DefaultTimeout = 5 * time.Minute

// DefaultMaxResponseBytes bounds one response, which is at most one whole
// reading's audio.
const DefaultMaxResponseBytes = 64 << 20

// HeaderKey carries an application key; CustomerKeyPrefix marks a key
// urbangate issued, which travels as a bearer token instead.
const (
	HeaderKey         = "X-Vvaves-Key"
	CustomerKeyPrefix = "vvaves_key_"
)

// Client talks to a vvaves deployment on behalf of ONE application.
type Client struct {
	baseURL      string
	key          string
	scope        string
	authorize    func(*http.Request) error
	signing      signing
	http         *http.Client
	maxBytes     int64
	insecureHTTP bool
	err          error
	doer         boundedDoer
	wire         *wire.ClientWithResponses
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithTimeout bounds a single call, replacing DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// WithScope names where vvaves keeps this application's readings, so two
// applications sharing one vvaves do not overwrite each other's cache.
func WithScope(scope string) Option {
	return func(c *Client) { c.scope = scope }
}

// WithAuthorize attaches a credential per call, typically a bearer token from
// the suite's identity provider. Set, it replaces the key.
func WithAuthorize(fn func(*http.Request) error) Option {
	return func(c *Client) { c.authorize = fn }
}

// New returns a Client. An empty baseURL yields one whose every method returns
// ErrNotConfigured, and a plain-http one off the cluster ErrInsecureBaseURL.
func New(baseURL, key string, opts ...Option) *Client {
	c := &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		key:      key,
		http:     &http.Client{Timeout: DefaultTimeout},
		maxBytes: DefaultMaxResponseBytes,
	}
	for _, o := range opts {
		o(c)
	}
	switch {
	case c.baseURL == "":
		c.err = ErrNotConfigured
	default:
		c.err = checkBaseURL(c.baseURL, c.insecureHTTP)
	}
	if c.err == nil && c.signing.publicURL != "" {
		c.err = checkBaseURL(c.signing.publicURL, c.insecureHTTP)
	}
	if c.err == nil {
		c.doer = boundedDoer{next: c.http, max: c.maxBytes}
		c.wire = newWire(c.baseURL, c.authEditor(), c.doer)
		if c.wire == nil {
			c.err = ErrNotConfigured
		}
	}
	return c
}

func (c *Client) ready() error { return c.err }

func (c *Client) authEditor() wire.RequestEditorFn {
	return func(_ context.Context, req *http.Request) error {
		switch {
		case c.authorize != nil:
			return c.authorize(req)
		case strings.HasPrefix(c.key, CustomerKeyPrefix):
			req.Header.Set("Authorization", "Bearer "+c.key)
		case c.key != "":
			req.Header.Set(HeaderKey, c.key)
		}
		return nil
	}
}

func newWire(baseURL string, auth wire.RequestEditorFn, doer wire.HttpRequestDoer) *wire.ClientWithResponses {
	w, err := wire.NewClientWithResponses(baseURL+"/", wire.WithHTTPClient(doer), wire.WithRequestEditorFn(auth))
	if err != nil {
		return nil
	}
	return w
}

// Errors the caller is expected to branch on.
var (
	ErrNotConfigured = errors.New("vvaves: not configured")
	// ErrUnauthorized — the key or the signature was rejected (401/403).
	ErrUnauthorized = errors.New("vvaves: unauthorized")
	// ErrBadRequest — vvaves refused the arguments (400/422). A caller bug.
	ErrBadRequest = errors.New("vvaves: bad request")
	// ErrUnavailable — transport failure or 5xx: degrade to no audio.
	ErrUnavailable = errors.New("vvaves: unavailable")
	// ErrNoAudio — vvaves answered but with no audio to play.
	ErrNoAudio = errors.New("vvaves: no audio")
)

func statusError(code int, body io.Reader) error {
	switch {
	case code == http.StatusBadRequest, code == http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrBadRequest, snippet(body))
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return ErrUnauthorized
	case code >= 500:
		return fmt.Errorf("%w: %s", ErrUnavailable, snippet(body))
	case code >= 400:
		return fmt.Errorf("%w: unexpected status %d: %s", ErrUnavailable, code, snippet(body))
	}
	return nil
}

func statusFrom(code int, body []byte) error {
	if err := statusError(code, strings.NewReader(string(body))); err != nil {
		return err
	}
	return fmt.Errorf("%w: unexpected status %d", ErrUnavailable, code)
}

func snippet(r io.Reader) string {
	if r == nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil {
		return ""
	}
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(b))
}

func wrap(err error) error {
	return fmt.Errorf("%w: %w", ErrUnavailable, err)
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func ptr[T any](v T) *T { return &v }
