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
| `sdk-feedback-native` | Feedback widget for React Native / Expo, iOS and Android (`@lalternative/skalpai-feedback-native`) |

JS packages form a single pnpm workspace (`pnpm-workspace.yaml`). `sdk-react`
depends on `sdk-feedback-widget` via `workspace:*`.

## go/

Brand-agnostic Go libraries. Independent Go modules (not part of the pnpm
workspace), each versioned with a path-prefixed tag (e.g. `go/fileguard/v0.5.0`).

| Package | Description |
| --- | --- |
| `busevents` | The shared bus contract — subjects each product publishes under `events.<product>.>` and their payloads, for producers and consumers alike. `go get github.com/lalternative/packages/go/busevents@go/busevents/v0.1.0` |
| `meet` | Server side of a LiveKit video call — role-scoped tokens, stage pinned in room metadata, webhooks, screen-share limits. UI in `meet/` (`@lalternative/meet`). `go get github.com/lalternative/packages/go/meet@go/meet/v0.1.0` |
| `transcribe` | Speech-to-text pipeline — ffmpeg chunking, parallel transcription with retry and Retry-After, duration and size gates, OpenAI/Groq/whisper.cpp providers. Needs ffmpeg and ffprobe on the host. `go get github.com/lalternative/packages/go/transcribe@go/transcribe/v0.1.0` |

The event-driven toolkit moved to [eventkit](https://github.com/lalternativefabrique/eventkit); search, fileguard, retrieval and language moved to [searchkit](https://github.com/lalternativefabrique/searchkit).

## spore/

The Spore SDKs moved to [spore-sdk](https://github.com/lalternativefabrique/spore-sdk).
