# ADR 0001 — A durable JetStream consumer brick in go-eda

- Status: Accepted
- Date: 2026-05-30
- Scope: `github.com/lalternative/packages/go/eda` (`pkg/consumer`), consumed by the
  `bootstrap` `--mode eda` template and by skalpai-family services.

## Context

A production bug surfaced in **reczi**: events were lost / infinitely retried
because reczi had **re-implemented a minimal JetStream consumer by hand** —
`Nak()` on every failure, no `Term()`, no `MaxDeliver`, no `BackOff`, no DLQ.

The root cause was **not** a negligent author and **not** an "incomplete
bootstrap" in the vague sense. It was structural:

1. A correct, battle-tested consumer already existed in **synthiz**
   (`apps/core/integration-events/consumer.go` + `pkg/nats/shared.go`):
   `Term` on permanent errors, bounded `MaxDeliver`, staged `BackOff`, a DLQ
   advisory stream, ack heartbeats, idempotency, and a reconnect loop.
2. But it lived as **application code inside synthiz**, not as an importable
   module. There was nothing for reczi to depend on.
3. The `bootstrap --mode eda` template made this worse: its `main.go` opened a
   NATS connection, grabbed a JetStream handle, and then left a literal
   dead-end:

   ```go
   _ = js // wire your go-eda JetStreamStore here (see .../examples)
   ```

   The template did the *easy, harmless* part and abandoned the developer at
   the *single most dangerous* line of an EDA system — redelivery semantics.
   Every new repo was therefore forced to re-derive the consumer, i.e. to
   reproduce reczi's bug.

| Concern | synthiz `StartConsumer` | old template eda | reczi (hand-rolled) |
| --- | --- | --- | --- |
| Permanent error → `Term()` | ✅ | ❌ | ❌ Nak everywhere |
| Bounded `MaxDeliver` | ✅ | ❌ | ❌ |
| Staged `BackOff` | ✅ 30/60/120s | ❌ | ❌ |
| DLQ (advisory stream) | ✅ | ❌ | ❌ |
| Heartbeat anti-`AckWait` | ✅ | ❌ | ❌ |
| Idempotency | ✅ `event_id` | ❌ | ❌ |
| Reconnect / retry loop | ✅ | ❌ | ❌ |

### Why go-eda's existing primitives did not cover this

`go-eda` already has `JetStreamStore.Subscribe`, but it uses an
**`OrderedConsumer`**: ordered replay for event-sourcing read models, with no
explicit ack and no redelivery. That is the *opposite* of what reczi needed —
a **durable `AckExplicit` work-queue consumer** that processes integration
events as retryable tasks. The two do not overlap; a new package was required.

## Decision

1. **Promote the brick into the shared library.** Add `pkg/consumer` to
   `github.com/lalternative/packages/go/eda`, generalised from synthiz (no synthiz-specific
   deps). It provides, **by default**, all seven guarantees above. The public
   surface a service implements is a 4-method `EventHandler`
   (`Name/Subject/DurableName/MaxDeliver`) plus `Handle` — business logic only.
   Permanent failures are signalled with `consumer.Permanent(err)` /
   `errors.Is(err, consumer.ErrPermanent)`. Idempotency is an injected,
   optional `IdempotencyStore` keyed by `(durable, event_id)`.

2. **The bootstrap consumes it; it does not re-teach it.** The
   `--mode eda` template's `main.go` now starts `consumer.Run` goroutines, and
   ships a working example handler under
   `apps/core/project/events/`. The `_ = js` dead-end is gone. New repos are
   correct by default.

3. **Make the rule explicit** in the template's `CLAUDE.md` and
   `docs/ARCHITECTURE.md`: *never call `js.Subscribe`/`Consume` directly — use
   the brick.*

This is the "both" option: a single shared source of truth **and** a template
that is correct out of the box.

## Consequences

- New EDA services cannot accidentally re-derive the reczi bug: the dangerous
  semantics are not in reach, only `Handle` is.
- One place to fix and improve consumer behaviour for every service.
- `go-eda` must be published (or `replace`d) for consumers to build. The
  template already carries the local-checkout `replace`; publishing
  `go-eda v0.x` removes it.

## Follow-ups

- **reczi**: replace the hand-rolled consumer with `pkg/consumer`. This is the
  fix that closes the original bug; until then reczi remains divergent.
- **synthiz**: migrate `integration-events/consumer.go` onto `pkg/consumer` to
  collapse the duplicate implementation (synthiz keeps its idempotency store by
  implementing `IdempotencyStore`).
- Add an integration test against an embedded `nats-server -js` exercising
  Term/DLQ/redelivery end-to-end (current tests cover the pure logic only).
- Publish `go-eda` and drop the `replace` directive from the template.
