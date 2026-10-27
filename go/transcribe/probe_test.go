package transcribe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseProbe_Verdicts(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    error
		seconds float64
		codec   string
	}{
		"audio with duration": {
			raw:     `{"streams":[{"codec_name":"mp3"}],"format":{"duration":"3600.024"}}`,
			seconds: 3600.024,
			codec:   "mp3",
		},
		"video container carrying audio": {
			raw:     `{"streams":[{"codec_name":"aac"}],"format":{"duration":"125.5"}}`,
			seconds: 125.5,
			codec:   "aac",
		},
		"no audio track": {
			raw:  `{"streams":[],"format":{"duration":"120.0"}}`,
			want: ErrNoAudioStream,
		},
		"empty codec name": {
			raw:  `{"streams":[{"codec_name":""}],"format":{"duration":"120.0"}}`,
			want: ErrNoAudioStream,
		},
		"live stream reports no duration": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{}}`,
			want: ErrUnboundedDuration,
		},
		"duration N/A": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{"duration":"N/A"}}`,
			want: ErrUnboundedDuration,
		},
		"zero duration": {
			raw:  `{"streams":[{"codec_name":"aac"}],"format":{"duration":"0"}}`,
			want: ErrUnboundedDuration,
		},
		"not json": {
			raw:  `ffprobe: command not found`,
			want: ErrUnreadableMedia,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseProbe(tc.raw)
			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProbe: %v", err)
			}
			if got.DurationSeconds != tc.seconds {
				t.Errorf("DurationSeconds = %v, want %v", got.DurationSeconds, tc.seconds)
			}
			if got.Codec != tc.codec {
				t.Errorf("Codec = %q, want %q", got.Codec, tc.codec)
			}
		})
	}
}

func TestParseProbe_RefusesRatherThanDefaults(t *testing.T) {
	for _, raw := range []string{`{}`, `{"streams":[]}`, `{"streams":[{"codec_name":"aac"}]}`} {
		if _, err := parseProbe(raw); err == nil {
			t.Errorf("parseProbe(%s) = nil error, want a refusal", raw)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("moov atom not found\nsecond line", errors.New("exit 1")); got != "moov atom not found" {
		t.Errorf("firstLine = %q", got)
	}
	if got := firstLine("   ", errors.New("exit 1")); got != "exit 1" {
		t.Errorf("empty stderr should fall back to the error: %q", got)
	}
}

func TestProbe_RefusesGarbage(t *testing.T) {
	requireBin(t, "ffprobe")
	path := filepath.Join(t.TempDir(), "source.mp3")
	if err := os.WriteFile(path, []byte("not audio at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := Probe(context.Background(), path, "")
	if err == nil {
		t.Fatal("Probe accepted a file with no audio stream")
	}
	if !errors.Is(err, ErrNoAudioStream) && !errors.Is(err, ErrUnreadableMedia) && !errors.Is(err, ErrUnboundedDuration) {
		t.Errorf("error = %v, want one of the probe refusals", err)
	}
}

func TestRequireMeasurableAudio_RefusesBeforeProvider(t *testing.T) {
	requireBin(t, "ffprobe")
	path := filepath.Join(t.TempDir(), "source.mp3")
	if err := os.WriteFile(path, []byte("not audio at all"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	prov := &countingProvider{}

	_, err := New(prov).Transcribe(context.Background(), FileSource{Path: path}, Options{RequireMeasurableAudio: true})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if prov.calls != 0 {
		t.Errorf("provider called %d time(s), want 0", prov.calls)
	}
}

func TestOptions_RequireMeasurableAudioDefaultsOff(t *testing.T) {
	if withDefaults(Options{}).RequireMeasurableAudio {
		t.Error("withDefaults turns the gate on")
	}
}
