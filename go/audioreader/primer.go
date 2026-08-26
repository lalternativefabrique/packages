package audioreader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	sharedtts "github.com/lalternative/packages/go/tts"
)

// uploader is all a primer does with a store: put one object away.
type uploader interface {
	Upload(ctx context.Context, key string, body io.Reader, contentType string) error
}

// Primer reads the opening of a text before anyone asks for it.
type Primer struct {
	tts         Provider
	storage     uploader
	openingSize int
	log         *slog.Logger
}

// NewPrimer returns nil when there is nothing to prime with, which is how the
// feature stays absent rather than half-present: no voice or nowhere to keep
// the audio means listeners wait exactly as they did before.
//
// A nil logger defaults to slog.Default().
func NewPrimer(provider Provider, store uploader, openingSize int, log *slog.Logger) *Primer {
	if provider == nil || store == nil {
		return nil
	}
	if log == nil {
		log = slog.Default()
	}
	return &Primer{tts: provider, storage: store, openingSize: openingSize, log: log}
}

// PrimeOpening reads the first piece of text and stores it.
//
// The cut is the same one the reading itself would make, so the opening is a
// piece the player can hand to a SourceBuffer as-is rather than a slice of
// audio that starts or ends mid-word.
func (p *Primer) PrimeOpening(ctx context.Context, scope, id, text string) error {
	if p == nil {
		return nil
	}
	pieces := sharedtts.Split(text, p.openingSize)
	if len(pieces) == 0 {
		return nil
	}

	audio, mime, err := p.tts.Synthesize(ctx, pieces[0], "")
	if err != nil {
		return fmt.Errorf("read opening: %w", err)
	}
	if len(audio) == 0 {
		return fmt.Errorf("read opening: no audio for a %d-rune piece", len([]rune(pieces[0])))
	}

	if err := p.storage.Upload(ctx, OpeningKey(scope, id, text), bytes.NewReader(audio), mime); err != nil {
		return fmt.Errorf("store opening: %w", err)
	}
	return nil
}
