package source

import "testing"

func TestNormalizeSourceType(t *testing.T) {
	cases := []struct {
		in     string
		want   SourceType
		wantOk bool
	}{
		{"youtube", SourceTypeYouTube, true},
		{"YouTube", SourceTypeYouTube, true},
		{" youtube ", SourceTypeYouTube, true},
		{"webpage", SourceTypeWebPage, true},
		{"article", SourceTypeWebPage, true},
		{"web", SourceTypeWebPage, true},
		{"pdf", SourceTypePDF, true},
		{"podcast", SourceTypePodcast, true},
		{"audio_url", SourceTypeAudioURL, true},
		{"audio", SourceTypeAudioURL, true},
		{"rss", SourceTypeRSS, true},
		{"", "", false},
		{"unknown", "", false},
	}
	for _, c := range cases {
		got, ok := NormalizeSourceType(c.in)
		if got != c.want || ok != c.wantOk {
			t.Errorf("NormalizeSourceType(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOk)
		}
	}
}

func TestExtractVideoID(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://www.youtube.com/watch?v=s76FlThKtBQ", "s76FlThKtBQ", false},
		{"https://youtu.be/s76FlThKtBQ?si=abc", "s76FlThKtBQ", false},
		{"https://www.youtube.com/shorts/abcDEF12345", "abcDEF12345", false},
		{"https://www.youtube.com/live/xyzABCDE000", "xyzABCDE000", false},
		{"s76FlThKtBQ", "s76FlThKtBQ", false},
		{"", "", true},
		{"not a url", "", true},
	}
	for _, c := range cases {
		got, err := ExtractVideoID(c.in)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("ExtractVideoID(%q) = (%q, %v), want (%q, err=%v)", c.in, got, err, c.want, c.wantErr)
		}
	}
}

func TestIsLiveURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://www.youtube.com/live/xyzABCDE000", true},
		{"https://youtube.com/live/abc", true},
		{"https://www.youtube.com/watch?v=abc", false},
		{"https://example.com/live/abc", false},
	}
	for _, c := range cases {
		if got := IsLiveURL(c.in); got != c.want {
			t.Errorf("IsLiveURL(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsDirectAudioURL(t *testing.T) {
	tests := []struct {
		url    string
		expect bool
	}{
		{"https://cdn.example.com/episode.mp3", true},
		{"https://cdn.example.com/episode.m4a", true},
		{"https://cdn.example.com/episode.MP3", true},
		{"https://cdn.example.com/episode.mp3?token=abc123", true},
		{"https://cdn.example.com/episode.mp3#t=10", true},
		{"https://podcasts.apple.com/episode/123", false},
		{"https://example.com/podcast/episode-42", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsDirectAudioURL(tt.url); got != tt.expect {
			t.Errorf("IsDirectAudioURL(%q) = %v, want %v", tt.url, got, tt.expect)
		}
	}
}

