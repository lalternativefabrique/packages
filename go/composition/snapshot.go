package composition

import (
	"encoding/json"
	"time"
)

// SnapshotInterval is how many events a composition takes before its state is
// worth storing. A post is saved every time its author pauses, so a text
// written over several sittings accumulates events for as long as it is being
// worked on, and replaying all of them on every autosave is what this bounds.
const SnapshotInterval = 50

// snapshotState is the stored shape of a composition. It is written to a store
// and read back by code that may be a release older, so it is a wire format:
// fields are added, never repurposed, and what an older version wrote has to
// stay readable — or fail loudly enough that the loader replays instead.
type snapshotState struct {
	Source         Content            `json:"source"`
	SourceVersion  int                `json:"source_version"`
	SourceEditedAt time.Time          `json:"source_edited_at"`
	Variants       map[string]Variant `json:"variants"`
}

// Snapshot hands over everything Apply builds up. The aggregate is the only
// thing that can do this: its state is private, and a loader has no way to
// reach it.
func (c *Composition) Snapshot() (any, error) {
	return snapshotState{
		Source:         c.source,
		SourceVersion:  c.sourceVersion,
		SourceEditedAt: c.sourceEditedAt,
		Variants:       c.Variants(),
	}, nil
}

// Restore takes back what Snapshot produced. An error here is not a failure:
// the loader answers it by replaying the stream from the beginning, which is
// always available and always right.
func (c *Composition) Restore(raw json.RawMessage) error {
	var s snapshotState
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	c.source = s.Source
	c.sourceVersion = s.SourceVersion
	c.sourceEditedAt = s.SourceEditedAt
	c.variants = s.Variants
	if c.variants == nil {
		c.variants = map[string]Variant{}
	}
	return nil
}
