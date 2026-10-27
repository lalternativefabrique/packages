package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lalternative/packages/go/transcribe"
)

func TestWhisperCpp_Transcribe(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"transcription array": {`{"transcription":[{"text":" hello "},{"text":""},{"text":"world"}]}`, "hello world"},
		"openai shape":        {`{"text":" hello world "}`, "hello world"},
		"raw text":            {"hello world", "hello world"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var fields map[string]string
			var fileField, fileName string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" {
					t.Error("whisper.cpp takes no Authorization header")
				}
				fields, fileField, fileName = readForm(t, r)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p := NewWhisperCpp(WhisperCppConfig{APIURL: srv.URL})
			res, err := p.Transcribe(context.Background(), transcribe.TranscribeRequest{File: strings.NewReader("x"), Language: "en"})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if res.Text != tc.want {
				t.Errorf("Text = %q, want %q", res.Text, tc.want)
			}
			if fileField != "audio_file" || fileName != "audio.wav" {
				t.Errorf("file part = %s/%s", fileField, fileName)
			}
			if fields["response_format"] != "json" || fields["language"] != "en" {
				t.Errorf("fields = %v", fields)
			}
			if !p.NeedsWAVConversion() {
				t.Error("NeedsWAVConversion = false")
			}
		})
	}
}
