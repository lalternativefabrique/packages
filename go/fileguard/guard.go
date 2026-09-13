package fileguard

import (
	"errors"
	"fmt"
	"strings"
)

// SourceType is what a caller says it is submitting. The list is closed: a
// value outside it is a rejection, never a default.
type SourceType string

const (
	SourceYouTube  SourceType = "youtube"
	SourcePodcast  SourceType = "podcast"
	SourcePDF      SourceType = "pdf"
	SourceRSS      SourceType = "rss"
	SourceWebPage  SourceType = "webpage"
	SourceAudioURL SourceType = "audio_url"
)

var (
	// ErrTypeNotAllowed reports a source type this deployment does not handle.
	ErrTypeNotAllowed = errors.New("fileguard: source type not allowed here")
	// ErrAnonNotAllowed reports a type an account-less visitor may not submit.
	ErrAnonNotAllowed = errors.New("fileguard: source type requires an account")
	// ErrUnsafeURL reports a URL that is not fetchable: a scheme other than
	// http(s), or a host that is (or resolves to) an internal address.
	ErrUnsafeURL = errors.New("fileguard: url is not a fetchable public url")
)

// Policy is what a product declares once, at wiring time, instead of spreading
// the same decisions across its handlers. The two products differ — Techtuel
// handles no documents, core rations anonymous visitors — and a shared guard
// has to carry that difference explicitly rather than pick a winner.
type Policy struct {
	// Allowed lists every type this deployment accepts.
	Allowed []SourceType
	// AllowedAnonymous lists what a visitor without an account may submit. It
	// is intersected with Allowed; empty means anonymous submission is refused
	// outright.
	AllowedAnonymous []SourceType
}

// Guard admits sources according to a Policy. It is the single entry point
// every ingestion path goes through, so a check is not something a handler can
// forget to call: the pipeline takes an AdmittedSource, and only Admit mints
// one.
type Guard struct {
	allowed   map[SourceType]bool
	anonymous map[SourceType]bool
}

// NewGuard builds a Guard from a Policy.
func NewGuard(p Policy) *Guard {
	g := &Guard{
		allowed:   make(map[SourceType]bool, len(p.Allowed)),
		anonymous: make(map[SourceType]bool, len(p.AllowedAnonymous)),
	}
	for _, t := range p.Allowed {
		g.allowed[t] = true
	}
	for _, t := range p.AllowedAnonymous {
		if g.allowed[t] {
			g.anonymous[t] = true
		}
	}
	return g
}

// Request is one submission to admit.
type Request struct {
	URL string
	// Type is the caller's declared type. Empty is a rejection: detection from
	// the URL shape falls through to "podcast" for anything unrecognised, which
	// would silently route an arbitrary URL to the decoder.
	Type SourceType
	// Anonymous is true for a visitor with no account.
	Anonymous bool
}

// AdmittedSource is a URL that passed the guard. Its fields are read-only by
// construction: the pipeline accepts this type rather than a string, so a path
// that skipped Admit does not compile.
type AdmittedSource struct {
	url        string
	sourceType SourceType
}

// URL returns the admitted URL.
func (s AdmittedSource) URL() string { return s.url }

// Type returns the admitted source type.
func (s AdmittedSource) Type() SourceType { return s.sourceType }

// Admit runs the checks every ingestion path owes, in the order that refuses
// most cheaply first: the declared type, then who is asking, then the URL
// itself — DNS resolution is the only step that costs anything.
func (g *Guard) Admit(req Request) (AdmittedSource, error) {
	t, ok := NormalizeSourceType(string(req.Type))
	if !ok {
		return AdmittedSource{}, fmt.Errorf("%w: %q", ErrTypeNotAllowed, req.Type)
	}
	if !g.allowed[t] {
		return AdmittedSource{}, fmt.Errorf("%w: %q", ErrTypeNotAllowed, t)
	}
	if req.Anonymous && !g.anonymous[t] {
		return AdmittedSource{}, fmt.Errorf("%w: %q", ErrAnonNotAllowed, t)
	}
	if err := ValidateFetchURL(req.URL); err != nil {
		return AdmittedSource{}, fmt.Errorf("%w: %v", ErrUnsafeURL, err)
	}
	return AdmittedSource{url: req.URL, sourceType: t}, nil
}

// AdmitResolved re-runs the URL check on an address a resolver produced, and
// mints a source for it. yt-dlp turns a public page into an arbitrary stream
// URL, so the address the decoder is handed is never the one the guard saw at
// submission; re-checking here is what makes the guarantee hold end to end.
func (g *Guard) AdmitResolved(from AdmittedSource, resolvedURL string) (AdmittedSource, error) {
	if err := ValidateFetchURL(resolvedURL); err != nil {
		return AdmittedSource{}, fmt.Errorf("%w: %v", ErrUnsafeURL, err)
	}
	return AdmittedSource{url: resolvedURL, sourceType: from.sourceType}, nil
}

// NormalizeSourceType maps user-facing aliases onto canonical types. It
// mirrors lib/sourceresolver's own normalizer, which cannot be imported here:
// fileguard carries no dependencies, and sourceresolver depends on it.
func NormalizeSourceType(raw string) (SourceType, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "youtube":
		return SourceYouTube, true
	case "podcast":
		return SourcePodcast, true
	case "pdf":
		return SourcePDF, true
	case "rss":
		return SourceRSS, true
	case "webpage", "article", "web":
		return SourceWebPage, true
	case "audio_url", "audio":
		return SourceAudioURL, true
	}
	return "", false
}
