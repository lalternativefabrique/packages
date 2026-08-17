package ddd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- a tiny snapshottable domain -----------------------------------------

type notepad struct {
	BaseAggregateRoot[string]
	lines []string
}

type lineWritten struct {
	Text string `json:"text"`
}

func (lineWritten) EventKind() string { return "notepad.line_written" }

func newNotepad(id string) *notepad {
	n := &notepad{}
	n.Init(id, "Notepad", SystemClock{})
	return n
}

func (n *notepad) Apply(env EventEnvelope[string]) error {
	p, ok := env.Payload.(*lineWritten)
	if !ok {
		return fmt.Errorf("%w: %T", ErrUnknownEvent, env.Payload)
	}
	n.lines = append(n.lines, p.Text)
	return nil
}

func (n *notepad) write(text string) error {
	return Raise[string, *notepad](n, &n.BaseAggregateRoot, &lineWritten{Text: text}, n.Apply)
}

type notepadState struct {
	Lines []string `json:"lines"`
}

func (n *notepad) Snapshot() (any, error) { return notepadState{Lines: n.lines}, nil }

func (n *notepad) Restore(raw json.RawMessage) error {
	var s notepadState
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	n.lines = s.Lines
	return nil
}

// --- doubles --------------------------------------------------------------

// countingReader records how many events each load actually replayed, which is
// the whole point of a snapshot and the only thing worth asserting about it.
type countingReader struct {
	mu      sync.Mutex
	events  []EventEnvelope[string]
	replays []int
}

func (r *countingReader) LoadFromVersion(_ context.Context, _ string, from int) ([]EventEnvelope[string], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []EventEnvelope[string]
	for _, e := range r.events {
		if e.AggregateVersion > from {
			out = append(out, e)
		}
	}
	r.replays = append(r.replays, len(out))
	return out, nil
}

func (r *countingReader) record(a *notepad) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, a.Uncommitted()...)
	a.MarkCommitted()
}

// all reads the stream without counting it, for test setup that is not the
// load under assertion.
func (r *countingReader) all() []EventEnvelope[string] {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]EventEnvelope[string](nil), r.events...)
}

func (r *countingReader) lastReplay() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.replays[len(r.replays)-1]
}

type memSnapshots struct {
	mu       sync.Mutex
	snap     SnapshotRecord[string]
	has      bool
	failLoad bool
	saves    int
}

func (s *memSnapshots) Save(_ context.Context, snap SnapshotRecord[string]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap, s.has, s.saves = snap, true, s.saves+1
	return nil
}

func (s *memSnapshots) Load(_ context.Context, id string) (SnapshotRecord[string], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failLoad {
		return SnapshotRecord[string]{}, errors.New("snapshot store is down")
	}
	if !s.has {
		return SnapshotRecord[string]{}, errors.New("not found")
	}
	return s.snap, nil
}

// writeLines appends n writes to whatever the reader already holds, picking up
// the version where the stream left off — a fresh aggregate would restart at
// version 1 and write a stream no store would accept.
func writeLines(t *testing.T, reader *countingReader, id string, n int) {
	t.Helper()
	pad := newNotepad(id)
	require.NoError(t, LoadFromHistory[string, *notepad](pad, &pad.BaseAggregateRoot, reader.all()))
	for i := range n {
		require.NoError(t, pad.write(fmt.Sprintf("line %d", pad.Version()+i)))
	}
	reader.record(pad)
}

// --- tests ----------------------------------------------------------------

func TestLoadReplaysEverythingWithoutASnapshot(t *testing.T) {
	reader := &countingReader{}
	writeLines(t, reader, "pad", 7)

	loader := NewSnapshotLoader[string](reader, nil, 5)
	pad := newNotepad("pad")
	require.NoError(t, Load(context.Background(), loader, pad, &pad.BaseAggregateRoot, "pad"))

	assert.Len(t, pad.lines, 7)
	assert.Equal(t, 7, pad.Version())
	assert.Equal(t, 7, reader.lastReplay())
}

func TestLoadReplaysOnlyWhatFollowsTheSnapshot(t *testing.T) {
	reader := &countingReader{}
	snaps := &memSnapshots{}
	loader := NewSnapshotLoader[string](reader, snaps, 5)
	ctx := context.Background()

	writeLines(t, reader, "pad", 12)

	pad := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, pad, &pad.BaseAggregateRoot, "pad"))
	require.NoError(t, MaybeSave(ctx, loader, pad))
	require.Equal(t, 1, snaps.saves)

	again := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, again, &again.BaseAggregateRoot, "pad"))

	assert.Equal(t, pad.lines, again.lines)
	assert.Equal(t, 12, again.Version(), "a restored aggregate resumes at the version it was saved at")
	assert.Equal(t, 0, reader.lastReplay(), "everything came from the snapshot")
}

func TestLoadStillWorksWhenTheSnapshotStoreFails(t *testing.T) {
	reader := &countingReader{}
	snaps := &memSnapshots{}
	loader := NewSnapshotLoader[string](reader, snaps, 5)
	ctx := context.Background()

	writeLines(t, reader, "pad", 6)
	pad := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, pad, &pad.BaseAggregateRoot, "pad"))
	require.NoError(t, MaybeSave(ctx, loader, pad))

	snaps.failLoad = true
	again := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, again, &again.BaseAggregateRoot, "pad"))

	assert.Equal(t, pad.lines, again.lines, "the event stream is enough on its own")
	assert.Equal(t, 6, reader.lastReplay())
}

func TestUnreadableSnapshotFallsBackToAFullReplay(t *testing.T) {
	reader := &countingReader{}
	snaps := &memSnapshots{}
	loader := NewSnapshotLoader[string](reader, snaps, 5)
	ctx := context.Background()

	writeLines(t, reader, "pad", 6)
	snaps.snap = SnapshotRecord[string]{
		AggregateID:      "pad",
		AggregateVersion: 4,
		State:            json.RawMessage(`"a shape this aggregate never wrote"`),
	}
	snaps.has = true

	pad := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, pad, &pad.BaseAggregateRoot, "pad"))

	assert.Len(t, pad.lines, 6)
	assert.Equal(t, 6, pad.Version())
	assert.Equal(t, 6, reader.lastReplay(), "a snapshot it cannot read is a snapshot it does not have")
}

func TestSnapshotIsWrittenOnlyOncePerInterval(t *testing.T) {
	reader := &countingReader{}
	snaps := &memSnapshots{}
	loader := NewSnapshotLoader[string](reader, snaps, 5)
	ctx := context.Background()

	// One save at a time, reloading in between, which is how a repository
	// actually drives an aggregate.
	for i := range 12 {
		pad := newNotepad("pad")
		require.NoError(t, Load(ctx, loader, pad, &pad.BaseAggregateRoot, "pad"))
		require.NoError(t, pad.write(fmt.Sprintf("line %d", i)))
		reader.record(pad)
		require.NoError(t, MaybeSave(ctx, loader, pad))
	}

	assert.Equal(t, 2, snaps.saves, "12 events at an interval of 5 is two snapshots, not twelve")
	assert.Equal(t, 10, snaps.snap.AggregateVersion)
}

func TestNoSnapshotStoreMeansNoSnapshots(t *testing.T) {
	reader := &countingReader{}
	loader := NewSnapshotLoader[string](reader, nil, 5)
	ctx := context.Background()

	writeLines(t, reader, "pad", 20)
	pad := newNotepad("pad")
	require.NoError(t, Load(ctx, loader, pad, &pad.BaseAggregateRoot, "pad"))

	assert.NoError(t, MaybeSave(ctx, loader, pad), "a domain that has not opted in is not an error")
}
