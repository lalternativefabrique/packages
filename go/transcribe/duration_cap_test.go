package transcribe

import (
	"context"
	"errors"
	"testing"
)

type countingProvider struct {
	calls int
}

func (p *countingProvider) MaxFileSize() int64       { return 1 << 30 }
func (p *countingProvider) NeedsWAVConversion() bool { return false }

func (p *countingProvider) Transcribe(context.Context, TranscribeRequest) (*TranscribeResult, error) {
	p.calls++
	return &TranscribeResult{Text: "hi"}, nil
}

func TestTranscribe_OverCap_RejectsBeforePayingForSTT(t *testing.T) {
	prov := &countingProvider{}

	_, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 10)}, Options{
		FFprobePath:        fakeFFprobe(t, 9000),
		MaxDurationSeconds: 7200,
	})
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("error = %v, want ErrTooLong", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d time(s), want 0", prov.calls)
	}
}

func TestTranscribe_ExactlyAtCap_Transcribes(t *testing.T) {
	prov := &countingProvider{}

	if _, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 10)}, Options{
		FFprobePath:           fakeFFprobe(t, 7200),
		MaxSingleShotDuration: 99999,
		MaxDurationSeconds:    7200,
	}); err != nil {
		t.Fatalf("unexpected error at exactly the cap: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
}

func TestTranscribe_NoCap_TranscribesAnyLength(t *testing.T) {
	prov := &countingProvider{}

	if _, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 10)}, Options{
		FFprobePath:           fakeFFprobe(t, 36000),
		MaxSingleShotDuration: 99999,
	}); err != nil {
		t.Fatalf("unexpected error with no cap set: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
}

func TestTranscribe_UnknownDuration_FallsThrough(t *testing.T) {
	prov := &countingProvider{}

	if _, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 10)}, Options{
		FFprobePath:        "/nonexistent/ffprobe",
		MaxDurationSeconds: 7200,
	}); err != nil {
		t.Fatalf("unexpected error when duration is unknown: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
}
