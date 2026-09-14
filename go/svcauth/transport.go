package svcauth

import (
	"net/http"
	"time"
)

// Transport authorises every request it carries with a token from c, so a
// client built on it authenticates calls a library makes on its own behalf.
// Authorize covers the case where the caller holds the request; this one the
// case where it only holds the *http.Client.
//
// The request is cloned before the header is set: RoundTrip must not modify
// the request it is given, and a retry would otherwise append a second
// Authorization header to the caller's copy.
//
// A token the issuer refuses or cannot mint fails the request rather than
// sending it unauthenticated, which would reach the callee as an anonymous
// call and be refused there with a less telling error.
func Transport(c *ClientCredentials, base http.RoundTripper) http.RoundTripper {
	return &authTransport{creds: c, base: base}
}

type authTransport struct {
	creds *ClientCredentials
	base  http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if t.creds == nil {
		return base.RoundTrip(req)
	}
	authorized := req.Clone(req.Context())
	if err := t.creds.Authorize(authorized); err != nil {
		return nil, err
	}
	return base.RoundTrip(authorized)
}

// HTTPClient returns a client that authorises every request with c, for the
// constructors that take one rather than a per-request hook. It is the whole
// wiring a caller needs: hand it where an *http.Client is expected.
//
// The client keeps c.Client's timeout when one is set, so the timeout chosen
// for reaching the token endpoint also bounds the calls made with the token.
func (c *ClientCredentials) HTTPClient() *http.Client {
	var base http.RoundTripper
	var timeout time.Duration
	if c.Client != nil {
		base = c.Client.Transport
		timeout = c.Client.Timeout
	}
	return &http.Client{Transport: Transport(c, base), Timeout: timeout}
}
