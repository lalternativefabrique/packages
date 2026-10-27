package providers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/lalternative/packages/go/transcribe"
)

const (
	groqDefaultAPIURL  = "https://api.groq.com/openai/v1/audio/transcriptions"
	groqDefaultModel   = "whisper-large-v3"
	groqDefaultMaxSize = 25 * 1024 * 1024
)

// Groq transcribes through Groq's OpenAI-compatible endpoint with Groq's
// defaults: whisper-large-v3 and a 25 MB payload.
type Groq struct {
	inner *OpenAI
}

// GroqConfig configures a Groq provider. Empty fields take Groq's defaults.
type GroqConfig struct {
	APIURL      string
	APIKey      string
	ModelID     string
	MaxFileSize int64
	HTTPClient  *http.Client
}

// NewGroq returns a provider for cfg.
func NewGroq(cfg GroqConfig) *Groq {
	if cfg.APIURL == "" {
		cfg.APIURL = groqDefaultAPIURL
	}
	if cfg.ModelID == "" {
		cfg.ModelID = groqDefaultModel
	}
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = groqDefaultMaxSize
	}
	return &Groq{inner: NewOpenAI(OpenAIConfig(cfg))}
}

// MaxFileSize implements transcribe.Provider.
func (p *Groq) MaxFileSize() int64 { return p.inner.MaxFileSize() }

// NeedsWAVConversion implements transcribe.Provider.
func (p *Groq) NeedsWAVConversion() bool { return false }

// Transcribe implements transcribe.Provider.
func (p *Groq) Transcribe(ctx context.Context, req transcribe.TranscribeRequest) (*transcribe.TranscribeResult, error) {
	res, err := p.inner.Transcribe(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("groq: %w", err)
	}
	return res, nil
}
