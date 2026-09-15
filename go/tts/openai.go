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
	"sync"
)

// FramesContentType is the response a server may answer with when asked: the
// reading as length-prefixed pieces, a big-endian uint32 byte count then that
// many bytes, each piece a complete, independently decodable audio file. A
// server that streams its synthesis can hand over every sentence as it is
// made; one that cannot answers a plain audio body and is read whole.
const FramesContentType = "application/x-lalter-audio-frames"

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
	// the limit hosted endpoints impose; WholeText sends the text as one
	// request whatever its length, for a server with no limit that cuts and
	// streams by sentence itself. A limit is not a target, though. Reading
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

// WholeText, as MaxChars, sends every text as a single request. Only for a
// server that takes any length and streams its sentences as it reads them:
// a hosted endpoint refuses the request, and one that answers whole would
// keep the listener waiting for all of it.
const WholeText = -1

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
	if cfg.MaxChars <= 0 && cfg.MaxChars != WholeText {
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
	pieces := v.pieces(text)
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
	// of order is text read out of order. A piece is several sends when the
	// server frames it, closed by one carrying done.
	slots := make([]chan spoken, len(pieces))
	for i := range slots {
		slots[i] = make(chan spoken, 64)
	}

	// Pieces are handed out in reading order to a fixed number of workers,
	// so the first piece is always the first one read: the listener is
	// waiting on it, and a later piece read ahead of it only delays them.
	queue := make(chan int)
	var wg sync.WaitGroup
	for range v.cfg.Concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range queue {
				send := func(s spoken) error {
					select {
					case slots[idx] <- s:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				err := v.say(ctx, pieces[idx], func(audio []byte) error {
					return send(spoken{audio: audio})
				})
				_ = send(spoken{done: true, err: err})
			}
		}()
	}
	go func() {
		defer close(queue)
		for idx := range pieces {
			select {
			case queue <- idx:
			case <-ctx.Done():
				return
			}
		}
	}()

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

func (v *OpenAIVoice) pieces(text string) []string {
	if v.cfg.MaxChars != WholeText {
		return Split(text, v.cfg.MaxChars)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return []string{text}
}

type spoken struct {
	audio []byte
	done  bool
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
		res, err := v.relay(ctx, slot, cancelled == nil, emit)
		if err != nil {
			return fmt.Errorf("tts: piece %d: %w", i, err)
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
	}
	return cancelled
}

// relay hands a slot's audio to emit as it lands and returns the send that
// closed the piece. Audio is dropped once an earlier piece was cancelled:
// the reading is already short, and the caller only needs the verdict.
func (v *OpenAIVoice) relay(ctx context.Context, slot chan spoken, live bool, emit func([]byte) error) (spoken, error) {
	for {
		var res spoken
		select {
		case res = <-slot:
		case <-ctx.Done():
			return spoken{}, ctx.Err()
		}
		if res.done {
			return res, nil
		}
		if !live {
			continue
		}
		if err := emit(res.audio); err != nil {
			// Nobody is listening any more: stop paying for the rest.
			return spoken{}, err
		}
	}
}

func (v *OpenAIVoice) say(ctx context.Context, text string, emit func([]byte) error) error {
	body, err := json.Marshal(map[string]any{
		"model":           v.cfg.Model,
		"input":           text,
		"voice":           v.cfg.VoiceID,
		"response_format": v.cfg.Format,
	})
	if err != nil {
		return fmt.Errorf("tts: build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.BaseURL+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", FramesContentType+", "+MIMEFor(v.cfg.Format)+";q=0.9, */*;q=0.1")
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.cfg.Client.Do(req)
	if err != nil {
		return fmt.Errorf("tts: call: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tts: status %d: %s", resp.StatusCode, bytes.TrimSpace(detail))
	}

	var total int
	if resp.Header.Get("Content-Type") == FramesContentType {
		total, err = readFrames(resp.Body, emit)
	} else {
		var audio []byte
		audio, err = io.ReadAll(resp.Body)
		if err == nil && len(audio) > 0 {
			total = len(audio)
			err = emit(audio)
		}
	}
	if err != nil {
		return fmt.Errorf("tts: read body: %w", err)
	}
	// A 200 carrying no bytes is a failure, not silence. Joined with the rest,
	// it would drop this piece's text from the audio with nothing reported
	// anywhere, and the gap would outlive the request in whatever cache the
	// caller keeps.
	if total == 0 {
		return fmt.Errorf("tts: no audio for a %d-rune piece", len([]rune(text)))
	}
	return nil
}

// readFrames hands each length-prefixed piece to emit as soon as it is whole,
// and reports how many bytes of audio went by. A body that ends inside a
// piece is an error: the reading was cut, and what arrived must not pass for
// the whole of it.
func readFrames(r io.Reader, emit func([]byte) error) (int, error) {
	var total int
	var head [4]byte
	for {
		if _, err := io.ReadFull(r, head[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, fmt.Errorf("frame header: %w", err)
		}
		n := binary.BigEndian.Uint32(head[:])
		frame := make([]byte, n)
		if _, err := io.ReadFull(r, frame); err != nil {
			return total, fmt.Errorf("frame of %d bytes: %w", n, err)
		}
		total += len(frame)
		if err := emit(frame); err != nil {
			return total, err
		}
	}
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
