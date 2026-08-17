package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// DefaultConcurrency caps how many pieces are read at once. Four covers most
// texts in a single round without leaning on a rate limit.
const DefaultConcurrency = 4

// Config points the client at a service and picks a voice. Every field has a
// default, so the zero value talks to OpenAI; BaseURL is what you change to
// talk to a Piper container instead.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	VoiceID string
	// Format must be a frame-based codec — mp3, opus, aac, flac. Pieces are
	// joined as bytes, which works for those because the codec settings stay
	// identical across requests for one (model, voice, format). wav would need
	// its header rewritten to declare the real length, and a wav built this
	// way announces the duration of its first piece alone.
	Format string
	// Concurrency defaults to DefaultConcurrency.
	Concurrency int
	// MaxChars is how much text goes into one request. It defaults to MaxChars,
	// the limit hosted endpoints impose — but a limit is not a target. Reading
	// happens one request at a time, so a text that fits in a single one is
	// read serially however high Concurrency is set: cutting smaller is what
	// turns waiting into parallel work.
	//
	// A self-hosted voice is the case where this matters. It has no per-request
	// limit and is slower per character than a hosted one, so leaving the
	// hosted limit in place reads a whole page as one long utterance while
	// three of its four workers sit idle.
	MaxChars int
	// Client defaults to one with no global timeout: a long piece can take
	// most of a minute, and cancellation belongs to the context rather than to
	// a deadline that cannot tell slow from stuck.
	Client *http.Client
	// OnUsage, when set, is called with the character count before reading
	// begins — these services bill per character of input, which is known in
	// advance. It is the seam for whatever the caller meters with; this
	// package has no opinion on it and no dependency on one.
	OnUsage func(chars int)
}

// OpenAIVoice reads text through the /v1/audio/speech protocol.
type OpenAIVoice struct {
	cfg Config
}

// NewOpenAIVoice wires a voice, filling in what the config leaves out.
func NewOpenAIVoice(cfg Config) *OpenAIVoice {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com"
	}
	if cfg.Model == "" {
		cfg.Model = "tts-1"
	}
	if cfg.VoiceID == "" {
		cfg.VoiceID = "nova"
	}
	if cfg.Format == "" {
		cfg.Format = "mp3"
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.MaxChars <= 0 {
		cfg.MaxChars = MaxChars
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	return &OpenAIVoice{cfg: cfg}
}

// MIME is the content type this voice's audio carries.
func (v *OpenAIVoice) MIME() string { return MIMEFor(v.cfg.Format) }

func (v *OpenAIVoice) Speak(ctx context.Context, text string) ([]byte, string, error) {
	var out []byte
	mime, err := v.SpeakStream(ctx, text, func(audio []byte) error {
		out = append(out, audio...)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return out, mime, nil
}

func (v *OpenAIVoice) SpeakStream(ctx context.Context, text string, emit func([]byte) error) (string, error) {
	pieces := Split(text, v.cfg.MaxChars)
	if len(pieces) == 0 {
		return "", errors.New("tts: nothing to read")
	}
	if v.cfg.OnUsage != nil {
		v.cfg.OnUsage(len([]rune(text)))
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// One slot per piece, filled by its own goroutine. Reading the slots in
	// order is what turns parallel work back into ordered speech: a piece that
	// finishes early waits for the ones before it, because audio arriving out
	// of order is text read out of order.
	slots := make([]chan spoken, len(pieces))
	for i := range slots {
		slots[i] = make(chan spoken, 1)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, v.cfg.Concurrency)
	for i, piece := range pieces {
		wg.Add(1)
		go func(idx int, text string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				slots[idx] <- spoken{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			audio, err := v.say(ctx, text)
			slots[idx] <- spoken{audio: audio, err: err}
		}(i, piece)
	}

	err := v.drain(ctx, slots, emit)
	// Cancel before waiting: pieces still in flight have nowhere to go now,
	// and their goroutines exit on the cancelled context.
	cancel()
	wg.Wait()
	if err != nil {
		return "", err
	}
	return MIMEFor(v.cfg.Format), nil
}

type spoken struct {
	audio []byte
	err   error
}

// drain reads the slots in order and emits what they hold.
//
// The first failure ends the reading, and the report names the piece that
// actually failed. A cancelled piece is usually collateral — the first real
// failure cancels its siblings — so it is only reported when nothing better
// turns up among the pieces already in hand.
func (v *OpenAIVoice) drain(ctx context.Context, slots []chan spoken, emit func([]byte) error) error {
	var cancelled error
	for i, slot := range slots {
		var res spoken
		select {
		case res = <-slot:
		case <-ctx.Done():
			return fmt.Errorf("tts: piece %d: %w", i, ctx.Err())
		}

		if res.err != nil {
			if errors.Is(res.err, context.Canceled) {
				// Keep looking: the piece that caused this cancellation may be
				// further along, and its error is the one worth reporting.
				if cancelled == nil {
					cancelled = fmt.Errorf("tts: piece %d: %w", i, res.err)
				}
				continue
			}
			return fmt.Errorf("tts: piece %d: %w", i, res.err)
		}
		if cancelled != nil {
			// A piece succeeded after one was cancelled: whatever cancelled it
			// came from outside, and the audio is short either way.
			return cancelled
		}
		if err := emit(res.audio); err != nil {
			// Nobody is listening any more: stop paying for the rest.
			return err
		}
	}
	return cancelled
}

func (v *OpenAIVoice) say(ctx context.Context, text string) ([]byte, error) {
	body, err := json.Marshal(map[string]any{
		"model":           v.cfg.Model,
		"input":           text,
		"voice":           v.cfg.VoiceID,
		"response_format": v.cfg.Format,
	})
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.BaseURL+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("tts: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}
	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tts: read body: %w", err)
	}
	// A 200 carrying no bytes is a failure, not silence. Joined with the rest,
	// it would drop this piece's text from the audio with nothing reported
	// anywhere, and the gap would outlive the request in whatever cache the
	// caller keeps.
	if len(audio) == 0 {
		return nil, fmt.Errorf("tts: no audio for a %d-rune piece", len([]rune(text)))
	}
	return audio, nil
}

// MIMEFor names the content type of an audio format.
func MIMEFor(format string) string {
	switch format {
	case "mp3":
		return "audio/mpeg"
	case "wav":
		return "audio/wav"
	case "opus":
		return "audio/ogg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}
