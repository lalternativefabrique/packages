package fileguard

import (
	"errors"
	"testing"
)

// The two products declare different policies on purpose: Techtuel handles no
// documents, core rations anonymous visitors because every non-YouTube type
// routes through Whisper. The guard has to express both rather than pick one.
var (
	corePolicy = Policy{
		Allowed:          []SourceType{SourceYouTube, SourceWebPage, SourcePDF, SourcePodcast, SourceAudioURL, SourceRSS},
		AllowedAnonymous: []SourceType{SourceYouTube},
	}
	techtuelPolicy = Policy{
		Allowed:          []SourceType{SourceYouTube, SourcePodcast, SourceAudioURL},
		AllowedAnonymous: []SourceType{SourceYouTube, SourcePodcast, SourceAudioURL},
	}
)

func TestAdmit_TypeMustBeInThePolicy(t *testing.T) {
	g := NewGuard(techtuelPolicy)

	// Techtuel transcribes audio and video; a PDF has no place in it and never
	// will, so the guard refuses it rather than letting a parser decide later.
	if _, err := g.Admit(Request{URL: "https://example.com/a.pdf", Type: SourcePDF}); !errors.Is(err, ErrTypeNotAllowed) {
		t.Errorf("pdf on techtuel: error = %v, want ErrTypeNotAllowed", err)
	}
	if _, err := g.Admit(Request{URL: "https://example.com/a.mp3", Type: SourceAudioURL}); err != nil {
		t.Errorf("audio_url on techtuel: %v", err)
	}

	// The same PDF is core's business.
	if _, err := NewGuard(corePolicy).Admit(Request{URL: "https://example.com/a.pdf", Type: SourcePDF}); err != nil {
		t.Errorf("pdf on core: %v", err)
	}
}

func TestAdmit_AnonymousPolicyIsPerProduct(t *testing.T) {
	core := NewGuard(corePolicy)

	if _, err := core.Admit(Request{URL: "https://youtube.com/watch?v=x", Type: SourceYouTube, Anonymous: true}); err != nil {
		t.Errorf("anon youtube on core: %v", err)
	}
	// A three-hour podcast costs orders of magnitude more than a captioned
	// video, and the trial identity is a renewable cookie.
	if _, err := core.Admit(Request{URL: "https://example.com/ep", Type: SourcePodcast, Anonymous: true}); !errors.Is(err, ErrAnonNotAllowed) {
		t.Errorf("anon podcast on core: error = %v, want ErrAnonNotAllowed", err)
	}

	// Techtuel's trial is metered in credits instead, so the same request passes.
	if _, err := NewGuard(techtuelPolicy).Admit(Request{URL: "https://example.com/ep", Type: SourcePodcast, Anonymous: true}); err != nil {
		t.Errorf("anon podcast on techtuel: %v", err)
	}
}

// An anonymous list may not widen the allowed list: a type absent from Allowed
// stays refused even if someone adds it to AllowedAnonymous.
func TestNewGuard_AnonymousCannotWidenAllowed(t *testing.T) {
	g := NewGuard(Policy{
		Allowed:          []SourceType{SourceYouTube},
		AllowedAnonymous: []SourceType{SourceYouTube, SourcePDF},
	})
	if _, err := g.Admit(Request{URL: "https://example.com/a.pdf", Type: SourcePDF, Anonymous: true}); !errors.Is(err, ErrTypeNotAllowed) {
		t.Errorf("error = %v, want ErrTypeNotAllowed", err)
	}
}

// An empty or unknown type is a rejection. Falling back to URL-shape detection
// would classify anything unrecognised as a podcast and hand it to the decoder.
func TestAdmit_RejectsUnknownAndEmptyType(t *testing.T) {
	g := NewGuard(corePolicy)
	for _, raw := range []SourceType{"", "unknown", "AUDIO_URL ", "video"} {
		_, err := g.Admit(Request{URL: "https://example.com/x", Type: raw})
		if raw == "AUDIO_URL " {
			// Aliases are case- and space-insensitive, so this one is admitted.
			if err != nil {
				t.Errorf("Admit(%q) = %v, want admitted", raw, err)
			}
			continue
		}
		if !errors.Is(err, ErrTypeNotAllowed) {
			t.Errorf("Admit(%q) error = %v, want ErrTypeNotAllowed", raw, err)
		}
	}
}

func TestAdmit_RejectsUnsafeURL(t *testing.T) {
	g := NewGuard(corePolicy)
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1/x",
		"http://10.0.0.5/feed.xml",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"",
	} {
		if _, err := g.Admit(Request{URL: u, Type: SourceWebPage}); !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("Admit(%q) error = %v, want ErrUnsafeURL", u, err)
		}
	}
}

// The order matters for cost: a type this product never handles is refused
// before anything resolves DNS.
func TestAdmit_TypeIsCheckedBeforeTheURL(t *testing.T) {
	_, err := NewGuard(techtuelPolicy).Admit(Request{URL: "http://127.0.0.1/x", Type: SourcePDF})
	if !errors.Is(err, ErrTypeNotAllowed) {
		t.Errorf("error = %v, want the type rejection to win", err)
	}
}

// yt-dlp turns a public page into an arbitrary stream URL, so what the decoder
// receives is never what the guard saw at submission.
func TestAdmitResolved_RechecksTheResolvedAddress(t *testing.T) {
	g := NewGuard(corePolicy)
	src, err := g.Admit(Request{URL: "https://example.com/episode", Type: SourcePodcast})
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}

	if _, err := g.AdmitResolved(src, "http://169.254.169.254/latest/meta-data/"); !errors.Is(err, ErrUnsafeURL) {
		t.Errorf("resolved to metadata endpoint: error = %v, want ErrUnsafeURL", err)
	}

	// A literal public IP, so the test does not depend on DNS.
	const resolved = "https://1.1.1.1/ep.mp3"
	got, err := g.AdmitResolved(src, resolved)
	if err != nil {
		t.Fatalf("AdmitResolved: %v", err)
	}
	if got.URL() != resolved {
		t.Errorf("URL() = %q, want the resolved address", got.URL())
	}
	// The type survives resolution: a podcast page stays a podcast once its
	// enclosure is known.
	if got.Type() != SourcePodcast {
		t.Errorf("Type() = %q, want podcast", got.Type())
	}
}

// The zero value must not pass for an admitted source: that is what makes the
// type a proof rather than a label.
func TestAdmittedSource_ZeroValueCarriesNothing(t *testing.T) {
	var s AdmittedSource
	if s.URL() != "" || s.Type() != "" {
		t.Errorf("zero AdmittedSource = %+v, want empty", s)
	}
}
