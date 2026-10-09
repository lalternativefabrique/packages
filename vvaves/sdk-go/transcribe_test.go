package sdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscribeSendsTheRecordingAsMultipart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transcribe" || r.Method != http.MethodPost {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer app-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		f, h, err := r.FormFile("audio")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		if h.Filename != "memo.webm" || string(b) != "opus" || r.FormValue("language") != "fr" {
			t.Errorf("file=%s body=%q lang=%q", h.Filename, b, r.FormValue("language"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"bonjour"}`))
	}))
	defer srv.Close()
	text, err := New(srv.URL, "app-key").Transcribe(context.Background(), "memo.webm", strings.NewReader("opus"), "fr")
	if err != nil || text != "bonjour" {
		t.Fatalf("Transcribe = %q, %v", text, err)
	}
}

func TestTranscribeCustomerKeyTravelsAsBearer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer vvaves_key_abc" {
			t.Errorf("auth headers: %v", r.Header)
		}
		r.ParseMultipartForm(1 << 20)
		if _, ok := r.MultipartForm.Value["language"]; ok {
			t.Error("empty language sent")
		}
		w.Write([]byte(`{"text":"x"}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "vvaves_key_abc").Transcribe(context.Background(), "a.wav", strings.NewReader("a"), ""); err != nil {
		t.Fatal(err)
	}
}

func TestTranscribeRefusesAnOversizedRecordingBeforeUpload(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer srv.Close()
	_, err := New(srv.URL, "k").Transcribe(context.Background(), "a.wav", bytes.NewReader(make([]byte, MaxAudioBytes+1)), "")
	if !errors.Is(err, ErrAudioTooLarge) || called {
		t.Errorf("err = %v, called = %v", err, called)
	}
}

func TestTranscribeMapsStatuses(t *testing.T) {
	for code, want := range map[int]error{400: ErrBadRequest, 403: ErrUnauthorized, 502: ErrUnavailable, 503: ErrUnavailable} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		if _, err := New(srv.URL, "k").Transcribe(context.Background(), "a", strings.NewReader("a"), ""); !errors.Is(err, want) {
			t.Errorf("%d: %v, want %v", code, err, want)
		}
		srv.Close()
	}
	if _, err := New("", "k").Transcribe(context.Background(), "a", strings.NewReader("a"), ""); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}
