//go:build !integration

package composition_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/go/eda/pkg/db"

	"github.com/lalternative/packages/go/composition"
)

// writeMany drives one composition through n author edits, saving each one the
// way autosave does.
func writeMany(t *testing.T, repo *composition.Repository, id composition.ID, n int) {
	t.Helper()
	ctx := context.Background()
	at := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	for i := range n {
		c, err := repo.Load(ctx, id)
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if err := c.UpdateSource(composition.Content{Body: fmt.Sprintf("draft %d", i)}, at.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("update: %v", err)
		}
		if err := repo.Save(ctx, c); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
}

// TestSnapshottedLoadMatchesAFullReplay is the property that matters: a
// composition rebuilt through a snapshot has to be the one the events describe,
// or the optimisation is quietly handing authors the wrong text.
func TestSnapshottedLoadMatchesAFullReplay(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	store := db.NewInMemoryStore[composition.ID]()

	snapshotted := composition.NewRepository(store).
		WithSnapshots(db.ForAggregates[composition.ID](db.NewInMemorySnapshotStore[composition.ID]()))
	writeMany(t, snapshotted, id, composition.SnapshotInterval*2+3)

	fromSnapshot, err := snapshotted.Load(ctx, id)
	if err != nil {
		t.Fatalf("load through snapshot: %v", err)
	}
	// The same events, read by a repository that never saw a snapshot.
	fromEvents, err := composition.NewRepository(store).Load(ctx, id)
	if err != nil {
		t.Fatalf("load through replay: %v", err)
	}

	if fromSnapshot.Source().Body != fromEvents.Source().Body {
		t.Errorf("source differs: snapshot %q, replay %q", fromSnapshot.Source().Body, fromEvents.Source().Body)
	}
	if fromSnapshot.SourceVersion() != fromEvents.SourceVersion() {
		t.Errorf("source version differs: snapshot %d, replay %d", fromSnapshot.SourceVersion(), fromEvents.SourceVersion())
	}
	if fromSnapshot.Version() != fromEvents.Version() {
		t.Errorf("aggregate version differs: snapshot %d, replay %d", fromSnapshot.Version(), fromEvents.Version())
	}
}

// TestSnapshottedCompositionStillAcceptsWrites guards the version a snapshot
// restores: optimistic concurrency is checked against it, so an aggregate that
// came back at the wrong one has its next save rejected.
func TestSnapshottedCompositionStillAcceptsWrites(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	repo := composition.NewRepository(db.NewInMemoryStore[composition.ID]()).
		WithSnapshots(db.ForAggregates[composition.ID](db.NewInMemorySnapshotStore[composition.ID]()))

	writeMany(t, repo, id, composition.SnapshotInterval+1)

	c, err := repo.Load(ctx, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := c.UpdateSource(composition.Content{Body: "written after the snapshot"}, time.Now()); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := repo.Save(ctx, c); err != nil {
		t.Fatalf("save after a snapshotted load: %v", err)
	}

	again, err := repo.Load(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := again.Source().Body; got != "written after the snapshot" {
		t.Errorf("reloaded text is %q", got)
	}
}

// TestHistoryIsWholeDespiteSnapshots keeps the two apart: a snapshot is a
// shortcut for loading state, never a replacement for the record. The version
// list an author reads comes from the events, all of them.
func TestHistoryIsWholeDespiteSnapshots(t *testing.T) {
	ctx := context.Background()
	id := uuid.NewString()
	repo := composition.NewRepository(db.NewInMemoryStore[composition.ID]()).
		WithSnapshots(db.ForAggregates[composition.ID](db.NewInMemorySnapshotStore[composition.ID]()))

	const edits = composition.SnapshotInterval + 5
	writeMany(t, repo, id, edits)

	history, err := repo.History(ctx, id)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != edits {
		t.Errorf("history holds %d events, want %d", len(history), edits)
	}
}
