package transcribe

import (
	"context"
	"errors"
	"testing"
)

func TestTranscribe_UnmeasurableAndOversized_RejectsBeforeSTT(t *testing.T) {
	prov := &countingProvider{}

	_, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 4096)}, Options{
		FFprobePath:              "/nonexistent/ffprobe",
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024,
	})
	if !errors.Is(err, ErrUnmeasurable) {
		t.Fatalf("error = %v, want ErrUnmeasurable", err)
	}
	if errors.Is(err, ErrTooLong) {
		t.Error("reported as ErrTooLong; nothing was measured")
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d time(s), want 0", prov.calls)
	}
}

func TestTranscribe_UnmeasurableButSmall_StillTranscribes(t *testing.T) {
	prov := &countingProvider{}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 512)}, Options{
		FFprobePath:              "/nonexistent/ffprobe",
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
	if res.ProbeError == nil {
		t.Error("ProbeError = nil, want the probe failure surfaced")
	}
}

func TestTranscribe_MeasurableAndOversized_IgnoresSizeBound(t *testing.T) {
	prov := &countingProvider{}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 4096)}, Options{
		FFprobePath:              fakeFFprobe(t, 60),
		MaxSingleShotDuration:    99999,
		MaxDurationSeconds:       7200,
		MaxBytesWhenUnmeasurable: 1024,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
	if res.ProbeError != nil {
		t.Errorf("ProbeError = %v, want nil when the duration was measured", res.ProbeError)
	}
}

func TestTranscribe_NoSizeBound_AcceptsUnmeasurable(t *testing.T) {
	prov := &countingProvider{}

	if _, err := New(prov).Transcribe(context.Background(), FileSource{Path: writeTempAudio(t, 1<<20)}, Options{
		FFprobePath:        "/nonexistent/ffprobe",
		MaxDurationSeconds: 7200,
	}); err != nil {
		t.Fatalf("unexpected error with no size bound set: %v", err)
	}
	if prov.calls == 0 {
		t.Error("provider never called")
	}
}
