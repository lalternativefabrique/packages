package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/lalternative/packages/go/transcribe"
)

// WhisperCpp sends audio to a whisper.cpp HTTP server, which takes WAV PCM
// 16kHz mono and no authentication.
type WhisperCpp struct {
	client      *http.Client
	apiURL      string
	maxFileSize int64
}

// WhisperCppConfig configures a whisper.cpp provider.
type WhisperCppConfig struct {
	APIURL string
	// MaxFileSize in bytes; 0 means 25 MB.
	MaxFileSize int64
	HTTPClient  *http.Client
}

// NewWhisperCpp returns a provider for cfg.
func NewWhisperCpp(cfg WhisperCppConfig) *WhisperCpp {
	maxSize := cfg.MaxFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFileSize
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return &WhisperCpp{client: client, apiURL: cfg.APIURL, maxFileSize: maxSize}
}

// MaxFileSize implements transcribe.Provider.
func (p *WhisperCpp) MaxFileSize() int64 { return p.maxFileSize }

// NeedsWAVConversion implements transcribe.Provider.
func (p *WhisperCpp) NeedsWAVConversion() bool { return true }

// Transcribe implements transcribe.Provider. whisper.cpp returns
// {"transcription":[{"text":...}]}; some builds answer {"text":...} instead.
func (p *WhisperCpp) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (*transcribe.TranscribeResult, error) {
	if p.apiURL == "" {
		return nil, fmt.Errorf("whispercpp: api url is not configured")
	}
	if req.File == nil {
		return nil, fmt.Errorf("whispercpp: audio file is required")
	}

	fileName := req.FileName
	if fileName == "" {
		fileName = "audio.wav"
	}
	fields := map[string]string{"response_format": "json"}
	if req.Language != "" {
		fields["language"] = req.Language
	}
	body, contentType, err := multipartBody("audio_file", fileName, req.File, fields)
	if err != nil {
		return nil, fmt.Errorf("whispercpp: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL, body)
	if err != nil {
		return nil, fmt.Errorf("whispercpp: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)

	respBody, err := do(p.client, httpReq)
	if err != nil {
		return nil, fmt.Errorf("whispercpp: %w", err)
	}

	text := strings.TrimSpace(string(respBody))
	var cppPayload struct {
		Transcription []struct {
			Text string `json:"text"`
		} `json:"transcription"`
	}
	if err := json.Unmarshal(respBody, &cppPayload); err == nil && len(cppPayload.Transcription) > 0 {
		var parts []string
		for _, t := range cppPayload.Transcription {
			if s := strings.TrimSpace(t.Text); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			text = strings.Join(parts, " ")
		}
	} else {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(respBody, &payload); err == nil && payload.Text != "" {
			text = strings.TrimSpace(payload.Text)
		}
	}

	return &transcribe.TranscribeResult{Text: text}, nil
}
