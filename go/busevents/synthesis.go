package busevents

import (
	"encoding/json"
	"fmt"
)

// Published by Synthiz and L'Alter under their own prefix, each consuming the
// other's (Synthiz ADR-063, L'Alter ADR 0035).
const SynthesisCached = "synthesis.cached"

const (
	Synthiz = "synthiz"
	Lalter  = "lalter"
)

// SynthesisCachedEvent is a comprehension sheet a product paid for, offered
// to the other's cache. It is published only after the product's own engine
// ran on a cache miss, never for a sheet received from the bus, so the two
// products cannot ping-pong. Only public sources are published.
//
// The cache key is SourceType, SourceURL (canonicalized by
// go/synthesis/source) and Language. Segments is the source text as a
// []synthesis.Segment and Content the sheet as a synthesis.Content, both in
// go/synthesis's JSON.
type SynthesisCachedEvent struct {
	Meta
	SourceType    string          `json:"source_type"`
	SourceURL     string          `json:"source_url"`
	Title         string          `json:"title,omitempty"`
	Language      string          `json:"language"`
	EngineVersion string          `json:"engine_version"`
	Segments      json.RawMessage `json:"segments"`
	Content       json.RawMessage `json:"content"`
}

func (e SynthesisCachedEvent) Validate() error {
	if err := e.validate(); err != nil {
		return err
	}
	if e.SourceType == "" || e.SourceURL == "" || e.Language == "" || e.EngineVersion == "" {
		return fmt.Errorf("%w: source_type, source_url, language and engine_version are required", ErrInvalid)
	}
	if len(e.Content) == 0 || len(e.Segments) == 0 {
		return fmt.Errorf("%w: content and segments are required", ErrInvalid)
	}
	return nil
}
