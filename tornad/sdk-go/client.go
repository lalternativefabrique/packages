// Package sdk is the Go client for the tornad API.
//
// It exists because every consumer was writing this by hand. packages/go/search
// carried a tornade client covering /search alone; lalter's cortex built its
// own /fetch request beside it. Each re-derived the same auth header, the same
// status handling and the same request shape, from reading tornad's source —
// and neither ever learned that /map and /crawl exist.
//
// # Where the transport comes from
//
// internal/wire is generated from openapi/tornad.json, tornad's own contract.
// It owns every path, method and parameter, so none of them is typed by hand
// here: a route renamed upstream becomes a compile error rather than a 404 in
// production, and a field added or renamed surfaces the same way instead of as
// a value silently never read.
//
// It is internal because it is not this package's API. Its methods return raw
// *http.Response and generated pointer types; what is exported wraps them with
// typed errors and the pointer-to-value conversions that keep "this page could
// not be read" distinguishable from "tornad is down".
//
// Updating after an API change is ./refresh-contract.sh, which fetches
// /openapi.json from a running tornad and regenerates — then reconcile any
// compile error the new shape causes.
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

	"github.com/lalternative/packages/tornad/sdk-go/internal/wire"
)

// DefaultTimeout bounds a single call.
//
// Generous because these are not fast operations: a merged search waits on
// academic engines that answer in 6-8s, and a render loads a page in a real
// browser. Short enough that a hung backend degrades instead of hanging.
const DefaultTimeout = 30 * time.Second

// Client talks to a tornad deployment on behalf of ONE application.
//
// The key identifies the calling application, never a user. A deployment
// reachable only from inside the cluster may run with no key at all, which is
// why an empty one is not an error here: tornad decides whether to refuse.
type Client struct {
	baseURL string
	key     string
	http    *http.Client
	wire    *wire.ClientWithResponses
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client, for a caller that pools connections
// or instruments its transport.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithTimeout bounds a single call, replacing DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http.Timeout = d }
}

// New returns a Client. An empty baseURL yields one whose every method returns
// ErrNotConfigured, so a deployment with no tornad reachable can hold a client
// and branch on the error rather than on a nil pointer.
func New(baseURL, key string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	c.wire = newWire(c.baseURL, c.key, c.http)
	return c
}

// newWire builds the generated transport. Its error is dropped on purpose: it
// can only come from a ClientOption, and none is passed here — surfacing it
// would force New to return an error for a case that cannot arise, when a blank
// baseURL is already handled by every method returning ErrNotConfigured.
func newWire(baseURL, key string, doer wire.HttpRequestDoer) *wire.ClientWithResponses {
	if baseURL == "" {
		return nil
	}
	auth := wire.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
		if key != "" {
			req.Header.Set("X-Tornad-Key", key)
		}
		return nil
	})
	w, err := wire.NewClientWithResponses(baseURL+"/", wire.WithHTTPClient(doer), auth)
	if err != nil {
		return nil
	}
	return w
}

// Errors the caller is expected to branch on.
var (
	// ErrNotConfigured — no base URL. The call was never made, and a caller
	// with a static fallback should use it.
	ErrNotConfigured = errors.New("tornad: not configured")
	// ErrUnauthorized — the key was rejected (401/403). Operator error.
	ErrUnauthorized = errors.New("tornad: unauthorized")
	// ErrBadRequest — tornad refused the arguments (400). A bug in the caller.
	ErrBadRequest = errors.New("tornad: bad request")
	// ErrNotFound — no such crawl (404).
	ErrNotFound = errors.New("tornad: not found")
	// ErrUpstream — tornad reached the open web and did not get a page (502).
	//
	// It has its own sentinel because it is ROUTINE, not an outage: a publisher
	// refusing a datacenter address, a page that never settles, a site that is
	// down. Folded into ErrUnavailable it reads as "tornad is broken", so a
	// caller retries a URL that will never load instead of moving on.
	ErrUpstream = errors.New("tornad: upstream page could not be read")
	// ErrUnavailable — transport failure, a 5xx other than 502, or a backend
	// this deployment does not have configured (503). Transient or structural;
	// either way, degrading is safer than failing the caller's own request.
	ErrUnavailable = errors.New("tornad: unavailable")
)

// statusError maps a response status onto the sentinels above.
func statusError(code int, body io.Reader) error {
	switch {
	case code == http.StatusBadRequest:
		return fmt.Errorf("%w: %s", ErrBadRequest, snippet(body))
	case code == http.StatusUnauthorized, code == http.StatusForbidden:
		return ErrUnauthorized
	case code == http.StatusNotFound:
		return ErrNotFound
	case code == http.StatusBadGateway:
		return fmt.Errorf("%w: %s", ErrUpstream, snippet(body))
	case code >= 500:
		return fmt.Errorf("%w: %s", ErrUnavailable, snippet(body))
	case code >= 400:
		return fmt.Errorf("%w: unexpected status %d: %s", ErrUnavailable, code, snippet(body))
	}
	return nil
}

// snippet reads the head of an error body, so a failure says what tornad
// reported rather than only which status it used.
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

func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

func ptr[T any](v T) *T { return &v }
