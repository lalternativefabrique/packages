// Package source is the identity of a source both Synthiz and L'Alter key
// their synthesis caches on: its type and its canonical URL. Two URLs of the
// same content must canonicalize alike, or the cache pays twice.
package source

import "strings"

// SourceType represents the type of a content source.
type SourceType string

const (
	SourceTypeYouTube  SourceType = "youtube"
	SourceTypePodcast  SourceType = "podcast"
	SourceTypePDF      SourceType = "pdf"
	SourceTypeRSS      SourceType = "rss"
	SourceTypeWebPage  SourceType = "webpage"
	SourceTypeAudioURL SourceType = "audio_url"
)

// IsValid returns true if the source type is a known value.
func (st SourceType) IsValid() bool {
	switch st {
	case SourceTypeYouTube, SourceTypePodcast, SourceTypePDF, SourceTypeRSS, SourceTypeWebPage, SourceTypeAudioURL:
		return true
	default:
		return false
	}
}

// String returns the string representation.
func (st SourceType) String() string {
	return string(st)
}

// NormalizeSourceType maps user-facing aliases to canonical SourceType values.
// Returns ("", false) when the input is empty or does not match any known
// type or alias. Callers that need auto-detect must handle empty input
// before calling this helper.
func NormalizeSourceType(raw string) (SourceType, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	switch trimmed {
	case "youtube":
		return SourceTypeYouTube, true
	case "podcast":
		return SourceTypePodcast, true
	case "pdf":
		return SourceTypePDF, true
	case "rss":
		return SourceTypeRSS, true
	case "webpage", "article", "web":
		return SourceTypeWebPage, true
	case "audio_url", "audio":
		return SourceTypeAudioURL, true
	default:
		return "", false
	}
}
