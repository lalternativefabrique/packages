# composition

Event-sourced writing engine: one authored source, and the variants derived
from it.

## Why this exists

An editor writes a document; the product wants its history back. Not the raw
autosave stream — the sittings an author would recognise — plus the ability to
replay any of them and restore it. Every app that holds a text editor rebuilds
this, and rebuilds it differently.

This package holds the engine. It carries **no vocabulary from any bounded
context**: a variant is keyed by an opaque `kind` string, which the caller maps
to whatever it means for them — a social platform, a language, a channel.

## The guarantee

The source is a sanctuary. Nothing in this package writes generated content
into it; only `UpdateSource` and `RewriteSource`, which an author's own edits
reach. A variant is always a separate document, so generating one can never
destroy what was written.

## Content

```go
type Content struct {
	Title string          `json:"title"`
	Body  string          `json:"body"`
	Rich  json.RawMessage `json:"rich,omitempty"`
}
```

`Rich` is the editor's own representation, stored opaquely — the engine never
parses it. `Body` is the plain text the rest of a system reads. Two documents
are compared by decoded value, not by bytes, because a JSONB round-trip
reorders keys and would otherwise report an edit on every save.

## History

Raw autosaves count a version per hesitation. `SourceVersions` folds them into
sittings: a pause longer than `SessionGap`, or about a paragraph's worth of
change (`SessionGrowth`), reads as a step of its own. `AllSourceVersions` is
the unfolded list — anything asking "is this a real point in this text's past?"
has to ask that one, since the folded list moves as the author keeps typing.

`Origin` marks what produced a version when it was not the author typing:
restored, revised, dictated, illustrated. Only the caller knows — an autosave
carries the finished text either way.

## Usage

```go
store, err := composition.NewJetStreamStore(ctx, nc)
repo := composition.NewRepository(store)

// Optional: load through snapshots instead of the full stream.
snapshots, err := composition.NewJetStreamSnapshots(ctx, nc)
repo = repo.WithSnapshots(snapshots)

c, err := repo.Load(ctx, id)
if err := c.UpdateSource(content, time.Now()); err != nil { ... }
if err := repo.Save(ctx, c); err != nil { ... }

events, err := repo.History(ctx, id)
versions := composition.SourceVersions(events)
at, err := composition.Replay(id, events, versions[0].AggregateVersion)
```

## Testing

```bash
go test ./...                      # 27 BDD scenarios, in-memory store
go test -tags=integration ./...    # same feature file, real JetStream
```

The integration run needs NATS with JetStream >= 2.11 for the atomic OCC the
store defaults to; point `NATS_URL` at a non-default one.
