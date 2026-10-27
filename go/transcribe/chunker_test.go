package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeFFmpeg stands in for `ffmpeg -f segment ... chunk_%03d.ext`: it writes
// nChunks non-empty files matching the output pattern, which is the last arg.
func fakeFFmpeg(t *testing.T, nChunks int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg script is POSIX sh")
	}
	script := filepath.Join(t.TempDir(), "fakeffmpeg.sh")
	body := `#!/bin/sh
pattern=""
for a in "$@"; do pattern="$a"; done
outdir=$(dirname "$pattern")
ext=$(echo "$pattern" | sed 's/.*chunk_%03d//')
i=0
while [ "$i" -lt ` + fmt.Sprintf("%d", nChunks) + ` ]; do
  n=$(printf '%03d' "$i")
  echo "audio$i" > "$outdir/chunk_$n$ext"
  i=$((i+1))
done
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func fakeFFprobe(t *testing.T, seconds int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffprobe script is POSIX sh")
	}
	script := filepath.Join(t.TempDir(), "fakeffprobe.sh")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\necho %d\n", seconds)), 0o700); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestTranscribe_Chunked_AllSucceed(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, req TranscribeRequest) (string, error) {
		return "T:" + req.FileName, nil
	}}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel:   3,
		ChunkDuration: 300,
		FFmpegPath:    fakeFFmpeg(t, 4),
		FFprobePath:   fakeFFprobe(t, 1200),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(res.Segments))
	}
	for i, s := range res.Segments {
		if s.StartSeconds != i*300 {
			t.Fatalf("segment %d offset = %d, want %d", i, s.StartSeconds, i*300)
		}
		if !strings.HasPrefix(s.Text, "T:chunk_") {
			t.Fatalf("segment %d text = %q", i, s.Text)
		}
	}
	if len(res.FailedChunks) != 0 {
		t.Fatalf("expected no failures, got %v", res.FailedChunks)
	}
}

func TestTranscribe_Chunked_GracefulDegradation(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, req TranscribeRequest) (string, error) {
		if strings.Contains(req.FileName, "chunk_001") {
			return "", errors.New("boom")
		}
		return "T:" + req.FileName, nil
	}}

	res, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 2, ChunkDuration: 300, FFmpegPath: fakeFFmpeg(t, 4), FFprobePath: fakeFFprobe(t, 1200),
	})
	if err != nil {
		t.Fatalf("should degrade gracefully, got error: %v", err)
	}
	if len(res.FailedChunks) != 1 || res.FailedChunks[0] != 1 {
		t.Fatalf("expected FailedChunks=[1], got %v", res.FailedChunks)
	}
	if !strings.Contains(res.Segments[1].Text, "unavailable") {
		t.Fatalf("failed chunk should carry marker, got %q", res.Segments[1].Text)
	}
}

func TestTranscribe_Chunked_HardFailWhenMajorityFail(t *testing.T) {
	path := writeTempAudio(t, 10)
	prov := &fakeProvider{maxFileSize: 1 << 30, responder: func(_ int, req TranscribeRequest) (string, error) {
		if strings.Contains(req.FileName, "chunk_000") {
			return "ok", nil
		}
		return "", errors.New("boom")
	}}

	_, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{
		MaxParallel: 4, ChunkDuration: 300, FFmpegPath: fakeFFmpeg(t, 4), FFprobePath: fakeFFprobe(t, 1200),
	})
	if err == nil {
		t.Fatal("expected hard failure when majority of chunks fail")
	}
}
