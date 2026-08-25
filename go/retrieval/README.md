# retrieval

Decides which candidates of a similarity search are relevant, and fuses a dense
score with a lexical one.

No storage, no embedding provider, no SQL: callers bring their own candidates.
Works against pgvector, any other vector store, or a fixture in a test.

```go
import "github.com/lalternative/packages/go/retrieval"
```

Forked from Synthiz's `lib/retrieval` (`github.com/digstack/synthiz/lib/retrieval`),
where the numbers in this doc were measured. The two copies are not kept in
sync automatically — if the gate or fusion logic changes here, or drifts from
the source, that is expected, not a bug.

## Why not a similarity threshold

Measured on a production corpus of 948 summary embeddings (bge-m3):

| query | max | p99 | mean | stddev | relevant docs |
|---|---|---|---|---|---|
| féodalisme | 0.510 | 0.372 | 0.279 | 0.042 | 5 |
| marketing | 0.501 | 0.473 | 0.363 | 0.041 | 8 |
| vin degustation | 0.649 | 0.422 | 0.326 | 0.039 | 3 |
| *a query matching nothing* | 0.424 | 0.376 | 0.288 | 0.034 | 0 |

`féodalisme` and the nothing-query peak **0.09 apart**, yet one has five
relevant documents and the other none. A cutoff at 0.65 returned nothing for a
user holding five feudalism sources; lowering it far enough to reach them let
unrelated documents back in.

The scale moves with the query — a one-word query compared against a whole
summary tops out far lower than a paragraph does — and two texts in the same
language always share a floor of similarity. There is no natural zero: an
article about Cleopatra still scores 0.350 against `féodalisme`.

## What is used instead

**The gap to the rest of the field.** A relevant document stands out from the
corpus; when nothing matches, every candidate bunches together. The floor is
computed per query as `mean + z*stddev` over that query's own distribution.

**Plus an absolute minimum.** When nothing matches the spread collapses and the
least-bad document looks unusually detached — a K2 climbing story cleared 3.5σ
on a nonsense query. A candidate must both stand out **and** be close enough in
absolute terms.

```go
dist := retrieval.DescribeSimilarities(allSimilarities) // over the whole corpus
gate := retrieval.DefaultGate()

if gate.Admits(candidate.Similarity, dist) {
    score := gate.DenseScore(candidate.Similarity, dist)
}
```

Compute the distribution over the **full corpus**, not the retrieved top-N: the
top-N sits above its own mean by construction, which makes the test partly
circular and ties the floor to an arbitrary limit.

## Fusing dense and lexical

`Fuse` ranks on normalised scores rather than on rank. Reciprocal rank fusion
only reads position, so rank 1 of a list that found nothing weighs as much as
rank 1 of a list that nailed it — in production the best and worst of twenty RRF
candidates sat within 12% of each other.

```go
ranked := retrieval.Fuse(candidates, dist, gate, retrieval.EqualWeights(), 0.15)
```

`minScore` drops what neither signal supports, which is the "no match" answer
rank fusion cannot give.

## Passages inside documents

When candidates are chunks rather than whole documents, distances do not
separate topic from mention: on a real question the noise interleaves with the
signal (0.305 noise, 0.324 signal, 0.370 noise, 0.378 signal).

`KeepCohesive` asks a different question — not *is this passage close?* but *is
this document about the topic?*

```go
kept := retrieval.KeepCohesive(passages, retrieval.DefaultCohesion)
```

Set `SourceChunks` on each passage. A raw count reads length, not subject: a
three-chunk article had to place two thirds of everything it contains to
qualify, where a 1440-chunk video needs 0.1% of its own.

## Tuning

`DefaultZThreshold` (2.0) and `DefaultSimilarityFloor` (0.44) are starting
points measured on one corpus with one embedding model. A different model or
corpus shifts them. Measure before trusting them:

```sql
SELECT avg(sim), stddev(sim),
       percentile_cont(0.95) WITHIN GROUP (ORDER BY sim)
FROM (SELECT 1 - (embedding <=> :query) AS sim FROM your_embeddings) q;
```

Run it for a query you know has relevant documents and one you know has none.
The floor belongs between the second query's peak and the first query's weakest
genuine match.
