package tts

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func frameBytes(pieces ...[]byte) []byte {
	var out []byte
	for _, p := range pieces {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(p)))
		out = append(out, length[:]...)
		out = append(out, p...)
	}
	return out
}

func TestRemoteVoiceNilWithoutBaseURL(t *testing.T) {
	if v := NewRemoteVoice(RemoteConfig{}); v != nil {
		t.Fatal("expected nil voice without a BaseURL")
	}
}

func TestRemoteVoiceSpeak(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("mp3-bytes"))
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL + "/", Scope: "chat"})
	audio, mime, err := v.Speak(context.Background(), "bonjour")
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "mp3-bytes" || mime != "audio/mpeg" {
		t.Fatalf("audio=%q mime=%q", audio, mime)
	}
	if got["text"] != "bonjour" || got["scope"] != "chat" || got["stream"] != false {
		t.Fatalf("request body = %v", got)
	}
}

func TestRemoteVoiceSpeakStreamFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["stream"] != true {
			t.Errorf("stream = %v", req["stream"])
		}
		w.Header().Set("Content-Type", framesContentType)
		w.Write(frameBytes([]byte("one"), []byte("two")))
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL})
	var pieces []string
	mime, err := v.SpeakStream(context.Background(), "text", func(p []byte) error {
		pieces = append(pieces, string(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" {
		t.Fatalf("mime = %q", mime)
	}
	if len(pieces) != 2 || pieces[0] != "one" || pieces[1] != "two" {
		t.Fatalf("pieces = %v", pieces)
	}
}

func TestRemoteVoiceSpeakStreamCacheHitServedWhole(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("whole-cached"))
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL})
	var pieces []string
	mime, err := v.SpeakStream(context.Background(), "text", func(p []byte) error {
		pieces = append(pieces, string(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if mime != "audio/mpeg" || len(pieces) != 1 || pieces[0] != "whole-cached" {
		t.Fatalf("mime=%q pieces=%v", mime, pieces)
	}
}

func TestRemoteVoiceErrorCarriesDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"speech is not configured"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL})
	_, _, err := v.Speak(context.Background(), "text")
	if err == nil || !strings.Contains(err.Error(), "503") || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoteVoicePregenerate(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/pregenerate" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL, Scope: "writing-live"})
	if err := v.Pregenerate(context.Background(), "abc123", "le texte"); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "abc123" || got["scope"] != "writing-live" || got["text"] != "le texte" {
		t.Fatalf("request body = %v", got)
	}
}

func TestRemoteVoicePrimeOpening(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/speak/prime" {
			t.Errorf("path = %q, want /speak/prime — pregenerate would read the whole text", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL, Scope: "chat-message"})
	if err := v.PrimeOpening(context.Background(), "msg-1", "le texte"); err != nil {
		t.Fatal(err)
	}
	// Priming means naming ahead of time what will be listened to: without
	// the id the opening could never be found again.
	if got["id"] != "msg-1" || got["scope"] != "chat-message" || got["text"] != "le texte" {
		t.Fatalf("request body = %v", got)
	}
}

// A reading asked for by name is what finds one primed under that name.
func TestRemoteVoiceSpeakNamedSendsTheID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("audio"))
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL, Scope: "chat-message"})
	audio, mime, err := v.SpeakNamed(context.Background(), "msg-1", "le texte")
	if err != nil {
		t.Fatal(err)
	}
	if got["id"] != "msg-1" || got["scope"] != "chat-message" {
		t.Fatalf("request body = %v, want the reading named", got)
	}
	if string(audio) != "audio" || mime != "audio/mpeg" {
		t.Errorf("got (%q, %q)", audio, mime)
	}
}

func TestRemoteVoicePrimeOpeningReportsAFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"priming needs both speech and a store"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	v := NewRemoteVoice(RemoteConfig{BaseURL: srv.URL, Scope: "chat-message"})
	if err := v.PrimeOpening(context.Background(), "msg-1", "le texte"); err == nil {
		t.Fatal("a 503 must be reported so the caller can log it")
	}
}
