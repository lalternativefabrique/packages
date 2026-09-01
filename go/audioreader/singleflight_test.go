package audioreader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingVoice holds every synthesis until it is released, so a test can put
// several listeners on the same text at once and see how many readings that
// actually starts.
type blockingVoice struct {
	release chan struct{}
	calls   atomic.Int32
	audio   []byte
	err     error
}

func newBlockingVoice(audio []byte) *blockingVoice {
	return &blockingVoice{release: make(chan struct{}), audio: audio}
}

func (v *blockingVoice) Synthesize(_ context.Context, _, _ string) ([]byte, string, error) {
	v.calls.Add(1)
	<-v.release
	if v.err != nil {
		return nil, "", v.err
	}
	return v.audio, "audio/mpeg", nil
}

func (v *blockingVoice) SynthesizeStream(_ context.Context, _, _ string, emit func([]byte) error) (string, error) {
	v.calls.Add(1)
	<-v.release
	if v.err != nil {
		return "", v.err
	}
	return "audio/mpeg", emit(v.audio)
}

// waitForCalls gives the leader time to reach the voice before the test
// releases it, so "one call" is a real observation rather than a race the
// waiters happened to win.
func waitForCalls(t *testing.T, v *blockingVoice, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for v.calls.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("voice was called %d times, waited for %d", v.calls.Load(), want)
		}
		time.Sleep(time.Millisecond)
	}
}

// Several listeners pressing play on the same text at once must pay for one
// reading, not one each. The cache cannot do this on its own: it is filled
// when a reading ends, so for the tens of seconds one takes it is empty and
// every arrival would start another.
func TestConcurrentListenersShareOneReading(t *testing.T) {
	voice := newBlockingVoice([]byte("the reading"))
	r := &Reader{tts: voice, storage: newPrimedStore(), openingSize: testOpeningChars, log: testLogger()}
	ar := Request{Scope: testScope, ID: "sf-1", Text: "un texte partage", BillTo: "someone"}

	const listeners = 5
	var wg sync.WaitGroup
	bodies := make([]string, listeners)
	codes := make([]int, listeners)
	for i := range listeners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.Serve(rec, httptest.NewRequest(http.MethodGet, "/audio", nil), ar, nil)
			bodies[i] = rec.Body.String()
			codes[i] = rec.Code
		}()
	}

	waitForCalls(t, voice, 1)
	close(voice.release)
	wg.Wait()

	if got := voice.calls.Load(); got != 1 {
		t.Errorf("the voice read %d times, want 1: the listeners must share one reading", got)
	}
	for i := range listeners {
		if codes[i] != http.StatusOK {
			t.Errorf("listener %d got %d, want 200", i, codes[i])
		}
		if bodies[i] != "the reading" {
			t.Errorf("listener %d got %q, want the shared reading", i, bodies[i])
		}
	}
}

// A reading that failed must fail for everyone waiting on it, once. The point
// is that a voice which is down is called once per reading rather than once
// per listener.
func TestASharedReadingSharesItsFailure(t *testing.T) {
	voice := newBlockingVoice(nil)
	voice.err = errors.New("tts unreachable")
	r := &Reader{tts: voice, storage: newPrimedStore(), openingSize: testOpeningChars, log: testLogger()}
	ar := Request{Scope: testScope, ID: "sf-2", Text: "un texte", BillTo: "someone"}

	var wg sync.WaitGroup
	codes := make([]int, 3)
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			r.Serve(rec, httptest.NewRequest(http.MethodGet, "/audio", nil), ar, nil)
			codes[i] = rec.Code
		}()
	}

	waitForCalls(t, voice, 1)
	close(voice.release)
	wg.Wait()

	if got := voice.calls.Load(); got != 1 {
		t.Errorf("the voice was called %d times, want 1", got)
	}
	for i, c := range codes {
		if c != http.StatusBadGateway {
			t.Errorf("listener %d got %d, want 502", i, c)
		}
	}
}

// Different texts are different readings: collapsing them would serve one
// listener the audio of someone else's words.
func TestDifferentTextsDoNotShareAReading(t *testing.T) {
	voice := newBlockingVoice([]byte("audio"))
	close(voice.release)
	r := &Reader{tts: voice, storage: newPrimedStore(), openingSize: testOpeningChars, log: testLogger()}

	for _, text := range []string{"premier texte", "second texte"} {
		rec := httptest.NewRecorder()
		r.Serve(rec, httptest.NewRequest(http.MethodGet, "/audio", nil),
			Request{Scope: testScope, ID: "sf-3", Text: text, BillTo: "someone"}, nil)
	}

	if got := voice.calls.Load(); got != 2 {
		t.Errorf("the voice read %d times, want 2: two texts are two readings", got)
	}
}

// Once a reading is finished the next listener starts a fresh one rather than
// joining the completed one — by then the cache holds it, so in practice they
// are served from there.
func TestAFinishedReadingIsNotJoined(t *testing.T) {
	flight := newInflight()
	var calls atomic.Int32
	read := func() ([]byte, string, error) {
		calls.Add(1)
		return []byte("audio"), "audio/mpeg", nil
	}

	for range 3 {
		if _, _, _, shared := flight.do("same-key", read); shared {
			t.Error("a sequential call must not report itself as shared")
		}
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("read %d times, want 3: sequential calls each start their own", got)
	}
}
