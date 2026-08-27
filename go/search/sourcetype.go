package search

import "net/url"

func detectSourceType(rawURL string, category Category) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "web"
	}
	host := parsed.Hostname()

	switch {
	case host == "youtube.com" || host == "www.youtube.com" || host == "youtu.be" || host == "m.youtube.com":
		return "youtube"
	case host == "podcasts.apple.com" || host == "open.spotify.com" || host == "soundcloud.com":
		return "podcast"
	case category == "videos":
		return "youtube"
	default:
		return "web"
	}
}
