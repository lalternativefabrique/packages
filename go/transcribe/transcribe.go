// Package transcribe turns an audio source into timed transcript segments: it
// localizes the audio, splits long files with ffmpeg, transcribes the chunks
// in parallel against a Provider with retry and backoff, and returns the
// segments re-offset to absolute time. It performs no I/O beyond ffmpeg,
// ffprobe and the Provider.
package transcribe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TranscribeRequest is one provider call: a whole file or one chunk.
type TranscribeRequest struct {
	FileName string
	File     io.Reader
	// Language is a hint sent to the backend; "" lets it detect.
	Language string
}

// ProviderSegment is one timed span, Start/End in seconds relative to the
// start of the submitted payload.
type ProviderSegment struct {
	Text  string
	Start float64
	End   float64
}

// TranscribeResult is what a Provider returns for one payload. Segments is nil
// when the backend exposes no per-segment timing; Language is the ISO 639-1
// code the backend detected, or "".
type TranscribeResult struct {
	Text     string
	Segments []ProviderSegment
	Language string
}

// Provider is a speech-to-text backend. MaxFileSize bounds one request's
// payload in bytes; inputs above it are chunked. NeedsWAVConversion reports a
// backend that only accepts WAV PCM 16kHz mono (whisper.cpp).
type Provider interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (*TranscribeResult, error)
	MaxFileSize() int64
	NeedsWAVConversion() bool
}

// RateLimitError is returned by a Provider on HTTP 429. RetryAfter is the
// parsed Retry-After header, 0 when absent. Wrapping providers must use %w so
// the orchestrator still finds it with errors.As.
type RateLimitError struct {
	RetryAfter time.Duration
	StatusCode int
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limited: status=%d retry_after=%s", e.StatusCode, e.RetryAfter)
}

// ParseRetryAfter reads a Retry-After header, delta-seconds or HTTP-date,
// relative to now. It returns 0 when empty, unparseable or in the past.
func ParseRetryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil {
		if secs <= 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(header); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}
