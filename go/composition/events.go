package composition

import (
	"bytes"
	"encoding/json"
	"reflect"
	"time"
)

// Content is what an author reads and writes: a title, a plain-text body, and
// the editor's own rich representation of the same text.
type Content struct {
	Title string          `json:"title"`
	Body  string          `json:"body"`
	Rich  json.RawMessage `json:"rich,omitempty"`
}

func (c Content) same(other Content) bool {
	return c.Title == other.Title &&
		c.Body == other.Body &&
		sameRich(c.Rich, other.Rich)
}

// sameRich compares two editor documents by value rather than by bytes. The
// same document reaches us spelled differently depending on where it came
// from: Postgres stores rich_content as JSONB, which reorders keys and drops
// whitespace, so the text read back never matches the editor's own
// serialisation byte for byte. Comparing raw bytes reports an edit on every
// save and fills the author's history with versions identical to each other.
func sameRich(a, b json.RawMessage) bool {
	if bytes.Equal(a, b) {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	av, aerr := decodeRich(a)
	bv, berr := decodeRich(b)
	if aerr != nil || berr != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// decodeRich keeps numbers as the text they were written as. Decoding them into
// float64 would make two documents that differ past 53 bits compare equal, and
// calling an author's edit a no-op is the one mistake this comparison must
// never make.
func decodeRich(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Origin says what produced a version, when it was not the author typing it.
// Nothing in the text tells these apart afterwards, and each of them replaces
// writing the author may want back: a version that carries one always ends its
// sitting, so what it displaced stays reachable.
type Origin string

const (
	OriginTyped       Origin = ""
	OriginRestored    Origin = "restored"
	OriginRevised     Origin = "revised"
	OriginDictated    Origin = "dictated"
	OriginIllustrated Origin = "illustrated"
	// OriginSettled is a write nobody made: an illustration arriving on its own
	// and filling the slot that was waiting for it. The document really did
	// change and has to be stored, but the author took no step, so it folds into
	// whatever sitting it lands in rather than becoming one.
	OriginSettled Origin = "settled"
)

func (o Origin) marksAStep() bool { return o != OriginTyped && o != OriginSettled }

type SourceUpdated struct {
	Content
	Origin Origin    `json:"origin,omitempty"`
	At     time.Time `json:"at"`
}

func (SourceUpdated) EventKind() string { return KindSourceUpdated }

type VariantRequested struct {
	Kind string    `json:"kind"`
	At   time.Time `json:"at"`
}

func (VariantRequested) EventKind() string { return KindVariantRequested }

// VariantGenerated carries SourceVersion so a reader can tell a variant that
// was derived from the current source from one left behind by a later edit.
type VariantGenerated struct {
	Kind          string    `json:"kind"`
	SourceVersion int       `json:"source_version"`
	Content       Content   `json:"content"`
	At            time.Time `json:"at"`
}

func (VariantGenerated) EventKind() string { return KindVariantGenerated }

type VariantGenerationFailed struct {
	Kind   string    `json:"kind"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func (VariantGenerationFailed) EventKind() string { return KindVariantGenerationFailed }

type VariantEdited struct {
	Kind    string    `json:"kind"`
	Content Content   `json:"content"`
	Origin  Origin    `json:"origin,omitempty"`
	At      time.Time `json:"at"`
}

func (VariantEdited) EventKind() string { return KindVariantEdited }

const (
	KindSourceUpdated           = "composition.source.updated"
	KindVariantRequested        = "composition.variant.requested"
	KindVariantGenerated        = "composition.variant.generated"
	KindVariantGenerationFailed = "composition.variant.generation_failed"
	KindVariantEdited           = "composition.variant.edited"
)
