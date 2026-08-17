package ddd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Snapshottable is implemented by aggregates that can hand over their state and
// take it back. Replaying an aggregate's whole history on every load is fine
// until the history is long: a document saved every few seconds accumulates
// events for as long as someone keeps writing, and each load pays for all of
// them again.
//
// The aggregate is the only one that can do this — its state is its own, and a
// library has no way to read or rebuild private fields. Snapshot returns a
// JSON-serializable value; Restore takes back what Snapshot produced.
//
// The shape returned by Snapshot is stored as it is written, so it outlives the
// code that produced it: treat it as a wire format. Restore should return an
// error rather than guess when it meets state it no longer understands — a
// refusal costs a full replay, where a wrong guess costs correctness.
type Snapshottable interface {
	Snapshot() (any, error)
	Restore(state json.RawMessage) error
}

// SnapshotRecord is the stored form of an aggregate's state at a version. It
// mirrors the shape event stores persist, so a store implementation satisfies
// SnapshotStore without a translation layer.
type SnapshotRecord[ID comparable] struct {
	AggregateID      ID              `json:"aggregate_id"`
	AggregateType    string          `json:"aggregate_type"`
	AggregateVersion int             `json:"aggregate_version"`
	State            json.RawMessage `json:"state"`
}

// SnapshotStore persists one snapshot per aggregate, overwriting the previous
// one. Load reports a missing snapshot with an error; SnapshotLoader treats any
// error as "no snapshot" and replays instead, so a store that has lost its
// snapshots costs speed, never correctness.
type SnapshotStore[ID comparable] interface {
	Save(ctx context.Context, snap SnapshotRecord[ID]) error
	Load(ctx context.Context, aggregateID ID) (SnapshotRecord[ID], error)
}

// EventReader is the slice of an event store a snapshot load needs: the events
// after a version, rather than all of them.
type EventReader[ID comparable] interface {
	LoadFromVersion(ctx context.Context, aggregateID ID, fromVersion int) ([]EventEnvelope[ID], error)
}

// SnapshotLoader loads an aggregate from its latest snapshot plus the events
// recorded since, and writes a new snapshot once enough events have piled up
// on top of the last one.
//
// Every 50 events by default: a load then replays at most that many, whatever
// the aggregate's age, and a snapshot is written once per 50 saves rather than
// on each one. Interval is per domain — an aggregate with heavy state and rare
// writes wants a larger one than a chatty, cheap one.
type SnapshotLoader[ID comparable] struct {
	events    EventReader[ID]
	snapshots SnapshotStore[ID]
	interval  int
}

// DefaultSnapshotInterval is the event count between snapshots when none is
// given.
const DefaultSnapshotInterval = 50

// NewSnapshotLoader wires a loader. A nil snapshot store, or an interval below
// one, turns snapshotting off: Load replays everything and Save writes nothing,
// which is what a domain that has not opted in should get.
func NewSnapshotLoader[ID comparable](events EventReader[ID], snapshots SnapshotStore[ID], interval int) *SnapshotLoader[ID] {
	if interval <= 0 {
		interval = DefaultSnapshotInterval
	}
	return &SnapshotLoader[ID]{events: events, snapshots: snapshots, interval: interval}
}

// Load fills the aggregate from its snapshot and replays what came after.
//
// Anything that goes wrong with the snapshot — none stored, a store that is
// down, state the aggregate cannot read — falls back to a full replay. The
// event stream is the truth and it is always sufficient; a snapshot is an
// optimisation, and an optimisation must never be the reason a load fails.
func Load[ID comparable, A interface {
	AggregateRoot[ID]
	Snapshottable
}](ctx context.Context, l *SnapshotLoader[ID], a A, base *BaseAggregateRoot[ID], id ID) error {
	if l == nil || l.events == nil {
		return errors.New("ddd: snapshot loader has no event reader")
	}
	from := 0
	if l.snapshots != nil {
		if snap, err := l.snapshots.Load(ctx, id); err == nil {
			if err := a.Restore(snap.State); err == nil {
				base.RestoreVersion(snap.AggregateVersion)
				from = snap.AggregateVersion
			}
		}
	}

	history, err := l.events.LoadFromVersion(ctx, id, from)
	if err != nil {
		return err
	}
	for _, env := range history {
		if err := a.Apply(env); err != nil {
			return err
		}
		base.version = env.AggregateVersion
	}
	return nil
}

// MaybeSave writes a snapshot when the aggregate has moved at least an interval
// past the one already stored. It is meant to be called after a successful save,
// with the version the aggregate is now at.
//
// Failures are returned so a caller can log them, never so a caller must act:
// the events are already committed, and a snapshot that did not get written
// only means the next load replays further.
func MaybeSave[ID comparable, A interface {
	AggregateRoot[ID]
	Snapshottable
}](ctx context.Context, l *SnapshotLoader[ID], a A) error {
	if l == nil || l.snapshots == nil {
		return nil
	}
	version := a.Version()
	if version < l.interval {
		return nil
	}
	// Compared against what is stored rather than counted in memory: a
	// repository loads and drops aggregates constantly, so a counter living on
	// the aggregate would reset on every load and snapshot far too often.
	stored := 0
	if snap, err := l.snapshots.Load(ctx, a.ID()); err == nil {
		stored = snap.AggregateVersion
	}
	if version-stored < l.interval {
		return nil
	}

	state, err := a.Snapshot()
	if err != nil {
		return fmt.Errorf("ddd: take snapshot: %w", err)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("ddd: marshal snapshot: %w", err)
	}
	return l.snapshots.Save(ctx, SnapshotRecord[ID]{
		AggregateID:      a.ID(),
		AggregateType:    a.AggregateType(),
		AggregateVersion: version,
		State:            raw,
	})
}
