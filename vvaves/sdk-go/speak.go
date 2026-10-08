package sdk

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lalternative/packages/vvaves/sdk-go/internal/wire"
)

// FramesContentType is a streamed reading: big-endian uint32 length-prefixed
// mp3 pieces, each decodable on its own.
const FramesContentType = "application/x-lalter-audio-frames"

// MaxFrameBytes bounds one streamed piece. A length prefix is four bytes the
// server chose; trusting it would allocate up to 4 GiB on a single header.
const MaxFrameBytes = 8 << 20

// ErrFrameTooLarge is a streamed piece announcing more than MaxFrameBytes.
var ErrFrameTooLarge = errors.New("vvaves: streamed frame exceeds MaxFrameBytes")

// Gender picks a voice.
type Gender = wire.HttpapiSpeakRequestGender

const (
	GenderFemale  = wire.Female
	GenderMale    = wire.Male
	GenderNeutral = wire.Neutral
)

// Reading names one text to read. Scope defaults to the client's WithScope;
// an empty ID keys the reading on the text alone.
type Reading struct {
	Text   string
	Scope  string
	ID     string
	Lang   string
	Gender Gender
}

// Audio is a whole reading.
type Audio struct {
	Bytes []byte
	MIME  string
}

func (c *Client) body(r Reading, stream bool) wire.HttpapiSpeakRequest {
	scope := r.Scope
	if scope == "" {
		scope = c.scope
	}
	b := wire.HttpapiSpeakRequest{Text: ptr(r.Text)}
	if scope != "" {
		b.Scope = ptr(scope)
	}
	if r.ID != "" {
		b.Id = ptr(r.ID)
	}
	if r.Lang != "" {
		b.Lang = ptr(r.Lang)
	}
	if r.Gender != "" {
		b.Gender = ptr(r.Gender)
	}
	if stream {
		b.Stream = ptr(true)
	}
	return b
}

// Speak returns a whole reading.
func (c *Client) Speak(ctx context.Context, r Reading) (Audio, error) {
	resp, err := c.speak(ctx, r, false)
	if err != nil {
		return Audio{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return Audio{}, wrap(err)
	}
	if len(b) == 0 {
		return Audio{}, ErrNoAudio
	}
	return Audio{Bytes: b, MIME: audioMIME(resp)}, nil
}

// SpeakStream hands each piece to emit as vvaves reads it. A reading vvaves
// already keeps comes back whole even when streaming was asked, so emit may
// be called once with all of it.
func (c *Client) SpeakStream(ctx context.Context, r Reading, emit func([]byte) error) (string, error) {
	resp, err := c.speak(ctx, r, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != FramesContentType {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", wrap(err)
		}
		if len(b) == 0 {
			return "", ErrNoAudio
		}
		return audioMIME(resp), emit(b)
	}

	var got bool
	for {
		var length [4]byte
		if _, err := io.ReadFull(resp.Body, length[:]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", wrap(fmt.Errorf("read frame length: %w", err))
		}
		n := binary.BigEndian.Uint32(length[:])
		if n > MaxFrameBytes {
			return "", fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, n)
		}
		if n == 0 {
			continue
		}
		piece := make([]byte, n)
		if _, err := io.ReadFull(resp.Body, piece); err != nil {
			return "", wrap(fmt.Errorf("read frame: %w", err))
		}
		got = true
		if err := emit(piece); err != nil {
			return "", err
		}
	}
	if !got {
		return "", ErrNoAudio
	}
	return "audio/mpeg", nil
}

func (c *Client) speak(ctx context.Context, r Reading, stream bool) (*http.Response, error) {
	if err := c.ready(); err != nil {
		return nil, err
	}
	resp, err := c.wire.Speak(ctx, c.body(r, stream))
	if err != nil {
		return nil, wrap(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, statusError(resp.StatusCode, resp.Body)
	}
	return resp, nil
}

// Exists reports whether a reading is ready to be served without paying for
// a synthesis.
func (c *Client) Exists(ctx context.Context, r Reading) (bool, error) {
	if err := c.ready(); err != nil {
		return false, err
	}
	res, err := c.wire.SpeakExistsWithResponse(ctx, c.body(r, false))
	if err != nil {
		return false, wrap(err)
	}
	if res.JSON200 == nil {
		return false, statusFrom(res.StatusCode(), res.Body)
	}
	return deref(res.JSON200.Ready), nil
}

// Prime has vvaves read only the opening of a reading ahead of any listener.
// nil means scheduled, not stored. r.ID is required.
func (c *Client) Prime(ctx context.Context, r Reading) error {
	if err := c.ready(); err != nil {
		return err
	}
	res, err := c.wire.PrimeSpeakWithResponse(ctx, c.body(r, false))
	if err != nil {
		return wrap(err)
	}
	return accepted(res.StatusCode(), res.Body)
}

// Pregenerate has vvaves read a whole reading ahead of any listener. nil
// means scheduled, not stored.
func (c *Client) Pregenerate(ctx context.Context, r Reading) error {
	if err := c.ready(); err != nil {
		return err
	}
	res, err := c.wire.PregenerateSpeakWithResponse(ctx, c.body(r, false))
	if err != nil {
		return wrap(err)
	}
	return accepted(res.StatusCode(), res.Body)
}

func accepted(code int, body []byte) error {
	if code >= 200 && code <= 299 {
		return nil
	}
	return statusFrom(code, body)
}

func audioMIME(resp *http.Response) string {
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "audio/") {
		return "audio/mpeg"
	}
	return ct
}
