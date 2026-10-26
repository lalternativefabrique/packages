# synthesis

The engine Synthiz and L'Alter both write a source's comprehension sheet with
(Synthiz ADR-063, L'Alter ADR 0035), and the source identity both key their
synthesis caches on.

```
go get github.com/lalternative/packages/go/synthesis
```

- `synthesis.New(agent, call).Synthesize(ctx, source, segments, language)`
  writes the sheet through a cortex agent whose catalog declares
  `synthesis.Skills()`. A cached sheet belongs to no one: give `call` no
  person and no memory.
- `source.CanonicalizeURL(type, url)` is the cache key's URL. Two products
  that canonicalize differently pay twice for the same source.
- `EngineVersion` is recorded with each sheet; bump it when a change to the
  skills would make a cached sheet differ from a new one.

Sheets travel between the two products as `busevents.SynthesisCachedEvent`.
