# Go LLM Agent

Provider-agnostic contract for LLM calls: a single **Chat** call, or a **ReAct
loop** alternating between model and tools. Backed by [Eino](https://github.com/cloudwego/eino),
which stays entirely behind the interface — no `eino` type appears in the
public API.

Module path: `github.com/lalternative/packages/go/agent`.

## Why this exists

The same agent runtime was copy-pasted into Skalpai (`apps/engine/agent`) and
Synthiz (`apps/core/llm/agent`). The two copies had drifted by exactly one
comment. This package is the extraction; Lungor is its first consumer.

## Two execution shapes

```go
chat, err := agent.NewChat(agent.Provider{
    Kind:   "anthropic",
    APIKey: key,
    Model:  "claude-sonnet-5",
})
resp, err := chat.Run(ctx, agent.Request{
    System:   "Rewrite the changelog as a social post.",
    Messages: []agent.Message{{Role: agent.RoleUser, Content: raw}},
})
```

`Chat` is one model call: the caller handles any orchestration. `Agent` adds
the loop — the model calls tools until it emits a final answer or hits
`MaxSteps` (default 5).

```go
ag, err := agent.NewAgent(p,
    agent.WithMaxSteps(8),
    agent.WithToolTimeout(15*time.Second),
    agent.WithCallback(auditLog),
)
```

Both expose `Run` and `Stream`. `Stream` returns a channel of `Event` —
text deltas, step boundaries, tool call start/end, usage, done, error.

## Providers

`Provider.Kind` selects the adapter: `anthropic`, `openai`, `mistral`, or
`openai-compat` with a `BaseURL` for anything speaking the OpenAI protocol
(Groq, Together, OpenRouter, Ollama, vLLM). `Headers` injects extra HTTP
headers without leaking provider specifics into the rest of the API.

Transient failures (429, 5xx, timeouts) are retried with exponential backoff
and jitter, transparently.

## Tools

A tool describes itself with a Go struct; the runtime derives JSON Schema from
`jsonschema:"..."` tags and ships it to the provider.

```go
type Tool interface {
    Name() string
    Description() string
    InputSchema() any
    Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

`ToolResult.Content` goes back to the model. `ToolResult.Metadata` does not —
it reaches `Callback` only, for audit logs, citations or latency breakdowns
you want recorded but not paid for in tokens.

Implementations must honor `ctx.Done()`: the runtime relies on it to abort
in-flight tools when a session closes or the timeout fires.

## Dependency weight

Eino pulls a large transitive tree (AWS SDK, Google API, gRPC, OpenTelemetry).
A service that only needs one-shot text rewriting pays for all of it. That is
a deliberate trade for a single shared runtime; if it ever bites, the fix is a
build-tagged provider split, not a second copy of this package.

## Pending migration

Skalpai and Synthiz still carry their own copies. Until they import this
module, three copies exist. Migrating them is tracked work, not a suggestion:
the value of the extraction is only realised once they are gone.
