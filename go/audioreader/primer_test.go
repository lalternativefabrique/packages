package audioreader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

const (
	testScope        = "test-audio"
	testOpeningChars = 800
)

// readerWith builds a Reader over a store, for the parts of serving primed
// audio that need no voice.
func readerWith(store *primedStore) *Reader {
	return &Reader{storage: store, openingSize: testOpeningChars, log: testLogger()}
}

type primedStore struct {
	saved map[string][]byte
	fail  bool
}

func newPrimedStore() *primedStore { return &primedStore{saved: map[string][]byte{}} }

func (p *primedStore) Upload(_ context.Context, key string, body io.Reader, _ string) error {
	if p.fail {
		return errors.New("bucket unavailable")
	}
	b, _ := io.ReadAll(body)
	p.saved[key] = b
	return nil
}

func (p *primedStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	b, ok := p.saved[key]
	if !ok {
		return nil, ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

type recordingVoice struct {
	spoke []string
	audio []byte
	err   error
}

func (v *recordingVoice) Synthesize(_ context.Context, text, _ string) ([]byte, string, error) {
	v.spoke = append(v.spoke, text)
	if v.err != nil {
		return nil, "", v.err
	}
	return v.audio, "audio/mpeg", nil
}

func (v *recordingVoice) SynthesizeStream(ctx context.Context, text, userID string, emit func([]byte) error) (string, error) {
	audio, mime, err := v.Synthesize(ctx, text, userID)
	if err != nil {
		return "", err
	}
	return mime, emit(audio)
}

// longSynthesis is text the reader would cut into several pieces.
func longSynthesis() string {
	var parts []string
	for range 6 {
		parts = append(parts, strings.Repeat("une phrase de longueur normale. ", 23))
	}
	return strings.Join(parts, "\n\n")
}

func TestPrimingReadsOnlyTheOpening(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{audio: []byte("opening bytes")}
	primer := NewPrimer(voice, store, testOpeningChars, nil)

	text := longSynthesis()
	if err := primer.PrimeOpening(context.Background(), testScope, "syn-1", text); err != nil {
		t.Fatalf("PrimeOpening: %v", err)
	}

	if len(voice.spoke) != 1 {
		t.Fatalf("read %d times, want one — the rest is what nobody is waiting for", len(voice.spoke))
	}
	// Most of a synthesis is never listened to; reading it whole up front
	// would spend the voice on pages nobody opens.
	if spoken := len([]rune(voice.spoke[0])); spoken > testOpeningChars+100 {
		t.Errorf("read %d runes up front, want roughly one piece", spoken)
	}
	if len(store.saved) != 1 {
		t.Errorf("stored %d objects, want the opening", len(store.saved))
	}
}

// The opening lives under its own key: handed back under the whole reading's
// key, a listener would hear the first paragraph and then silence, with
// nothing to say the rest exists.
func TestTheOpeningIsKeptApartFromAWholeReading(t *testing.T) {
	text := longSynthesis()
	if OpeningKey(testScope, "syn-1", text) == CacheKey(testScope, "syn-1", text) {
		t.Error("the opening and a whole reading must not share a key")
	}
}

func TestServingPrimedAudioLeavesExactlyTheRestToRead(t *testing.T) {
	store := newPrimedStore()
	voice := &recordingVoice{audio: []byte("opening bytes")}
	primer := NewPrimer(voice, store, testOpeningChars, nil)

	text := longSynthesis()
	if err := primer.PrimeOpening(context.Background(), testScope, "syn-1", text); err != nil {
		t.Fatalf("PrimeOpening: %v", err)
	}

	audio, rest := readerWith(store).openingFor(context.Background(), Request{Scope: testScope, ID: "syn-1", Text: text})

	if string(audio) != "opening bytes" {
		t.Fatalf("served %q, want the primed opening", audio)
	}
	// Every word of the synthesis is either in the opening that was read or in
	// what is left — none read twice, none skipped.
	spokenOpening := voice.spoke[0]
	if got, want := len(strings.Fields(spokenOpening))+len(strings.Fields(rest)), len(strings.Fields(text)); got != want {
		t.Errorf("opening and rest hold %d words, the synthesis has %d", got, want)
	}
	if strings.HasPrefix(rest, strings.Fields(spokenOpening)[0]+" ") && strings.HasPrefix(text, rest) {
		t.Error("the rest starts where the opening did — it would be read twice")
	}
}

func TestWithoutAPrimedOpeningTheWholeTextIsRead(t *testing.T) {
	text := longSynthesis()

	audio, rest := readerWith(newPrimedStore()).openingFor(context.Background(), Request{Scope: testScope, ID: "syn-never-primed", Text: text})

	if audio != nil {
		t.Error("nothing was primed, so nothing should be served")
	}
	if rest != text {
		t.Error("the whole text must be read when its opening is missing")
	}
}

// A synthesis short enough to be one piece gains nothing from being halved,
// and splitting it would mean two round trips where one did.
func TestAShortSynthesisIsNotSplit(t *testing.T) {
	short := "Une synthèse courte, lue d'un seul tenant."

	audio, rest := readerWith(newPrimedStore()).openingFor(context.Background(), Request{Scope: testScope, ID: "syn-2", Text: short})
	if audio != nil || rest != short {
		t.Error("a one-piece synthesis must be read as it is")
	}
}

func TestPrimingIsOffWithoutAVoiceOrSomewhereToKeepIt(t *testing.T) {
	if NewPrimer(nil, newPrimedStore(), testOpeningChars, nil) != nil {
		t.Error("no voice means no priming")
	}
	if NewPrimer(&recordingVoice{}, nil, testOpeningChars, nil) != nil {
		t.Error("nowhere to keep the audio means no priming")
	}
}

func TestAnEmptyReadingIsNotStored(t *testing.T) {
	store := newPrimedStore()
	primer := NewPrimer(&recordingVoice{audio: nil}, store, testOpeningChars, nil)

	// Silence stored as an opening would play as a gap before every listen,
	// with nothing to say the reading failed.
	if err := primer.PrimeOpening(context.Background(), testScope, "syn-3", longSynthesis()); err == nil {
		t.Error("a reading with no audio must be an error, not an empty object")
	}
	if len(store.saved) != 0 {
		t.Error("nothing should have been stored")
	}
}
