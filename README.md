# packages

Standalone SDKs and shared packages for the Skalpai platform.

## skalpai/

| Package | Description |
| --- | --- |
| `sdk-go` | Go SDK — `go get github.com/lalternative/packages/skalpai/sdk-go` |
| `sdk-browser` | Browser SDK (`@digstack/skalpai-sdk-browser`) |
| `sdk-node` | Node SDK (`@digstack/skalpai-sdk-node`) |
| `sdk-feedback-widget` | Vanilla feedback widget (`@digstack/skalpai-feedback-widget`) |
| `sdk-react` | React wrapper for the feedback widget (`@digstack/skalpai-sdk-react`) |
| `waitlist` | Waitlist widget (`@digstack/skalpai-waitlist`) |

JS packages form a single pnpm workspace (`pnpm-workspace.yaml`). `sdk-react`
depends on `sdk-feedback-widget` via `workspace:*`.

## go/

Brand-agnostic Go libraries. Independent Go modules (not part of the pnpm
workspace), each versioned with a path-prefixed tag (e.g. `go/eda/v0.1.1`).

| Package | Description |
| --- | --- |
| `eda` | Event-Driven Architecture toolkit — durable JetStream consumer, outbox, projection, process-manager, CQRS/DDD building blocks. `go get github.com/lalternative/packages/go/eda@go/eda/v0.1.1` |

Submodules `go/eda/pkg/obs/{otelobs,prom}` carry their own `go.mod` (optional
observability adapters) and are tagged independently if needed.
