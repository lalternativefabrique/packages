package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/transcribe"
)

func TestGroq_Defaults(t *testing.T) {
	p := NewGroq(GroqConfig{APIKey: "k"})
	if got, want := p.MaxFileSize(), int64(groqDefaultMaxSize); got != want {
		t.Fatalf("MaxFileSize: got %d, want %d", got, want)
	}
	if p.NeedsWAVConversion() {
		t.Fatal("NeedsWAVConversion: got true, want false")
	}
	if p.inner.apiURL != groqDefaultAPIURL || p.inner.modelID != groqDefaultModel {
		t.Fatalf("got url=%q model=%q", p.inner.apiURL, p.inner.modelID)
	}
}

func TestGroq_Transcribe(t *testing.T) {
	var fields map[string]string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		fields, _, _ = readForm(t, r)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "bonjour"})
	}))
	defer srv.Close()

	res, err := NewGroq(GroqConfig{APIURL: srv.URL, APIKey: "secret"}).Transcribe(context.Background(), transcribe.TranscribeRequest{
		FileName: "audio.m4a", File: strings.NewReader("fake audio bytes"), Language: "fr",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "bonjour" {
		t.Fatalf("Text: got %q", res.Text)
	}
	if fields["model"] != groqDefaultModel {
		t.Fatalf("model field: got %q, want %q", fields["model"], groqDefaultModel)
	}
	if auth != "Bearer secret" {
		t.Fatalf("Authorization: got %q", auth)
	}
}
