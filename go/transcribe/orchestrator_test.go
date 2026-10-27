package transcribe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type fakeProvider struct {
	maxFileSize int64
	needsWAV    bool
	calls       int32
	responder   func(call int, req TranscribeRequest) (string, error)
}

func (p *fakeProvider) MaxFileSize() int64       { return p.maxFileSize }
func (p *fakeProvider) NeedsWAVConversion() bool { return p.needsWAV }

func (p *fakeProvider) Transcribe(_ context.Context, req TranscribeRequest) (*TranscribeResult, error) {
	n := atomic.AddInt32(&p.calls, 1)
	if p.responder != nil {
		text, err := p.responder(int(n), req)
		if err != nil {
			return nil, err
		}
		return &TranscribeResult{Text: text}, nil
	}
	return &TranscribeResult{Text: "ok"}, nil
}

func writeTempAudio(t *testing.T, size int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audio.mp3")
	if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTranscribe_SingleShot(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		return "hello world", nil
	}}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: "/nonexistent/ffprobe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Segments) != 1 || res.Segments[0].Text != "hello world" {
		t.Fatalf("got %+v", res.Segments)
	}
	if res.Segments[0].StartSeconds != 0 {
		t.Fatalf("single-shot offset must be 0, got %d", res.Segments[0].StartSeconds)
	}
}

func TestTranscribe_CarriesProbedDuration(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: fakeFFprobe(t, 95)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DurationSeconds != 95 {
		t.Fatalf("DurationSeconds = %v, want 95 (probed)", res.DurationSeconds)
	}
}

func TestTranscribe_NoProbe_DurationZero(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{FFprobePath: "/nonexistent/ffprobe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.DurationSeconds != 0 {
		t.Fatalf("DurationSeconds = %v, want 0 when probe unavailable", res.DurationSeconds)
	}
}

func TestTranscribe_RetryThenSuccess(t *testing.T) {
	path := writeTempAudio(t, 10)
	var attempts int32
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			return "", errors.New("transient")
		}
		return "recovered", nil
	}}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 1, ChunkDuration: 300, FFmpegPath: fakeFFmpeg(t, 1), FFprobePath: fakeFFprobe(t, 1200),
	})
	if err != nil {
		t.Fatalf("expected recovery, got error: %v", err)
	}
	if res.Segments[0].Text != "recovered" {
		t.Fatalf("got %q", res.Segments[0].Text)
	}
	if attempts < 3 {
		t.Fatalf("expected >=3 attempts, got %d", attempts)
	}
}

// 429 widens the budget from 3 to 5 attempts, so four rate limits in a row
// still end in the successful fifth call.
func TestTranscribe_RateLimitWidensBudget(t *testing.T) {
	path := writeTempAudio(t, 10)
	var attempts int32
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, _ TranscribeRequest) (string, error) {
		if atomic.AddInt32(&attempts, 1) <= 4 {
			return "", &RateLimitError{StatusCode: 429, RetryAfter: time.Millisecond}
		}
		return "after backoff", nil
	}}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 1, ChunkDuration: 300, FFmpegPath: fakeFFmpeg(t, 1), FFprobePath: fakeFFprobe(t, 1200),
	})
	if err != nil {
		t.Fatalf("expected success within widened budget, got: %v", err)
	}
	if res.Segments[0].Text != "after backoff" {
		t.Fatalf("got %q", res.Segments[0].Text)
	}
}

func TestTranscribe_ProviderTimingIsReoffsetPerChunk(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &timedProvider{}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 2, ChunkDuration: 300, FFmpegPath: fakeFFmpeg(t, 2), FFprobePath: fakeFFprobe(t, 600),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(res.Segments))
	}
	want := []float64{0, 10, 300, 310}
	for i, s := range res.Segments {
		if !s.HasTiming || s.Start != want[i] {
			t.Errorf("segment %d: Start=%v HasTiming=%v, want Start=%v HasTiming=true", i, s.Start, s.HasTiming, want[i])
		}
	}
	if res.Language != "fr" {
		t.Errorf("Language = %q, want fr", res.Language)
	}
}

type timedProvider struct{}

func (timedProvider) MaxFileSize() int64       { return 1 << 30 }
func (timedProvider) NeedsWAVConversion() bool { return false }
func (timedProvider) Transcribe(context.Context, TranscribeRequest) (*TranscribeResult, error) {
	return &TranscribeResult{
		Text:     "a b",
		Language: "fr",
		Segments: []ProviderSegment{{Text: "a", Start: 0, End: 10}, {Text: "b", Start: 10, End: 20}},
	}, nil
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"empty", "", 0},
		{"delta seconds", "30", 30 * time.Second},
		{"delta zero", "0", 0},
		{"negative delta", "-5", 0},
		{"http date future", now.Add(45 * time.Second).UTC().Format(http.TimeFormat), 45 * time.Second},
		{"http date past", now.Add(-45 * time.Second).UTC().Format(http.TimeFormat), 0},
		{"garbage", "not-a-number", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRetryAfter(tc.header, now); got != tc.want {
				t.Errorf("ParseRetryAfter(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

func TestRateLimitError_ErrorsAs(t *testing.T) {
	base := &RateLimitError{RetryAfter: 12 * time.Second, StatusCode: 429}
	wrapped := fmt.Errorf("groq: %w", fmt.Errorf("whisper: %w", base))

	var rle *RateLimitError
	if !errors.As(wrapped, &rle) {
		t.Fatal("errors.As failed to unwrap RateLimitError through wrapping")
	}
	if rle.RetryAfter != 12*time.Second || rle.StatusCode != 429 {
		t.Errorf("got %+v", rle)
	}
}

func TestJoinText(t *testing.T) {
	if got := JoinText([]Segment{{Text: "a"}, {Text: "b"}, {Text: "c"}}); got != "a b c" {
		t.Fatalf("got %q", got)
	}
}
