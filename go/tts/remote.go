package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// framesContentType mirrors audioreader.FramesContentType. Importing it would
// make this module and audioreader depend on each other, so the wire constant
// is duplicated here; the protocol it names is frozen either way.
const framesContentType = "application/x-lalter-audio-frames"

// RemoteConfig points a voice at a tornade /speak endpoint.
type RemoteConfig struct {
	BaseURL string
	// Scope names where tornade keeps the readings, so two applications
	// sharing one tornade do not overwrite each other's cache. Empty leaves
	// the naming to tornade's default.
	Scope string
	// Client defaults to one with no global timeout, same as OpenAIVoice: a
	// long reading can take minutes, and cancellation belongs to the context.
	Client *http.Client
}

// RemoteVoice reads text through a tornade instance instead of a speech
// service directly. Tornade owns the synthesis, the cache and the store, so
// every application speaking through the same tornade shares one paid reading
// of the same words — and can prime or pregenerate one ahead of any listener.
type RemoteVoice struct {
	cfg RemoteConfig
}

var _ Voice = (*RemoteVoice)(nil)

// NewRemoteVoice wires a voice against tornade, or nil without a BaseURL —
// same contract as the callers' own constructors: absent, not half-present.
func NewRemoteVoice(cfg RemoteConfig) *RemoteVoice {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	return &RemoteVoice{cfg: cfg}
}

func (v *RemoteVoice) Speak(ctx context.Context, text string) ([]byte, string, error) {
	resp, err := v.speak(ctx, text, false)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("tts: read body: %w", err)
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
	}
	return audio, respMIME(resp), nil
}

func (v *RemoteVoice) SpeakStream(ctx context.Context, text string, emit func([]byte) error) (string, error) {
	resp, err := v.speak(ctx, text, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// A cache hit is served whole with its real content type even when the
	// request asked to stream — tornade answers from the store before it
	// considers reading anything aloud. Only a paying listen carries frames.
	if resp.Header.Get("Content-Type") != framesContentType {
		audio, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("tts: read body: %w", err)
		}
		if len(audio) == 0 {
			return "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
		}
		if err := emit(audio); err != nil {
			return "", err
		}
		return respMIME(resp), nil
	}

	var got bool
	for {
		var length [4]byte
		if _, err := io.ReadFull(resp.Body, length[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("tts: read frame length: %w", err)
		}
		piece := make([]byte, binary.BigEndian.Uint32(length[:]))
		if _, err := io.ReadFull(resp.Body, piece); err != nil {
			return "", fmt.Errorf("tts: read frame: %w", err)
		}
		if len(piece) == 0 {
			continue
		}
		got = true
		if err := emit(piece); err != nil {
			return "", err
		}
	}
	if !got {
		return "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
	}
	return MIMEFor("mp3"), nil
}

// Pregenerate asks tornade to read text in full and keep it, ahead of any
// listener. Tornade acknowledges before the reading starts, so a nil return
// means scheduled, not stored; id is what lets the reading be asked for later
// under the same name.
func (v *RemoteVoice) Pregenerate(ctx context.Context, id, text string) error {
	resp, err := v.post(ctx, "/speak/pregenerate", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PrimeOpening asks tornade to read only the start of text and keep it,
// ahead of any listener.
//
// It is what makes pressing play feel immediate: the opening comes out of the
// store at once and the rest is read while it plays, so the listener hears
// one recording. Pregenerate reads the whole text instead — that is the trade
// between them, and it is a question of how many listens are expected, not of
// which caller is asking. A text heard once or not at all wants this one:
// reading all of it ahead pays for every text on the chance that someone
// listens to one.
//
// Tornade acknowledges before the reading starts, so a nil return means
// scheduled, not stored. id is required — an opening nobody can name again is
// one no listener will ever be served.
func (v *RemoteVoice) PrimeOpening(ctx context.Context, id, text string) error {
	resp, err := v.post(ctx, "/speak/prime", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// SpeakNamed reads text under a name, so a reading primed or pregenerated
// earlier under that same name is the one served.
//
// Speak leaves the name out, which has tornade key the reading on the text
// alone: the cache still works, but nothing read ahead of time is ever found,
// because it was stored under an id this call never mentions. A caller that
// primes must ask by the same id it primed with.
func (v *RemoteVoice) SpeakNamed(ctx context.Context, id, text string) ([]byte, string, error) {
	resp, err := v.post(ctx, "/speak", map[string]any{
		"text":  text,
		"scope": v.cfg.Scope,
		"id":    id,
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("tts: read body: %w", err)
	}
	if len(audio) == 0 {
		return nil, "", fmt.Errorf("tts: no audio for a %d-rune text", len([]rune(text)))
	}
	return audio, respMIME(resp), nil
}

func (v *RemoteVoice) speak(ctx context.Context, text string, stream bool) (*http.Response, error) {
	return v.post(ctx, "/speak", map[string]any{
		"text":   text,
		"scope":  v.cfg.Scope,
		"stream": stream,
	})
}

func (v *RemoteVoice) post(ctx context.Context, path string, payload map[string]any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: call: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("tts: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	return resp, nil
}

func respMIME(resp *http.Response) string {
	ct := resp.Header.Get("Content-Type")
	if ct == "" || strings.HasPrefix(ct, "text/") || strings.HasPrefix(ct, "application/json") {
		return MIMEFor("mp3")
	}
	return ct
}
