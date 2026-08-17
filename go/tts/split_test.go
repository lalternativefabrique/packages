package tts

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitForTTS_ShortText(t *testing.T) {
	got := Split("hello world", 4096)
	if len(got) != 1 || got[0] != "hello world" {
		t.Errorf("got %v, want single chunk", got)
	}
}

func TestSplitForTTS_Empty(t *testing.T) {
	if got := Split("", 4096); len(got) != 0 {
		t.Errorf("expected no chunks, got %v", got)
	}
}

func TestSplitForTTS_RespectsMax(t *testing.T) {
	// Build a text > 4096 chars made of paragraphs of varying length.
	var b strings.Builder
	for i := 0; i < 60; i++ {
		// ~100-char sentence; 60 of them = ~6000 chars.
		b.WriteString("Ceci est une phrase de test pour vérifier le découpage du texte par le splitter TTS de OpenAI.")
		b.WriteString(" ")
		if i%5 == 4 {
			b.WriteString("\n\n")
		}
	}
	text := b.String()
	if utf8.RuneCountInString(text) <= 4096 {
		t.Fatalf("test text not long enough: %d", utf8.RuneCountInString(text))
	}

	chunks := Split(text, 4096)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 4096 {
			t.Errorf("chunk %d has %d runes, exceeds 4096", i, n)
		}
		if c == "" {
			t.Errorf("chunk %d is empty", i)
		}
	}
}

func TestSplitForTTS_PrefersSentenceBoundary(t *testing.T) {
	// Two sentences that together exceed maxChars; each fits alone.
	a := strings.Repeat("a", 3000) + "."
	b := strings.Repeat("b", 3000) + "."
	chunks := Split(a+" "+b, 4096)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], ".") {
		t.Errorf("chunk 0 should end on sentence boundary: %q...", chunks[0][:min(40, len(chunks[0]))])
	}
}

func TestSplitForTTS_HardSplitsHugeWord(t *testing.T) {
	huge := strings.Repeat("x", 10000)
	chunks := Split(huge, 4096)
	if len(chunks) < 3 {
		t.Fatalf("expected >=3 chunks for 10000-char word, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 4096 {
			t.Errorf("chunk %d exceeds limit: %d", i, n)
		}
	}
}

func TestSplitForTTS_MultiByteRunes(t *testing.T) {
	// '€' is 3 bytes but 1 rune. Build > 4096 runes.
	text := strings.Repeat("é ", 3000) // 6000 runes (incl. spaces)
	chunks := Split(text, 4096)
	if len(chunks) < 2 {
		t.Fatalf("expected >=2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 4096 {
			t.Errorf("chunk %d has %d runes, exceeds 4096", i, n)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
