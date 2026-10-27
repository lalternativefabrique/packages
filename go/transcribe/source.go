package transcribe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Source is where the audio comes from. Localize must materialize it as a
// seekable local file: m4a/mp4 keep their index at the end of the file and
// cannot be segmented from a pipe. The returned cleanup is deferred by the
// caller.
type Source interface {
	Localize(ctx context.Context) (path string, cleanup func(), err error)
}

// URLSource localizes a direct audio URL through ffmpeg.
type URLSource struct {
	URL string
	// WAV re-encodes to WAV PCM 16kHz mono, as whisper.cpp expects. When false
	// the codec is stream-copied into a matching container.
	WAV bool
	// FFmpegPath and FFprobePath override the PATH lookup.
	FFmpegPath  string
	FFprobePath string
}

// Localize downloads the URL into a temp file.
func (s URLSource) Localize(ctx context.Context) (string, func(), error) {
	ffmpegPath, err := ffmpegBinary(s.FFmpegPath, "ffmpeg")
	if err != nil {
		return "", nil, err
	}

	tmpDir, err := os.MkdirTemp("", "transcribe-src-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	var outputFile string
	var args []string
	if s.WAV {
		outputFile = filepath.Join(tmpDir, "audio.wav")
		args = []string{"-y", "-i", s.URL, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "--", outputFile}
	} else {
		outputFile = filepath.Join(tmpDir, "audio"+s.copyExtension(ctx))
		args = []string{"-y", "-i", s.URL, "-vn", "-c:a", "copy", "--", outputFile}
	}

	var stderr strings.Builder
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("ffmpeg download failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	if _, err := os.Stat(outputFile); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("downloaded audio file not found: %w", err)
	}

	return outputFile, cleanup, nil
}

// No single container holds every codec under stream-copy: MP4 rejects mp3.
var containerForCodec = map[string]string{
	"aac":       ".m4a",
	"alac":      ".m4a",
	"mp3":       ".mp3",
	"opus":      ".ogg",
	"vorbis":    ".ogg",
	"flac":      ".flac",
	"pcm_s16le": ".wav",
	"pcm_s16be": ".wav",
	"pcm_f32le": ".wav",
}

// copyExtension picks the output container from the probed codec, never from
// the URL: ffmpeg selects its muxer from the extension, and an HLS URL
// (.m3u8) would make it re-emit a playlist instead of one audio file.
func (s URLSource) copyExtension(ctx context.Context) string {
	const fallback = ".m4a"
	ffprobePath, err := ffmpegBinary(s.FFprobePath, "ffprobe")
	if err != nil {
		return fallback
	}

	var stdout strings.Builder
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"--", s.URL,
	)
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return fallback
	}

	codec := strings.ToLower(strings.TrimSpace(stdout.String()))
	if i := strings.IndexAny(codec, "\r\n"); i != -1 {
		codec = codec[:i]
	}
	if ext, ok := containerForCodec[codec]; ok {
		return ext
	}
	return fallback
}

// FileSource localizes audio already on the local filesystem. The file's
// lifecycle stays with whoever created it.
type FileSource struct {
	Path string
}

// Localize returns the path with a no-op cleanup.
func (s FileSource) Localize(_ context.Context) (string, func(), error) {
	if _, err := os.Stat(s.Path); err != nil {
		return "", nil, fmt.Errorf("file source not found: %w", err)
	}
	return s.Path, func() {}, nil
}
