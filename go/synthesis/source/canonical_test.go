package source

import "testing"

func TestCanonicalizeURL_YouTube(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"youtu.be with si token", "https://youtu.be/s76FlThKtBQ?si=3GvOTqy1ZLr4AkTV", "https://www.youtube.com/watch?v=s76FlThKtBQ"},
		{"watch with extra params", "https://www.youtube.com/watch?v=GD7o7wlxnwQ&time_continue=1", "https://www.youtube.com/watch?v=GD7o7wlxnwQ"},
		{"shorts", "https://www.youtube.com/shorts/abcDEF12345", "https://www.youtube.com/watch?v=abcDEF12345"},
		{"live", "https://www.youtube.com/live/xyzABCDE000", "https://www.youtube.com/watch?v=xyzABCDE000"},
		{"already canonical", "https://www.youtube.com/watch?v=s76FlThKtBQ", "https://www.youtube.com/watch?v=s76FlThKtBQ"},
		{"invalid passes through", "not a url at all", "not a url at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalizeURL(SourceTypeYouTube, tc.in); got != tc.want {
				t.Errorf("CanonicalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeURL_WebPage_StripsTrackers(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"utm params", "https://lemonde.fr/article/123?utm_source=twitter&utm_medium=social", "https://lemonde.fr/article/123"},
		{"fbclid", "https://example.com/page?fbclid=IwAR0xY", "https://example.com/page"},
		{"mixed trackers and content", "https://example.com/article?id=42&utm_source=newsletter&fbclid=xxx", "https://example.com/article?id=42"},
		{"fragment removed", "https://example.com/page#section-2", "https://example.com/page"},
		{"host lowercased", "https://Example.COM/Path", "https://example.com/Path"},
		{"non-tracker params preserved", "https://example.com/search?q=hello&page=2", "https://example.com/search?page=2&q=hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalizeURL(SourceTypeWebPage, tc.in); got != tc.want {
				t.Errorf("CanonicalizeURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalizeURL_OtherTypesPassThrough(t *testing.T) {
	url := "https://example.com/document.pdf?id=42"
	for _, st := range []SourceType{SourceTypePDF, SourceTypeRSS} {
		if got := CanonicalizeURL(st, url); got != url {
			t.Errorf("type %s: CanonicalizeURL(%q) = %q, want unchanged", st, url, got)
		}
	}
}

func TestCanonicalizeURL_Audio_StripsVolatileParams(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"cdn signature params dropped",
			"https://media.radiofrance-podcast.net/ep.mp3?token=abc&expires=1754812800",
			"https://media.radiofrance-podcast.net/ep.mp3",
		},
		{
			"listener attribution prefix dropped",
			"https://media.radiofrance-podcast.net/ep.mp3?_=1&awCollectionId=42",
			"https://media.radiofrance-podcast.net/ep.mp3",
		},
		{
			"trackers dropped",
			"https://media.radiofrance-podcast.net/ep.mp3?utm_source=rss&fbclid=xxx",
			"https://media.radiofrance-podcast.net/ep.mp3",
		},
		{
			"host lowercased and fragment removed",
			"https://Media.RadioFrance-Podcast.NET/ep.mp3#t=30",
			"https://media.radiofrance-podcast.net/ep.mp3",
		},
		{
			"content-bearing params preserved",
			"https://cdn.example.com/stream?episode=1234",
			"https://cdn.example.com/stream?episode=1234",
		},
		{
			"invalid passes through",
			"not a url at all",
			"not a url at all",
		},
	}
	for _, tc := range cases {
		for _, st := range []SourceType{SourceTypePodcast, SourceTypeAudioURL} {
			t.Run(string(st)+"/"+tc.name, func(t *testing.T) {
				if got := CanonicalizeURL(st, tc.in); got != tc.want {
					t.Errorf("CanonicalizeURL(%s, %q) = %q, want %q", st, tc.in, got, tc.want)
				}
			})
		}
	}
}

func TestAudioURLVariantsCanonicalizeAlike(t *testing.T) {
	base := "https://media.radiofrance-podcast.net/ep.mp3"
	signed := base + "?token=abc&expires=1754812800"

	if got, want := CanonicalizeURL(SourceTypeAudioURL, signed), CanonicalizeURL(SourceTypeAudioURL, base); got != want {
		t.Errorf("signed and unsigned enclosure URLs must share one identity: %q != %q", got, want)
	}
}
