package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// MaxAudioBytes is the largest recording vvaves accepts on /transcribe.
const MaxAudioBytes = 25 << 20

// ErrAudioTooLarge is a recording past MaxAudioBytes, refused before upload.
var ErrAudioTooLarge = errors.New("vvaves: recording exceeds MaxAudioBytes")

// Transcribe turns a recording into text. The key or token needs the
// vvaves:transcribe scope; language may be empty.
//
// Hand-written until /transcribe is in the contract this package generates
// from; then it moves onto internal/wire like the speak routes.
func (c *Client) Transcribe(ctx context.Context, filename string, audio io.Reader, language string) (string, error) {
	if err := c.ready(); err != nil {
		return "", err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("audio", filename)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	n, err := io.Copy(part, io.LimitReader(audio, MaxAudioBytes+1))
	if err != nil {
		return "", fmt.Errorf("vvaves: read recording: %w", err)
	}
	if n > MaxAudioBytes {
		return "", ErrAudioTooLarge
	}
	if language != "" {
		if err := form.WriteField("language", language); err != nil {
			return "", fmt.Errorf("%w: %w", ErrBadRequest, err)
		}
	}
	if err := form.Close(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/transcribe", &body)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadRequest, err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	if err := c.authEditor()(ctx, req); err != nil {
		return "", fmt.Errorf("vvaves: authorize: %w", err)
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		return "", wrap(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if err := statusError(resp.StatusCode, resp.Body); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: unexpected status %d", ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", wrap(fmt.Errorf("decode transcription: %w", err))
	}
	return out.Text, nil
}
