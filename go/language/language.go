// Package language reads and normalizes the language of a text, as an ISO
// 639-1 code. It is the one implementation every product keys its content,
// its doctrine and its translations by.
package language

import (
	"strings"

	"github.com/abadojack/whatlanggo"
)

// Normalize reduces a language tag to its lowercase primary subtag ("en-US"
// → "en", " FR " → "fr"), or "" when it is not a two-letter code.
func Normalize(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_"); i > 0 {
		tag = tag[:i]
	}
	if len(tag) != 2 {
		return ""
	}
	return tag
}

// Or returns language, or fallback when it is unknown.
func Or(language, fallback string) string {
	if language == "" {
		return fallback
	}
	return language
}

// minDetectableRunes is the text below which a statistical guess is noise: a
// three-word title reads as half the languages of Europe.
const minDetectableRunes = 40

// Detect reads which language texts are written in, as an ISO 639-1 code, or
// "" when they are too short or too mixed to tell.
func Detect(texts ...string) string {
	joined := strings.TrimSpace(strings.Join(texts, "\n"))
	if len([]rune(joined)) < minDetectableRunes {
		return ""
	}
	info := whatlanggo.Detect(joined)
	if !info.IsReliable() {
		return ""
	}
	return Normalize(info.Lang.Iso6391())
}
