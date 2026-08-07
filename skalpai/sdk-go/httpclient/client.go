// Package httpclient wires outgoing HTTP calls into the current trace.
package httpclient

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Wrap returns client with a tracing transport, so each outgoing call emits a
// span and carries traceparent to the receiving service. Without it a slow
// upstream shows up as unexplained time inside its caller, and the remote
// service roots a separate trace.
//
// The caller's transport and timeout are preserved: they carry TLS, proxy and
// deadline settings the call site depends on.
func Wrap(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}

	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}

	wrapped := *client
	wrapped.Transport = otelhttp.NewTransport(base)
	return &wrapped
}
