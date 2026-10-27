package providers

import "testing"

func clearWhisperEnv(t *testing.T) {
	t.Helper()
	for _, prefix := range []string{"", "VOICE_", "SOURCE_"} {
		for _, k := range []string{"WHISPER_PROVIDER", "WHISPER_API_URL", "WHISPER_API_KEY", "WHISPER_MODEL", "WHISPER_MAX_FILE_SIZE"} {
			t.Setenv(prefix+k, "")
		}
	}
}

func TestFromEnv_PrefixUsed(t *testing.T) {
	clearWhisperEnv(t)
	t.Setenv("VOICE_WHISPER_PROVIDER", "openai")
	t.Setenv("VOICE_WHISPER_API_URL", "https://voice.example/v1/audio/transcriptions")
	t.Setenv("VOICE_WHISPER_API_KEY", "voice-key")
	t.Setenv("VOICE_WHISPER_MODEL", "whisper-voice")
	t.Setenv("VOICE_WHISPER_MAX_FILE_SIZE", "1234")

	o, ok := FromEnv("VOICE_").(*OpenAI)
	if !ok {
		t.Fatalf("expected *OpenAI")
	}
	if o.apiURL != "https://voice.example/v1/audio/transcriptions" || o.apiKey != "voice-key" || o.modelID != "whisper-voice" || o.maxFileSize != 1234 {
		t.Errorf("got url=%q key=%q model=%q max=%d", o.apiURL, o.apiKey, o.modelID, o.maxFileSize)
	}
}

func TestFromEnv_FallbackToGlobal(t *testing.T) {
	clearWhisperEnv(t)
	t.Setenv("WHISPER_PROVIDER", "openai")
	t.Setenv("WHISPER_API_URL", "https://global.example/v1/audio/transcriptions")
	t.Setenv("WHISPER_API_KEY", "global-key")
	t.Setenv("WHISPER_MODEL", "whisper-global")

	o, ok := FromEnv("VOICE_").(*OpenAI)
	if !ok {
		t.Fatalf("expected *OpenAI")
	}
	if o.apiURL != "https://global.example/v1/audio/transcriptions" || o.apiKey != "global-key" || o.modelID != "whisper-global" {
		t.Errorf("got url=%q key=%q model=%q", o.apiURL, o.apiKey, o.modelID)
	}
}

func TestFromEnv_PartialOverride(t *testing.T) {
	clearWhisperEnv(t)
	t.Setenv("WHISPER_API_URL", "https://global.example/v1/audio/transcriptions")
	t.Setenv("WHISPER_API_KEY", "global-key")
	t.Setenv("WHISPER_MODEL", "whisper-global")
	t.Setenv("VOICE_WHISPER_API_KEY", "voice-only-key")

	o, ok := FromEnv("VOICE_").(*OpenAI)
	if !ok {
		t.Fatalf("expected *OpenAI")
	}
	if o.apiURL != "https://global.example/v1/audio/transcriptions" || o.apiKey != "voice-only-key" || o.modelID != "whisper-global" {
		t.Errorf("got url=%q key=%q model=%q", o.apiURL, o.apiKey, o.modelID)
	}
}

func TestFromEnv_Defaults(t *testing.T) {
	clearWhisperEnv(t)

	o, ok := FromEnv("").(*OpenAI)
	if !ok {
		t.Fatalf("expected *OpenAI by default")
	}
	if o.apiURL != "" || o.apiKey != "" || o.modelID != "" || o.maxFileSize != defaultMaxFileSize {
		t.Errorf("got url=%q key=%q model=%q max=%d", o.apiURL, o.apiKey, o.modelID, o.maxFileSize)
	}
}

func TestFromEnv_OtherProviders(t *testing.T) {
	clearWhisperEnv(t)
	t.Setenv("SOURCE_WHISPER_PROVIDER", "groq")
	if _, ok := FromEnv("SOURCE_").(*Groq); !ok {
		t.Error("expected *Groq")
	}
	t.Setenv("WHISPER_PROVIDER", "whispercpp")
	if _, ok := FromEnv("").(*WhisperCpp); !ok {
		t.Error("expected *WhisperCpp")
	}
}
