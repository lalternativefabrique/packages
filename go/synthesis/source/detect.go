package source

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

)

// IsLiveURL returns true if the URL is a YouTube Live stream URL.
func IsLiveURL(input string) bool {
	parsed, err := url.Parse(strings.TrimSpace(input))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	return strings.HasSuffix(host, "youtube.com") && strings.HasPrefix(parsed.Path, "/live/")
}

// ExtractVideoID extracts the video ID from a YouTube URL or bare ID.
func ExtractVideoID(input string) (string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", fmt.Errorf("%w: url is required", ErrInvalidURL)
	}

	parsed, err := url.Parse(trimmed)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		host := strings.ToLower(parsed.Host)
		host = strings.TrimPrefix(host, "www.")

		if host == "youtu.be" {
			id := strings.Trim(parsed.Path, "/")
			if id != "" {
				return id, nil
			}
		}

		if strings.HasSuffix(host, "youtube.com") {
			if v := parsed.Query().Get("v"); v != "" {
				return v, nil
			}
			for _, prefix := range []string{"/shorts/", "/live/"} {
				if strings.HasPrefix(parsed.Path, prefix) {
					id := strings.TrimPrefix(parsed.Path, prefix)
					id = strings.Trim(id, "/")
					if id != "" {
						return id, nil
					}
				}
			}
		}
	}

	// Check if it's a bare video ID
	bareIDPattern := regexp.MustCompile(`^[A-Za-z0-9_-]{6,}$`)
	if bareIDPattern.MatchString(trimmed) {
		return trimmed, nil
	}

	return "", fmt.Errorf("%w: invalid YouTube URL or video id", ErrInvalidURL)
}

// IsYouTubeURL reports whether the URL host is a YouTube domain.
func IsYouTubeURL(input string) bool {
	parsed, err := url.Parse(strings.TrimSpace(input))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimPrefix(parsed.Host, "www."))
	return host == "youtu.be" || strings.HasSuffix(host, "youtube.com")
}

var audioExtensions = map[string]bool{
	".mp3":  true,
	".m4a":  true,
	".wav":  true,
	".ogg":  true,
	".opus": true,
	".aac":  true,
	".flac": true,
	".wma":  true,
}

// IsDirectAudioURL checks if the URL points directly to an audio file by its
// path extension (query/fragment stripped).
func IsDirectAudioURL(sourceURL string) bool {
	urlPath := sourceURL
	if idx := strings.IndexAny(urlPath, "?#"); idx != -1 {
		urlPath = urlPath[:idx]
	}
	ext := strings.ToLower(path.Ext(urlPath))
	return audioExtensions[ext]
}
