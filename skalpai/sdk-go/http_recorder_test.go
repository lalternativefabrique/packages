package skalpai

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWrapHTTPHandlerExposesFlusher(t *testing.T) {
	flushed := false
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatalf("ResponseWriter does not implement http.Flusher")
		}
		_, _ = w.Write([]byte("event: ping\n\n"))
		f.Flush()
		flushed = true
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if !flushed {
		t.Fatalf("handler did not reach Flush")
	}
	if !rec.Flushed {
		t.Fatalf("Flush did not reach the underlying ResponseWriter")
	}
	if rec.Body.String() != "event: ping\n\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestWrapHTTPHandlerFlushCountsBytes(t *testing.T) {
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chunk"))
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("chunk"))
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))

	if rec.Body.String() != "chunkchunk" {
		t.Fatalf("body = %q, want chunkchunk", rec.Body.String())
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestWrapHTTPHandlerUnwrapReturnsUnderlyingWriter(t *testing.T) {
	rec := httptest.NewRecorder()

	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			t.Fatalf("ResponseWriter does not implement Unwrap")
		}
		if u.Unwrap() != http.ResponseWriter(rec) {
			t.Fatalf("Unwrap did not return the original ResponseWriter")
		}
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
}

func TestWrapHTTPHandlerHijackReportsUnsupported(t *testing.T) {
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("ResponseWriter does not implement http.Hijacker")
		}
		if _, _, err := hj.Hijack(); err == nil {
			t.Fatalf("Hijack on a non-hijackable writer should fail")
		}
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/ws", nil))
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

func TestWrapHTTPHandlerHijackDelegates(t *testing.T) {
	rec := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}

	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, _, err := w.(http.Hijacker).Hijack(); err != nil {
			t.Fatalf("Hijack returned %v", err)
		}
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))

	if !rec.hijacked {
		t.Fatalf("Hijack did not reach the underlying ResponseWriter")
	}
}

func TestWrapHTTPHandlerPushReportsUnsupported(t *testing.T) {
	h := WrapHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p, ok := w.(http.Pusher)
		if !ok {
			t.Fatalf("ResponseWriter does not implement http.Pusher")
		}
		if err := p.Push("/style.css", nil); err != http.ErrNotSupported {
			t.Fatalf("Push error = %v, want ErrNotSupported", err)
		}
	}), HTTPMiddlewareConfig{ServiceName: "test-service"})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/index.html", nil))
}
