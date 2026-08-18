// Package composition holds the event-sourced writing engine: one authored
// source, and the variants derived from it.
//
// The source is a sanctuary. Nothing in this package writes generated content
// into it — only UpdateSource, which an author's own keystrokes reach. A
// variant is always a separate document, so generating one can never destroy
// what was written.
//
// It carries no vocabulary from any bounded context: a variant is keyed by an
// opaque kind string, which the caller maps to whatever it means for them (a
// social platform here, something else elsewhere).
package composition

import (
	"fmt"
	"strings"
	"time"

	"github.com/lalternative/packages/go/eda/pkg/cqrs"
	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

type ID = string

const AggregateType = "Composition"

type Status string

const (
	StatusPending    Status = "pending"
	StatusGenerating Status = "generating"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

type Variant struct {
	Kind          string
	Status        Status
	Content       Content
	SourceVersion int
	Reason        string
	RequestedAt   time.Time
	GeneratedAt   time.Time
	EditedAt      time.Time
}

// Stale reports whether the source moved on after this variant was derived.
// The author is the one who decides what to do about it; the aggregate only
// answers the question.
func (v Variant) Stale(sourceVersion int) bool {
	return v.Status == StatusReady && v.SourceVersion < sourceVersion
}

type Composition struct {
	ddd.BaseAggregateRoot[ID]
	source         Content
	sourceVersion  int
	sourceEditedAt time.Time
	variants       map[string]Variant
}

func New(id ID) *Composition {
	c := &Composition{variants: map[string]Variant{}}
	c.Init(id, AggregateType, ddd.SystemClock{})
	return c
}

func (c *Composition) Source() Content { return c.source }

// SourceVersion counts source edits, not aggregate versions: it is what a
// variant pins itself to, so generating variants never makes them stale.
func (c *Composition) SourceVersion() int { return c.sourceVersion }

func (c *Composition) Variant(kind string) (Variant, bool) {
	v, ok := c.variants[kind]
	return v, ok
}

func (c *Composition) Variants() map[string]Variant {
	out := make(map[string]Variant, len(c.variants))
	for k, v := range c.variants {
		out[k] = v
	}
	return out
}

func (c *Composition) Apply(env ddd.EventEnvelope[ID]) error {
	switch p := env.Payload.(type) {
	case *SourceUpdated:
		c.source = p.Content
		c.sourceVersion++
		c.sourceEditedAt = p.At
	case *VariantRequested:
		v := c.variants[p.Kind]
		v.Kind = p.Kind
		v.Status = StatusGenerating
		v.RequestedAt = p.At
		v.Reason = ""
		c.variants[p.Kind] = v
	case *VariantGenerated:
		v := c.variants[p.Kind]
		v.Kind = p.Kind
		v.Status = StatusReady
		v.Content = p.Content
		v.SourceVersion = p.SourceVersion
		v.GeneratedAt = p.At
		v.Reason = ""
		c.variants[p.Kind] = v
	case *VariantGenerationFailed:
		v := c.variants[p.Kind]
		v.Kind = p.Kind
		v.Status = StatusFailed
		v.Reason = p.Reason
		c.variants[p.Kind] = v
	case *VariantEdited:
		v := c.variants[p.Kind]
		v.Kind = p.Kind
		v.Status = StatusReady
		v.Content = p.Content
		v.EditedAt = p.At
		c.variants[p.Kind] = v
	default:
		return fmt.Errorf("%w: %T", ddd.ErrUnknownEvent, env.Payload)
	}
	return nil
}

// UpdateSource records an author's edit. Generated content never reaches this
// method: that is what makes the source a sanctuary rather than a convention.
func (c *Composition) UpdateSource(content Content, now time.Time) error {
	if content.same(c.source) {
		return nil
	}
	return c.raise(&SourceUpdated{Content: content, At: now})
}

// RewriteSource records text that reached the source by some route other than
// the author typing it — brought back from the history, revised on request,
// dictated aloud. What came before stays in the stream, so any of them can be
// undone; the origin is what keeps the version a point of its own rather than
// something folded into the sitting it interrupted.
func (c *Composition) RewriteSource(content Content, origin Origin, now time.Time) error {
	if content.same(c.source) {
		return nil
	}
	return c.raise(&SourceUpdated{Content: content, Origin: origin, At: now})
}

// RewriteVariant is RewriteSource for one adaptation: the same guarantee, on
// the document the author was actually reading.
func (c *Composition) RewriteVariant(kind string, content Content, origin Origin, now time.Time) error {
	if err := validKind(kind); err != nil {
		return err
	}
	v, ok := c.variants[kind]
	if !ok {
		return fmt.Errorf("%w: no %s variant to rewrite", cqrs.ErrValidation, kind)
	}
	if v.Content.same(content) {
		return nil
	}
	return c.raise(&VariantEdited{Kind: kind, Content: content, Origin: origin, At: now})
}

func (c *Composition) RequestVariant(kind string, now time.Time) error {
	if err := validKind(kind); err != nil {
		return err
	}
	if strings.TrimSpace(c.source.Body) == "" {
		return fmt.Errorf("%w: cannot derive a variant from an empty source", cqrs.ErrValidation)
	}
	if v, ok := c.variants[kind]; ok && v.Status == StatusGenerating {
		return nil
	}
	return c.raise(&VariantRequested{Kind: kind, At: now})
}

func (c *Composition) GenerateVariant(kind string, content Content, now time.Time) error {
	if err := validKind(kind); err != nil {
		return err
	}
	// Callers replay the whole set of variants on every save; only a variant
	// whose text actually moved is a new version.
	if v, ok := c.variants[kind]; ok && v.Status == StatusReady && v.Content.same(content) {
		return nil
	}
	return c.raise(&VariantGenerated{
		Kind:          kind,
		SourceVersion: c.sourceVersion,
		Content:       content,
		At:            now,
	})
}

func (c *Composition) FailVariant(kind string, reason string, now time.Time) error {
	if err := validKind(kind); err != nil {
		return err
	}
	return c.raise(&VariantGenerationFailed{Kind: kind, Reason: reason, At: now})
}

// EditVariant is the author retouching a generated variant. A variant is a
// document of its own once it exists, so its edits are versioned exactly like
// the source's.
func (c *Composition) EditVariant(kind string, content Content, now time.Time) error {
	if err := validKind(kind); err != nil {
		return err
	}
	v, ok := c.variants[kind]
	if !ok {
		return fmt.Errorf("%w: no %s variant to edit", cqrs.ErrValidation, kind)
	}
	if v.Content.same(content) {
		return nil
	}
	return c.raise(&VariantEdited{Kind: kind, Content: content, At: now})
}

func (c *Composition) raise(payload ddd.EventPayload) error {
	return ddd.Raise[ID, *Composition](c, &c.BaseAggregateRoot, payload, c.Apply)
}

func validKind(kind string) error {
	if strings.TrimSpace(kind) == "" {
		return fmt.Errorf("%w: variant kind is required", cqrs.ErrValidation)
	}
	return nil
}
