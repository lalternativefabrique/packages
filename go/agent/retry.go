package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	// nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used — jitter only, not crypto
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// RateLimitError is returned by retryTransport when the upstream provider
// answers with HTTP 429. The model-level decorator unwraps it to decide how
// long to back off.
type RateLimitError struct {
	RetryAfter time.Duration
	Body       string
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limit exceeded (retry after %s): %s", e.RetryAfter, e.Body)
	}
	return fmt.Sprintf("rate limit exceeded: %s", e.Body)
}

// retryTransport intercepts 429 responses and converts them into a typed
// *RateLimitError. All other responses pass through unchanged.
type retryTransport struct {
	base http.RoundTripper
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		return resp, nil
	}

	var bodyBuf bytes.Buffer
	if resp.Body != nil {
		_, _ = io.Copy(&bodyBuf, resp.Body)
		_ = resp.Body.Close()
	}
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
	return nil, &RateLimitError{
		RetryAfter: retryAfter,
		Body:       bodyBuf.String(),
	}
}

// parseRetryAfter parses an HTTP Retry-After header per RFC 7231: either an
// integer number of seconds or an HTTP-date. Returns 0 on missing/invalid.
func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

const (
	defaultMaxRetries = 5
	defaultMaxBackoff = 60 * time.Second
)

// retryingChatModel wraps a ToolCallingChatModel and retries Generate / Stream
// calls that fail with a *RateLimitError. The retry honors Retry-After when
// present and falls back to exponential backoff with jitter otherwise.
type retryingChatModel struct {
	inner      model.ToolCallingChatModel
	maxRetries int
	maxBackoff time.Duration
}

func wrapWithRetry(m model.ToolCallingChatModel) model.ToolCallingChatModel {
	return &retryingChatModel{
		inner:      m,
		maxRetries: defaultMaxRetries,
		maxBackoff: defaultMaxBackoff,
	}
}

func (r *retryingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		out, err := r.inner.Generate(ctx, input, opts...)
		if err == nil {
			return out, nil
		}
		var rl *RateLimitError
		if !errors.As(err, &rl) {
			return nil, err
		}
		lastErr = err
		if attempt == r.maxRetries {
			break
		}
		if err := sleepBackoff(ctx, attempt, rl, r.maxBackoff); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (r *retryingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		out, err := r.inner.Stream(ctx, input, opts...)
		if err == nil {
			return out, nil
		}
		var rl *RateLimitError
		if !errors.As(err, &rl) {
			return nil, err
		}
		lastErr = err
		if attempt == r.maxRetries {
			break
		}
		if err := sleepBackoff(ctx, attempt, rl, r.maxBackoff); err != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

func (r *retryingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := r.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &retryingChatModel{inner: bound, maxRetries: r.maxRetries, maxBackoff: r.maxBackoff}, nil
}

func sleepBackoff(ctx context.Context, attempt int, rl *RateLimitError, maxBackoff time.Duration) error {
	backoff := rl.RetryAfter
	if backoff <= 0 {
		backoff = time.Duration(1<<attempt) * time.Second
	}
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	// Add jitter 100-500ms to desynchronise concurrent retries.
	backoff += 100*time.Millisecond + time.Duration(rand.Int64N(int64(400*time.Millisecond)))
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(backoff):
		return nil
	}
}
