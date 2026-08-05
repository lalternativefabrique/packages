package agent

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloudwego/eino-ext/components/model/claude"
	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
)

const (
	mistralBaseURL  = "https://api.mistral.ai/v1"
	deepseekBaseURL = "https://api.deepseek.com/v1"
)

// newChatModel builds an Eino ToolCallingChatModel from a Provider config.
//
// Supported Kind values:
//   - "openai"        — native OpenAI Chat Completions
//   - "openai-compat" — any OpenAI-compatible endpoint (Groq, OpenRouter, Together, vLLM, ...)
//   - "mistral"       — Mistral via openai-compat (Mistral exposes an OpenAI-shaped API)
//   - "deepseek"      — DeepSeek via openai-compat (api.deepseek.com/v1). Strong
//     tool-calling at fraction of Sonnet's cost. Reasoning mode (`thinking`) is
//     NOT enabled through this preset because the upstream openai adapter
//     cannot pass DeepSeek's non-standard `thinking` field — use the chat-only
//     models (deepseek-v4-flash, deepseek-v4-pro) here.
//   - "ovh"           — OVHcloud AI Endpoints (Kepler, Nebula, ...) via openai-compat
//   - "anthropic"     — native Anthropic Messages API via the claude adapter
//
// The returned model is wrapped in a retry decorator so callers always get
// a model that transparently handles 429s.
func newChatModel(p Provider) (model.ToolCallingChatModel, error) {
	ctx := context.Background()

	switch p.Kind {
	case "openai":
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:     p.APIKey,
			Model:      p.Model,
			BaseURL:    p.BaseURL,
			HTTPClient: newRetryHTTPClient(),
		})

	case "openai-compat":
		if p.BaseURL == "" {
			return nil, fmt.Errorf("agent: openai-compat provider requires BaseURL")
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:     p.APIKey,
			Model:      p.Model,
			BaseURL:    p.BaseURL,
			HTTPClient: newRetryHTTPClient(),
		})

	case "mistral":
		baseURL := p.BaseURL
		if baseURL == "" {
			baseURL = mistralBaseURL
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:     p.APIKey,
			Model:      p.Model,
			BaseURL:    baseURL,
			HTTPClient: newRetryHTTPClient(),
		})

	case "deepseek":
		baseURL := p.BaseURL
		if baseURL == "" {
			baseURL = deepseekBaseURL
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:     p.APIKey,
			Model:      p.Model,
			BaseURL:    baseURL,
			HTTPClient: newRetryHTTPClient(),
		})

	case "ovh":
		if p.BaseURL == "" {
			return nil, fmt.Errorf("agent: ovh provider requires BaseURL (e.g. https://oai.endpoints.kepler.ai.cloud.ovh.net/v1)")
		}
		return openai.NewChatModel(ctx, &openai.ChatModelConfig{
			APIKey:     p.APIKey,
			Model:      p.Model,
			BaseURL:    p.BaseURL,
			HTTPClient: newRetryHTTPClient(),
		})

	case "anthropic":
		return claude.NewChatModel(ctx, &claude.Config{
			APIKey:     p.APIKey,
			Model:      p.Model,
			MaxTokens:  4096,
			HTTPClient: newRetryHTTPClient(),
		})

	default:
		return nil, fmt.Errorf("agent: unsupported provider kind %q", p.Kind)
	}
}

// newRetryHTTPClient returns an *http.Client whose RoundTripper recognises
// 429 responses and surfaces them as a typed *RateLimitError so the model-level
// retry decorator can react to them.
func newRetryHTTPClient() *http.Client {
	return &http.Client{
		Transport: &retryTransport{base: http.DefaultTransport},
	}
}
