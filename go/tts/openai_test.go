package tts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// speechServer stands in for /v1/audio/speech. say decides what each request
// answers, keyed by the text it was asked to read.
func speechServer(t *testing.T, say func(input string) (status int, audio []byte, delay time.Duration)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("called %s, want /v1/audio/speech", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		input := inputOf(string(body))
		status, audio, delay := say(input)
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write(audio)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// inputOf digs the spoken text out of the request body without a JSON decode,
// which keeps the double free of the shape it is standing in for.
func inputOf(body string) string {
	const key = `"input":"`
	i := strings.Index(body, key)
	if i < 0 {
		return ""
	}
	rest := body[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		return rest
	}
	return rest[:j]
}

// longText builds text that Split is guaranteed to cut into pieces, each
// paragraph carrying a marker so the audio can be traced back to its source.
func longText(pieces int) (string, []string) {
	var paras []string
	var markers []string
	for i := range pieces {
		marker := fmt.Sprintf("piece%d", i)
		markers = append(markers, marker)
		paras = append(paras, marker+" "+strings.Repeat("mot ", MaxChars/2))
	}
	return strings.Join(paras, "\n\n"), markers
}

func TestSpeakJoinsThePiecesInReadingOrder(t *testing.T) {
	text, markers := longText(4)
	srv := speechServer(t, func(input string) (int, []byte, time.Duration) {
		// Later pieces answer faster, so anything that emitted on completion
		// rather than in order would come out backwards.
		for i, m := range markers {
			if strings.HasPrefix(input, m) {
				return http.StatusOK, []byte("<" + m + ">"), time.Duration(len(markers)-i) * 20 * time.Millisecond
			}
		}
		return http.StatusOK, []byte("<?>"), 0
	})

	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	audio, mime, err := v.Speak(context.Background(), text)
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if mime != "audio/mpeg" {
		t.Errorf("mime is %q, want audio/mpeg", mime)
	}
	// The markers must appear in the order they were written, whatever order
	// the answers came back in.
	at := -1
	for _, m := range markers {
		i := strings.Index(string(audio), "<"+m+">")
		if i < 0 {
			t.Fatalf("%s is missing from the audio: %q", m, audio)
		}
		if i < at {
			t.Fatalf("%s comes out before the piece preceding it: %q", m, audio)
		}
		at = i
	}
}

func TestSpeakStreamEmitsBeforeEverythingIsRead(t *testing.T) {
	text, markers := longText(4)
	last := markers[len(markers)-1]
	srv := speechServer(t, func(input string) (int, []byte, time.Duration) {
		// The final piece takes far longer than the rest, so an implementation
		// that only emitted once everything was in would have nothing to show
		// for most of the run.
		if strings.HasPrefix(input, last) {
			return http.StatusOK, []byte("x"), 700 * time.Millisecond
		}
		return http.StatusOK, []byte("x"), 0
	})

	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	first := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		var once sync.Once
		_, err := v.SpeakStream(context.Background(), text, func([]byte) error {
			once.Do(func() { close(first) })
			return nil
		})
		done <- err
	}()

	select {
	case <-first:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("nothing was emitted while a later piece was still being read")
	}
	if err := <-done; err != nil {
		t.Fatalf("SpeakStream: %v", err)
	}
}

func TestSpeakRefusesAnEmptyAnswer(t *testing.T) {
	srv := speechServer(t, func(string) (int, []byte, time.Duration) {
		return http.StatusOK, nil, 0
	})

	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	// Silence would be indistinguishable from a paragraph that never got read,
	// and it would outlive the request in whatever cache the caller keeps.
	if _, _, err := v.Speak(context.Background(), "quelque chose à lire"); err == nil {
		t.Fatal("a 200 with no bytes must be an error, not silence")
	}
}

func TestSpeakReportsTheFailureRatherThanItsFallout(t *testing.T) {
	text, markers := longText(4)
	srv := speechServer(t, func(input string) (int, []byte, time.Duration) {
		// The last piece fails; the earlier ones are slow enough to still be
		// in flight, so they get cancelled in its wake.
		if strings.HasPrefix(input, markers[3]) {
			return http.StatusTooManyRequests, []byte("slow down"), 0
		}
		return http.StatusOK, []byte("x"), 300 * time.Millisecond
	})

	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	_, _, err := v.Speak(context.Background(), text)
	if err == nil {
		t.Fatal("a failing piece must fail the reading")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error is %q, want the upstream cause rather than a cancellation", err)
	}
}

func TestSpeakStreamStopsWhenNobodyIsListening(t *testing.T) {
	text, _ := longText(6)
	var calls atomic.Int32
	srv := speechServer(t, func(string) (int, []byte, time.Duration) {
		calls.Add(1)
		return http.StatusOK, []byte("x"), 50 * time.Millisecond
	})

	v := NewOpenAIVoice(Config{BaseURL: srv.URL, Concurrency: 1})
	stop := errors.New("listener went away")
	_, err := v.SpeakStream(context.Background(), text, func([]byte) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("SpeakStream returned %v, want the emit error", err)
	}
	// The first piece was already paid for and one more may have been in
	// flight; what must not happen is the whole text being read to nobody.
	total := len(Split(text, MaxChars))
	if got := int(calls.Load()); got >= total {
		t.Errorf("%d of %d pieces were read after the listener left", got, total)
	}
}

func TestSpeakCountsCharactersBeforeReading(t *testing.T) {
	srv := speechServer(t, func(string) (int, []byte, time.Duration) {
		return http.StatusOK, []byte("x"), 0
	})

	var counted int
	v := NewOpenAIVoice(Config{BaseURL: srv.URL, OnUsage: func(chars int) { counted = chars }})
	text := "un texte accentué, compté en runes"
	if _, _, err := v.Speak(context.Background(), text); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if want := len([]rune(text)); counted != want {
		t.Errorf("metered %d characters, want %d", counted, want)
	}
}

func TestSpeakRefusesTextWithNothingToRead(t *testing.T) {
	v := NewOpenAIVoice(Config{BaseURL: "http://127.0.0.1:1"})
	if _, _, err := v.Speak(context.Background(), "   \n\n  "); err == nil {
		t.Fatal("whitespace is not something to read aloud")
	}
}

func TestSpeakSendsTheConfiguredVoice(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization is %q", got)
		}
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	v := NewOpenAIVoice(Config{
		BaseURL: srv.URL,
		APIKey:  "secret",
		Model:   "fr_FR-siwis-medium",
		VoiceID: "fr_FR-upmc-medium",
		Format:  "opus",
	})
	_, mime, err := v.Speak(context.Background(), "bonjour")
	if err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if mime != "audio/ogg" {
		t.Errorf("mime is %q, want audio/ogg for opus", mime)
	}
	for _, want := range []string{`"model":"fr_FR-siwis-medium"`, `"voice":"fr_FR-upmc-medium"`, `"response_format":"opus"`} {
		if !strings.Contains(body, want) {
			t.Errorf("request body lacks %s: %s", want, body)
		}
	}
}

// A self-hosted shim needs no key, and sending an empty Bearer header is how
// some of them start refusing requests.
func TestSpeakSendsNoAuthorizationWithoutAKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("an Authorization header was sent without an API key")
		}
		_, _ = w.Write([]byte("x"))
	}))
	defer srv.Close()

	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	if _, _, err := v.Speak(context.Background(), "bonjour"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
}

// Cutting smaller is what lets Concurrency do anything: a text that fits in a
// single request is read one utterance at a time, however many workers are
// allowed. This is the knob a self-hosted voice needs, since it has no
// per-request limit of its own and is slower per character than a hosted one.
func TestSmallerPiecesAreWhatMakesReadingParallel(t *testing.T) {
	text := strings.Repeat("une phrase de longueur normale. ", 90)

	var atOnce, peak int32
	var mu sync.Mutex
	srv := speechServer(t, func(string) (int, []byte, time.Duration) {
		mu.Lock()
		atOnce++
		if atOnce > peak {
			peak = atOnce
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		atOnce--
		mu.Unlock()
		return http.StatusOK, []byte("x"), 0
	})

	// The hosted limit: the whole text is one request, and nothing runs beside
	// anything else.
	v := NewOpenAIVoice(Config{BaseURL: srv.URL})
	if _, _, err := v.Speak(context.Background(), text); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if peak != 1 {
		t.Errorf("at the default size %d requests overlapped, want 1", peak)
	}

	peak = 0
	small := NewOpenAIVoice(Config{BaseURL: srv.URL, MaxChars: 800})
	if _, _, err := small.Speak(context.Background(), text); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if peak < 2 {
		t.Errorf("cut at 800 chars, only %d request(s) overlapped — nothing was parallel", peak)
	}
}
