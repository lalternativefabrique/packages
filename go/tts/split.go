package tts

import (
	"strings"
	"unicode/utf8"
)

// MaxChars is how much text /v1/audio/speech accepts in one request. Longer
// text has to be cut, and where it is cut is audible.
const MaxChars = 4096

// Split cuts text into pieces of at most maxChars runes, preferring the
// boundaries a reader would pause at anyway: paragraphs, then sentences, then
// words. Only a single word longer than a whole request is broken mid-word.
//
// Where a cut lands is not cosmetic. Each piece is synthesized on its own, so
// the model reads it as a complete utterance — cutting mid-sentence gives the
// first half a falling final intonation and the second half a fresh opening
// one, and the seam is plainly audible.
func Split(text string, maxChars int) []string {
	if maxChars <= 0 {
		return []string{text}
	}
	if utf8.RuneCountInString(text) <= maxChars {
		if text == "" {
			return nil
		}
		return []string{text}
	}

	var chunks []string
	for _, para := range splitKeepDelim(text, "\n\n") {
		if utf8.RuneCountInString(para) <= maxChars {
			chunks = appendPiece(chunks, para, maxChars)
			continue
		}
		for _, sent := range splitSentences(para) {
			if utf8.RuneCountInString(sent) <= maxChars {
				chunks = appendPiece(chunks, sent, maxChars)
				continue
			}
			for _, word := range splitWords(sent, maxChars) {
				chunks = appendPiece(chunks, word, maxChars)
			}
		}
	}
	return chunks
}

// appendPiece appends piece to the last chunk if it fits, otherwise starts a
// new chunk. piece is assumed to fit within maxChars by itself.
func appendPiece(chunks []string, piece string, maxChars int) []string {
	piece = strings.TrimRight(piece, " ")
	if piece == "" {
		return chunks
	}
	if len(chunks) == 0 {
		return append(chunks, piece)
	}
	last := chunks[len(chunks)-1]
	// Re-attach with a space if the previous chunk doesn't already end on
	// whitespace or a newline-bearing boundary.
	sep := ""
	if !strings.HasSuffix(last, "\n") && !strings.HasSuffix(last, " ") {
		sep = " "
	}
	combined := last + sep + piece
	if utf8.RuneCountInString(combined) <= maxChars {
		chunks[len(chunks)-1] = combined
		return chunks
	}
	return append(chunks, piece)
}

// splitKeepDelim splits s on delim, keeping the delimiter attached to the
// preceding part. Empty parts are skipped.
func splitKeepDelim(s, delim string) []string {
	if delim == "" || !strings.Contains(s, delim) {
		if s == "" {
			return nil
		}
		return []string{s}
	}
	var out []string
	for {
		i := strings.Index(s, delim)
		if i < 0 {
			if s != "" {
				out = append(out, s)
			}
			return out
		}
		out = append(out, s[:i+len(delim)])
		s = s[i+len(delim):]
	}
}

// splitSentences splits a paragraph into sentences on '.', '!', '?', '…'
// followed by whitespace. Punctuation is kept with the preceding sentence.
func splitSentences(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var out []string
	start := 0
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r != '.' && r != '!' && r != '?' && r != '…' {
			continue
		}
		// Consume any trailing closing quotes/brackets attached to the punctuation.
		j := i + 1
		for j < len(runes) && (runes[j] == '"' || runes[j] == '\'' || runes[j] == ')' || runes[j] == ']' || runes[j] == '»') {
			j++
		}
		if j >= len(runes) || runes[j] == ' ' || runes[j] == '\n' || runes[j] == '\t' {
			out = append(out, string(runes[start:j]))
			// Skip the whitespace separator.
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\n' || runes[j] == '\t') {
				j++
			}
			start = j
			i = j - 1
		}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}

// splitWords splits s into chunks of at most maxChars runes on word
// boundaries. If a single word exceeds maxChars, it is hard-split on rune
// boundaries.
func splitWords(s string, maxChars int) []string {
	words := strings.Fields(s)
	var out []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curLen = 0
		}
	}
	for _, w := range words {
		wLen := utf8.RuneCountInString(w)
		if wLen > maxChars {
			flush()
			for _, hard := range hardSplit(w, maxChars) {
				out = append(out, hard)
			}
			continue
		}
		extra := wLen
		if curLen > 0 {
			extra++ // space
		}
		if curLen+extra > maxChars {
			flush()
			cur.WriteString(w)
			curLen = wLen
			continue
		}
		if curLen > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
		curLen += extra
	}
	flush()
	return out
}

func hardSplit(s string, maxChars int) []string {
	var out []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += maxChars {
		end := i + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
