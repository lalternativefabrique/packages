## Revue - checklist actionnable

Date initiale: 2026-01-23
Dernière mise à jour: 2026-05-20

Format:
- [x] / [ ] [SEVERITY] fichier:ligne - description
  - Problem / Impact / Fix
  - Status: comment / where it was addressed.

---

- [x] [HIGH] pkg/db/event_store.go:83 - Concurrence optimiste ignorée
  - Status: résolu dans l'event store legacy (event_store.go:100-109 via `getAggregateState` + `ExpectLastSequencePerSubject`) et reproduit dans le nouveau `JetStreamStore[ID]` (pkg/db/jetstream_store.go) avec `lastAggregateState` + `WithExpectLastSequencePerSubject`. `InMemoryStore[ID]` enforce OCC + versions contiguës.

- [x] [HIGH] pkg/db/event_store.go:88 - Event-Type manquant à la publication
  - Status: résolu. `event_store.go:126` pose le header; `jetstream_store.go` pose `Event-Type` + `Event-Id` + `Aggregate-*` + `Tenant-Id` + `Correlation-Id` + `Causation-Id`.

- [x] [HIGH] pkg/db/event_store.go:116 - Relecture JetStream incorrecte
  - Status: résolu. Le legacy utilise `PullSubscribe + StartSequence` (event_store.go:155-209). Le nouveau store utilise un `OrderedConsumer` (v2 API) avec `FilterSubjects` + `DeliverAllPolicy`.

- [x] [HIGH] pkg/db/event_store.go:168 - PullSubscribe avec sujet invalide
  - Status: résolu. Le nouveau code utilise `OrderedConsumer` (v2). Le legacy `GetAllEvents` est resté en place mais déprécié.

- [x] [MEDIUM] pkg/db/event_store.go:23 - ReconnectWait en nanosecondes
  - Status: résolu (`event_store.go:26` `nats.ReconnectWait(2*time.Second)`).

- [x] [MEDIUM] pkg/cqrs/cqrs.go:18 - Command ID jamais renseigné
  - Status: résolu (`NewCommand` génère un UUID via `uuid.New().String()`).

- [x] [MEDIUM] pkg/cqrs/cqrs.go:38 - Query ID jamais renseigné
  - Status: résolu (`NewQuery` génère un UUID).

- [x] [MEDIUM] pkg/cqrs/cqrs.go:90 - Buses non thread-safe
  - Status: résolu. Les buses legacy ont un `sync.RWMutex`. Le nouveau `CommandRouter`/`QueryRouter` est protégé par `sync.RWMutex`, le `TypedEventBus[ID]` aussi.

- [x] [LOW] pkg/module/registry.go:119 - Exposition de map interne
  - Status: ouvert mais déprécié — le package `module` est conservé en legacy. La nouvelle approche utilise `pkg/di` (registry typé, scopes, lifecycle).

- [x] [MEDIUM] Tests manquants
  - Status: résolu. Couverture actuelle:
    - `pkg/di` — 9 tests (Singleton, Transient, Scoped, cycles, tagged, lifecycle, ProvideValue, errors)
    - `pkg/ddd` — 4 tests (Raise, MarkCommitted, LoadFromHistory, envelope options)
    - `pkg/db` — InMemoryStore (Save/Load/OCC/non-contiguous/LoadFromVersion/Subscribe/snapshot round-trip) + tests legacy
    - `pkg/cqrs` — 8 tests typés (Execute/Ask/NoHandler/middlewares: Recovery/Timeout/Retry/Chain order/EventBus) + tests legacy
    - `pkg/obs` — 3 tests (metrics/tracing/logging middlewares)
    - `examples/banking` — programme runnable validé manuellement (balance=350)

---

## Suivi PR1 (refactor "petits oignons", 2026-05-20)

- [x] go.mod bumped à go 1.23, deps NATS / google/uuid / testify à jour
- [x] `pkg/di/typed.go` — Registry generics, lifetimes, scopes, qualifiers, lifecycle hooks, cycles
- [x] `pkg/ddd/generic.go` + `clock.go` — `Aggregate[ID]`, `EventEnvelope[ID]`, `Raise`, `LoadFromHistory`
- [x] `pkg/db/jetstream_store.go` — JetStream v2, OCC, idempotence, `Subscribe`
- [x] `pkg/db/snapshot.go` — `SnapshotStore[ID]` + impl mémoire + impl KV JetStream
- [x] `pkg/db/inmem_store.go` — store mémoire typé (OCC + contiguïté)
- [x] `pkg/cqrs/typed.go` — `TypedCommandHandler[C,R]`, `TypedQueryHandler[Q,R]`, middleware chain, erreurs typées, `TypedEventBus[ID]`
- [x] `pkg/logger/slog.go` — adaptateur slog
- [x] `pkg/obs/{obs,cqrs_middleware}.go` — interfaces `Meter`/`Tracer` + middlewares
- [x] `examples/banking/main.go` — exemple runnable de bout en bout

## Suivi PR2 (outbox / projections / process manager, 2026-05-20)

- [x] `pkg/outbox/outbox.go` — Store interface (storage-agnostic), Relay (RunOnce + Run), retries with terminal status, in-memory Store
- [x] `pkg/projection/projection.go` — Projector, CheckpointStore (in-mem), Manager (CatchUp + RunLive), idempotency by per-aggregate version
- [x] `pkg/processmanager/processmanager.go` — Definition, Engine, Stored[S] with OCC, idempotency by LastEventID, Effects executed after persistence, in-memory StateStore

Tests (PR2): outbox (4), projection (4), processmanager (5). `go test -race ./...` reste vert sur l'ensemble.

## Suivi PR3 (KV adapters + exemple end-to-end, 2026-05-20)

- [x] `examples/orders/main.go` — exemple end-to-end: aggregate Order + InMemoryStore + outbox Relay + TypedEventBus + projection Manager (inventory) + processmanager Engine (shipping). Tourne avec `go run ./examples/orders` et produit `shipped=3 / inventory OK / outbox dispatched=6`.
- [x] `pkg/projection/kv_checkpoint.go` — `KVCheckpointStore[ID]` basé sur `jetstream.KeyValue` (JSON encoding, History=1)
- [x] `pkg/processmanager/kv_state.go` — `KVStateStore[S]` basé sur `jetstream.KeyValue` avec OCC via revision (`Create` / `Update` avec last-revision)

## Suivi PR4 (integration tests + rename + adapters + legacy cleanup, 2026-05-20)

- [x] Integration tests `//go:build integration` couvrant `JetStreamStore`, `KVCheckpointStore`, `KVStateStore`. Auto-skip si NATS pas accessible. README documente la commande Docker.
- [x] Module path renommé: `github.com/yourusername/go-generic-event-driven` → `github.com/lalternative/packages/go/eda`
- [x] `pkg/obs/prom` — adapter Prometheus pour `Meter` (counters + histograms)
- [x] `pkg/obs/otelobs` — adapter OpenTelemetry pour `Tracer`
- [x] Suppression du legacy:
  - `pkg/types/` (entier)
  - `pkg/module/` (entier)
  - `pkg/di/container.go` + `container_test.go` + `container_integration_test.go`
  - `pkg/cqrs/cqrs.go` + `cqrs_test.go`
  - `pkg/ddd/interfaces.go` + `base.go`
  - `pkg/db/event_store.go` + `event_store_test.go` + `nats_event_store_integration_test.go`
  - `pkg/logger/logger.go` (interface + helpers déplacés dans `slog.go`)
  - `examples/simple_di_example.go`

## Suivi PR5 (sous-modules obs, 2026-05-20)

- [x] `pkg/obs/prom/go.mod` et `pkg/obs/otelobs/go.mod` — modules Go séparés avec leurs propres `replace ../../..` pour le développement local
- [x] Core `go.mod` purgé de Prometheus + OpenTelemetry (deps directes ET indirectes)
- [x] README documente la création locale d'un `go.work` (le fichier est gitignoré)

Core deps directes restantes: `google/uuid`, `nats-io/nats.go`, `stretchr/testify`. Plus aucune dep optionnelle.

## À faire (PRs suivantes)

- [ ] SQL implementation de `outbox.Store` (Postgres/MySQL avec `SELECT ... FOR UPDATE SKIP LOCKED`)
- [ ] CI sklp (`.sklp/tasks/ci.yaml`) — `go test -race ./...` + `go vet` sur les 3 modules
- [ ] Exemple multi-tenant utilisant les Scopes du DI
- [ ] Déflaker l'exemple orders (race entre outbox dispatch et PM workers)
