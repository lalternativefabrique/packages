// Package providers implements transcribe.Provider for OpenAI-compatible
// endpoints, Groq and whisper.cpp.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/go/transcribe"
)

const (
	defaultMaxFileSize = 25 * 1024 * 1024
	requestTimeout     = 15 * time.Minute
	maxBodySnippet     = 400
)

// OpenAI sends audio to an OpenAI-compatible /v1/audio/transcriptions
// endpoint. It accepts compressed formats as they are.
type OpenAI struct {
	client      *http.Client
	apiURL      string
	apiKey      string
	modelID     string
	maxFileSize int64
}

// OpenAIConfig configures an OpenAI provider.
type OpenAIConfig struct {
	APIURL  string
	APIKey  string
	ModelID string
	// MaxFileSize in bytes; 0 means 25 MB.
	MaxFileSize int64
	// HTTPClient overrides the default client (15 minute timeout).
	HTTPClient *http.Client
}

// NewOpenAI returns a provider for cfg.
func NewOpenAI(cfg OpenAIConfig) *OpenAI {
	maxSize := cfg.MaxFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFileSize
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return &OpenAI{
		client:      client,
		apiURL:      cfg.APIURL,
		apiKey:      cfg.APIKey,
		modelID:     cfg.ModelID,
		maxFileSize: maxSize,
	}
}

// MaxFileSize implements transcribe.Provider.
func (p *OpenAI) MaxFileSize() int64 { return p.maxFileSize }

// NeedsWAVConversion implements transcribe.Provider.
func (p *OpenAI) NeedsWAVConversion() bool { return false }

// Transcribe implements transcribe.Provider. It asks for verbose_json and
// falls back to the plain text field when the backend returns no segments.
func (p *OpenAI) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (*transcribe.TranscribeResult, error) {
	if p.apiURL == "" {
		return nil, fmt.Errorf("openai: api url is not configured")
	}
	if req.File == nil {
		return nil, fmt.Errorf("openai: audio file is required")
	}

	fileName := req.FileName
	if fileName == "" {
		fileName = "audio.m4a"
	}
	fields := map[string]string{"response_format": "verbose_json"}
	if req.Language != "" {
		fields["language"] = req.Language
	}
	if p.modelID != "" {
		fields["model"] = p.modelID
	}
	body, contentType, err := multipartBody("file", fileName, req.File, fields)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, body)
	if err != nil {
		return nil, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	respBody, err := do(p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}

	text := strings.TrimSpace(string(respBody))
	var payload struct {
		Text     string `json:"text"`
		Language string `json:"language"`
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	var segments []transcribe.ProviderSegment
	if err := json.Unmarshal(respBody, &payload); err == nil {
		if payload.Text != "" {
			text = strings.TrimSpace(payload.Text)
		}
		for _, s := range payload.Segments {
			segments = append(segments, transcribe.ProviderSegment{
				Text:  strings.TrimSpace(s.Text),
				Start: s.Start,
				End:   s.End,
			})
		}
	}

	return &transcribe.TranscribeResult{
		Text:     text,
		Segments: segments,
		Language: strings.ToLower(strings.TrimSpace(payload.Language)),
	}, nil
}

func multipartBody(fileField, fileName string, file io.Reader, fields map[string]string) (*bytes.Buffer, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return nil, "", fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", fmt.Errorf("copy audio file: %w", err)
	}
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", fmt.Errorf("write field %s: %w", k, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart body: %w", err)
	}
	return &body, writer.FormDataContentType(), nil
}

func do(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, &transcribe.RateLimitError{
			RetryAfter: transcribe.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			StatusCode: resp.StatusCode,
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("transcription failed: status=%d body=%q", resp.StatusCode, snippet(respBody))
	}
	return respBody, nil
}

func snippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > maxBodySnippet {
		return s[:maxBodySnippet] + "...(truncated)"
	}
	return s
}
