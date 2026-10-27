package providers

import (
	"os"
	"strconv"
	"strings"

	"github.com/lalternative/packages/go/transcribe"
)

// FromEnv builds a provider from <prefix>WHISPER_* variables, each falling
// back to the unprefixed WHISPER_* key: WHISPER_PROVIDER ("openai" by default,
// "groq" or "whispercpp"), WHISPER_API_URL, WHISPER_API_KEY, WHISPER_MODEL and
// WHISPER_MAX_FILE_SIZE.
func FromEnv(prefix string) transcribe.Provider {
	providerType := strings.TrimSpace(strings.ToLower(envWithFallback(prefix, "WHISPER_PROVIDER")))
	apiURL := strings.TrimSpace(envWithFallback(prefix, "WHISPER_API_URL"))
	apiKey := strings.TrimSpace(envWithFallback(prefix, "WHISPER_API_KEY"))
	modelID := strings.TrimSpace(envWithFallback(prefix, "WHISPER_MODEL"))
	maxFileSize := envInt64WithFallback(prefix, "WHISPER_MAX_FILE_SIZE")

	switch providerType {
	case "whispercpp":
		return NewWhisperCpp(WhisperCppConfig{APIURL: apiURL, MaxFileSize: maxFileSize})
	case "groq":
		return NewGroq(GroqConfig{APIURL: apiURL, APIKey: apiKey, ModelID: modelID, MaxFileSize: maxFileSize})
	default:
		return NewOpenAI(OpenAIConfig{APIURL: apiURL, APIKey: apiKey, ModelID: modelID, MaxFileSize: maxFileSize})
	}
}

func envWithFallback(prefix, key string) string {
	if prefix != "" {
		if v := os.Getenv(prefix + key); strings.TrimSpace(v) != "" {
			return v
		}
	}
	return os.Getenv(key)
}

func envInt64WithFallback(prefix, key string) int64 {
	if v, err := strconv.ParseInt(envWithFallback(prefix, key), 10, 64); err == nil && v > 0 {
		return v
	}
	return 0
}
