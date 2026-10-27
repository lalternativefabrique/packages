package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lalternative/packages/go/transcribe"
)

func readForm(t *testing.T, r *http.Request) (fields map[string]string, fileField, fileName string) {
	t.Helper()
	fields = map[string]string{}
	mr, err := r.MultipartReader()
	if err != nil {
		t.Fatalf("MultipartReader: %v", err)
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return fields, fileField, fileName
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		b, _ := io.ReadAll(part)
		if part.FileName() != "" {
			fileField, fileName = part.FormName(), part.FileName()
			continue
		}
		fields[part.FormName()] = string(b)
	}
}

func TestOpenAI_VerboseJSONSegments(t *testing.T) {
	var fields map[string]string
	var fileField, fileName, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fields, fileField, fileName = readForm(t, r)
		_, _ = w.Write([]byte(`{"text":" bonjour le monde ","language":"FR","segments":[{"start":0,"end":1.5,"text":" bonjour "},{"start":1.5,"end":3,"text":"le monde"}]}`))
	}))
	defer srv.Close()

	p := NewOpenAI(OpenAIConfig{APIURL: srv.URL, APIKey: "secret", ModelID: "whisper-1"})
	res, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{
		FileName: "chunk_000.mp3", File: strings.NewReader("bytes"), Language: "fr",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "bonjour le monde" || res.Language != "fr" {
		t.Errorf("got text=%q language=%q", res.Text, res.Language)
	}
	if len(res.Segments) != 2 || res.Segments[1].Start != 1.5 || res.Segments[1].Text != "le monde" {
		t.Errorf("segments = %+v", res.Segments)
	}
	if auth != "Bearer secret" {
		t.Errorf("Authorization = %q", auth)
	}
	if fileField != "file" || fileName != "chunk_000.mp3" {
		t.Errorf("file part = %s/%s", fileField, fileName)
	}
	if fields["model"] != "whisper-1" || fields["language"] != "fr" || fields["response_format"] != "verbose_json" {
		t.Errorf("fields = %v", fields)
	}
}

func TestOpenAI_PlainTextFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("raw transcript"))
	}))
	defer srv.Close()

	res, err := NewOpenAI(OpenAIConfig{APIURL: srv.URL}).Transcribe(context.Background(), transcribe.TranscribeRequest{File: strings.NewReader("x")})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "raw transcript" || res.Segments != nil {
		t.Errorf("got %+v", res)
	}
}

func TestOpenAI_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := NewGroq(GroqConfig{APIURL: srv.URL}).Transcribe(context.Background(), transcribe.TranscribeRequest{File: strings.NewReader("x")})
	var rle *transcribe.RateLimitError
	if !errors.As(err, &rle) {
		t.Fatalf("error = %v, want RateLimitError through the groq wrapper", err)
	}
	if rle.RetryAfter != 7*time.Second || rle.StatusCode != 429 {
		t.Errorf("got %+v", rle)
	}
}

func TestOpenAI_ErrorCarriesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"bad audio"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := NewOpenAI(OpenAIConfig{APIURL: srv.URL}).Transcribe(context.Background(), transcribe.TranscribeRequest{File: strings.NewReader("x")})
	if err == nil || !strings.Contains(err.Error(), "status=400") || !strings.Contains(err.Error(), "bad audio") {
		t.Errorf("error = %v", err)
	}
}

func TestOpenAI_Unconfigured(t *testing.T) {
	p := NewOpenAI(OpenAIConfig{})
	if _, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{File: strings.NewReader("x")}); err == nil {
		t.Error("empty api url: want error")
	}
	if _, err := NewOpenAI(OpenAIConfig{APIURL: "http://x"}).Transcribe(context.Background(), transcribe.TranscribeRequest{}); err == nil {
		t.Error("nil file: want error")
	}
	if p.MaxFileSize() != defaultMaxFileSize || p.NeedsWAVConversion() {
		t.Errorf("defaults: max=%d wav=%v", p.MaxFileSize(), p.NeedsWAVConversion())
	}
}
