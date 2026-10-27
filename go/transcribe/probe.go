package transcribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// A crafted file can keep ffprobe reading indefinitely.
const probeTimeout = 2 * time.Minute

var (
	// ErrNoAudioStream reports a file ffprobe could open but which carries no
	// decodable audio.
	ErrNoAudioStream = errors.New("transcribe: no decodable audio stream")
	// ErrUnreadableMedia reports a file ffprobe could not parse at all.
	ErrUnreadableMedia = errors.New("transcribe: media could not be read")
	// ErrUnboundedDuration reports media without a finite duration, which is
	// what a live stream looks like.
	ErrUnboundedDuration = errors.New("transcribe: media has no finite duration")
)

// MediaInfo is what Probe established about a local file.
type MediaInfo struct {
	Codec           string
	DurationSeconds float64
}

// Probe verifies that path carries a decodable audio stream of finite
// duration. Unlike ProbeDuration it refuses rather than guesses.
func Probe(ctx context.Context, path string, ffprobePath string) (MediaInfo, error) {
	bin, err := ffmpegBinary(ffprobePath, "ffprobe")
	if err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, err)
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var stdout, stderr strings.Builder
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name:format=duration",
		"-of", "json",
		"--", path,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, firstLine(stderr.String(), err))
	}

	return parseProbe(stdout.String())
}

type probeOutput struct {
	Streams []struct {
		CodecName string `json:"codec_name"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func parseProbe(raw string) (MediaInfo, error) {
	var out probeOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return MediaInfo{}, fmt.Errorf("%w: %v", ErrUnreadableMedia, err)
	}
	if len(out.Streams) == 0 || strings.TrimSpace(out.Streams[0].CodecName) == "" {
		return MediaInfo{}, ErrNoAudioStream
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(out.Format.Duration), 64)
	if err != nil || seconds <= 0 {
		return MediaInfo{}, ErrUnboundedDuration
	}
	return MediaInfo{
		Codec:           strings.ToLower(strings.TrimSpace(out.Streams[0].CodecName)),
		DurationSeconds: seconds,
	}, nil
}

func firstLine(stderr string, fallback error) string {
	s := strings.TrimSpace(stderr)
	if s == "" {
		return fallback.Error()
	}
	if i := strings.IndexAny(s, "\r\n"); i != -1 {
		s = s[:i]
	}
	return s
}
