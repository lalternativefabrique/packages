package source

import (
	"net/url"
	"strings"
)

// CanonicalizeURL returns a stable, deduplication-friendly URL for the given
// source type. Two URLs that point at the same underlying content must
// canonicalize to the same string so a stable identity can be derived and
// downstream caches hit instead of duplicating work.
//
// Stays tolerant: any error during canonicalization returns the input
// unchanged so we never block ingestion on URL parsing edge cases.
func CanonicalizeURL(sourceType SourceType, rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return rawURL
	}

	switch sourceType {
	case SourceTypeYouTube:
		if videoID, err := ExtractVideoID(trimmed); err == nil {
			return "https://www.youtube.com/watch?v=" + videoID
		}
		return trimmed
	case SourceTypeWebPage:
		return canonicalizeWebPage(trimmed)
	case SourceTypePodcast, SourceTypeAudioURL:
		return canonicalizeAudio(trimmed)
	default:
		return trimmed
	}
}

// trackerParams lists query parameters known to be tracking/analytics-only
// noise. Stripping them is safe: the same page renders identically with or
// without them. Conservative by design — we only remove well-known trackers,
// never arbitrary query params (some sites use ?id=, ?p=, etc. for content).
var trackerParams = map[string]struct{}{
	// Google Analytics
	"utm_source":   {},
	"utm_medium":   {},
	"utm_campaign": {},
	"utm_content":  {},
	"utm_term":     {},
	"utm_id":       {},
	// Google Ads
	"gclid":  {},
	"gbraid": {},
	"wbraid": {},
	"dclid":  {},
	// Facebook / Meta
	"fbclid": {},
	// Microsoft / LinkedIn
	"msclkid":   {},
	"li_fat_id": {},
	// Mailchimp
	"mc_cid": {},
	"mc_eid": {},
	// HubSpot
	"_hsenc": {},
	"_hsmi":  {},
	// Instagram
	"igshid": {},
	"igsh":   {},
	// Twitter/X
	"s": {},
	"t": {},
	// Generic referrer params
	"ref":     {},
	"ref_src": {},
	"ref_url": {},
	// YouTube share token (in case of webpage that proxies a yt url)
	"si":      {},
	"feature": {},
}

// volatileAudioParams lists query parameters podcast CDNs attach to an
// enclosure URL without changing which file is served: expiring signatures and
// per-listener attribution. They differ on every fetch of the same episode, so
// keeping them would mint a new identity — and a new paid transcription — each
// time the feed is re-read.
var volatileAudioParams = map[string]struct{}{
	"token":             {},
	"expires":           {},
	"expiry":            {},
	"signature":         {},
	"sig":               {},
	"hash":              {},
	"key":               {},
	"awepisodeid":       {},
	"awcollectionid":    {},
	"aw_0_1st.playerid": {},
	"listenerid":        {},
	"_":                 {},
}

// canonicalizeAudio derives a stable identity for an audio enclosure. The file
// is what identifies the episode, so anything that varies per fetch or per
// listener is dropped while genuine content params are preserved.
func canonicalizeAudio(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return rawURL
	}

	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""

	if parsed.RawQuery != "" {
		query := parsed.Query()
		for key := range query {
			lower := strings.ToLower(key)
			if _, isTracker := trackerParams[lower]; isTracker {
				query.Del(key)
				continue
			}
			if _, isVolatile := volatileAudioParams[lower]; isVolatile {
				query.Del(key)
			}
		}
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}

func canonicalizeWebPage(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return rawURL
	}

	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	parsed.RawFragment = ""

	if parsed.RawQuery != "" {
		query := parsed.Query()
		for key := range query {
			if _, isTracker := trackerParams[strings.ToLower(key)]; isTracker {
				query.Del(key)
			}
		}
		parsed.RawQuery = query.Encode()
	}

	return parsed.String()
}
