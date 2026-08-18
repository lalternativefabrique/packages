package composition

import (
	"time"

	"github.com/lalternative/packages/go/eda/pkg/ddd"
)

// Version is one point in a composition's history, as an author would read it:
// what the text was, and what made it change.
type Version struct {
	AggregateVersion int
	EventKind        string
	VariantKind      string
	Content          Content
	Origin           Origin
	At               time.Time
}

// Replay rebuilds the aggregate as it stood after the given aggregate version.
// Zero or negative replays nothing; a version past the end replays everything.
func Replay(id ID, history []ddd.EventEnvelope[ID], upTo int) (*Composition, error) {
	kept := history
	if upTo > 0 {
		kept = nil
		for _, env := range history {
			if env.AggregateVersion > upTo {
				break
			}
			kept = append(kept, env)
		}
	}
	c := New(id)
	if err := ddd.LoadFromHistory[ID, *Composition](c, &c.BaseAggregateRoot, kept); err != nil {
		return nil, err
	}
	return c, nil
}

// SessionGap is the pause that separates two writing sessions. Autosave writes
// an event a few seconds after the typing stops, so the raw stream counts a
// version per hesitation; an author remembers what they wrote in sittings, not
// in keystrokes. A pause longer than this reads as having come back to the
// text rather than as still writing it.
const SessionGap = time.Minute

// SessionGrowth is how much the text can move inside one sitting before it is
// worth its own version. Time alone is the wrong measure on its own: an hour
// of steady writing never pauses, and folding it whole would leave the author
// a single point of return for a whole afternoon. About a paragraph's worth of
// change is a step they would recognise; fixing a few typos is not.
const SessionGrowth = 150

// SourceVersions lists the successive states of the source, oldest first. It is
// the history an author asks for: only their own edits, not the derivations
// those edits produced, and folded into the sittings they were written in.
func SourceVersions(history []ddd.EventEnvelope[ID]) []Version {
	return foldIntoSessions(allSourceVersions(history))
}

// AllSourceVersions is SourceVersions without the folding: every state the
// source ever held. Folding is how the history is read, not what it contains,
// so anything asking "is this a real point in this text's past?" — a restore
// deciding whether to accept a version — has to ask this rather than the list
// the author happens to be shown, which changes as they keep typing.
func AllSourceVersions(history []ddd.EventEnvelope[ID]) []Version {
	return allSourceVersions(history)
}

func allSourceVersions(history []ddd.EventEnvelope[ID]) []Version {
	var out []Version
	for _, env := range history {
		p, ok := env.Payload.(*SourceUpdated)
		if !ok {
			continue
		}
		out = append(out, Version{
			AggregateVersion: env.AggregateVersion,
			EventKind:        KindSourceUpdated,
			Content:          p.Content,
			Origin:           p.Origin,
			At:               p.At,
		})
	}
	return out
}

// foldIntoSessions keeps the last version of each writing session and drops the
// keystrokes leading to it, so what remains is the text as the author left it
// each time they stopped — or each time they had written enough for the step to
// be worth returning to.
//
// Growth is measured from the last version kept rather than from the previous
// one, so a paragraph typed in twenty small saves still adds up to a step. The
// pause, on the other hand, is measured between consecutive versions: someone
// writing without stopping was never interrupted, and fixed windows would
// invent sittings that never happened.
//
// Every kept version keeps its own AggregateVersion, so it stays a point the
// stream can be replayed at and restored to.
func foldIntoSessions(versions []Version) []Version {
	if len(versions) < 2 {
		return versions
	}
	out := make([]Version, 0, len(versions))
	anchor := versions[0]
	for i, v := range versions {
		if i == len(versions)-1 {
			out = append(out, v)
			continue
		}
		if endsSession(v, versions[i+1]) || grewEnough(anchor, versions[i+1]) {
			out = append(out, v)
			anchor = versions[i+1]
		}
	}
	return out
}

// grewEnough reports whether the text moved far enough since the last version
// kept to be a step of its own. It compares lengths rather than diffing: what
// matters is that the text moved, and by how much, not which words changed.
// Deleting a paragraph is as much of a step as writing one, so the distance is
// absolute.
func grewEnough(anchor, next Version) bool {
	grown := len(next.Content.Body) - len(anchor.Content.Body)
	if grown < 0 {
		grown = -grown
	}
	return grown >= SessionGrowth
}

// endsSession reports whether v is the last version of its sitting, given the
// one that follows it.
//
// Text that did not come from the author's own typing stands on both sides of
// the boundary: it is not the continuation of the sitting it interrupted, and
// what it displaced has to stay reachable — that text is the only way back from
// a rewrite the author turns out not to want.
//
// A gap that runs backwards is treated as a boundary too. Timestamps come from
// whichever process handled the write, so clocks can disagree; keeping the
// version is the answer that loses nothing, where folding it would drop one
// silently.
func endsSession(v, next Version) bool {
	// A write nobody made never ends the version before it: the illustration
	// the author asked for is still arriving, and cutting here would leave the
	// step they took holding a document without its picture.
	if next.Origin == OriginSettled {
		return false
	}
	if v.Origin.marksAStep() || next.Origin.marksAStep() {
		return true
	}
	gap := next.At.Sub(v.At)
	return gap >= SessionGap || gap < 0
}

// VariantVersions lists the successive states of one variant, generated and
// hand-edited alike, oldest first.
func VariantVersions(history []ddd.EventEnvelope[ID], kind string) []Version {
	return foldVariantSessions(allVariantVersions(history, kind))
}

// AllVariantVersions is VariantVersions unfolded, for the same reason
// AllSourceVersions exists.
func AllVariantVersions(history []ddd.EventEnvelope[ID], kind string) []Version {
	return allVariantVersions(history, kind)
}

func allVariantVersions(history []ddd.EventEnvelope[ID], kind string) []Version {
	var out []Version
	for _, env := range history {
		switch p := env.Payload.(type) {
		case *VariantGenerated:
			if p.Kind != kind {
				continue
			}
			out = append(out, Version{
				AggregateVersion: env.AggregateVersion,
				EventKind:        KindVariantGenerated,
				VariantKind:      kind,
				Content:          p.Content,
				At:               p.At,
			})
		case *VariantEdited:
			if p.Kind != kind {
				continue
			}
			out = append(out, Version{
				AggregateVersion: env.AggregateVersion,
				EventKind:        KindVariantEdited,
				VariantKind:      kind,
				Content:          p.Content,
				Origin:           p.Origin,
				At:               p.At,
			})
		}
	}
	return out
}

// foldVariantSessions folds a variant's history the way the source's is, except
// that a generation and a correction are never folded together: reading "the
// model wrote this" where someone fixed it by hand is the kind of history that
// misleads, so a change of hand always ends a session.
func foldVariantSessions(versions []Version) []Version {
	if len(versions) < 2 {
		return versions
	}
	out := make([]Version, 0, len(versions))
	for i, v := range versions {
		if i == len(versions)-1 {
			out = append(out, v)
			continue
		}
		next := versions[i+1]
		if next.EventKind != v.EventKind || endsSession(v, next) {
			out = append(out, v)
		}
	}
	return out
}
