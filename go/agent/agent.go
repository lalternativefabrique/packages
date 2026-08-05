// Package agent defines the shared contract for AI agents across
// l'alternative services (Skalpai, Synthiz, Lungor, ...).
//
// Two execution shapes are exposed:
//
//   - Chat: a single model call, optionally with tools but without an
//     internal loop. The caller is responsible for any orchestration.
//   - Agent: a ReAct-style loop where the model alternates between calling
//     tools and emitting text, until a final answer is produced or
//     MaxSteps is reached.
//
// Tools are provided by the caller and described to the model via JSON
// Schema derived from the Go struct returned by Tool.InputSchema().
package agent

import (
	"context"
	"encoding/json"
	"time"
)

// Role identifies the author of a Message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Tool is what a bounded context implements to expose a capability to the
// model.
//
// Implementations MUST honor ctx.Done() and return ctx.Err() promptly when
// cancelled — the runtime relies on this to abort in-flight tools when the
// session is closed or a global timeout fires.
//
// InputSchema must return a Go value (typically a struct) annotated with
// `jsonschema:"..."` tags. The runtime converts it to JSON Schema and ships
// it to the provider. Tools should validate args defensively even though the
// runtime performs schema-level validation.
type Tool interface {
	Name() string
	Description() string
	InputSchema() any
	Execute(ctx context.Context, args json.RawMessage) (ToolResult, error)
}

// ToolResult is the output of a Tool.Execute call.
//
// Content is what the runtime feeds back to the model as the tool's response.
// Metadata stays runtime-side: it is surfaced to Callbacks (audit log, NATS,
// metrics) but is never injected into the model context. Use Metadata for
// citations, source IDs, latency breakdowns, debug info — anything you want
// to log but not pay tokens for.
type ToolResult struct {
	Content  string
	Metadata map[string]any
}

// Provider is the minimal config needed to instantiate a chat model.
//
// For OpenAI-compatible endpoints (Mistral, Groq, Together, OpenRouter,
// Ollama, vLLM, ...) set Kind="openai-compat" and BaseURL to the provider's
// base URL. For native providers, Kind selects the dedicated adapter:
//   - "anthropic"
//   - "openai"
//   - "mistral"  (uses Mistral's native API rather than openai-compat)
//
// Headers lets callers inject extra HTTP headers (org IDs, beta flags, ...)
// without leaking provider-specific config into the rest of the API.
type Provider struct {
	Kind    string
	APIKey  string
	Model   string
	BaseURL string
	Headers map[string]string
}

// Message is a single entry in the conversation history.
//
// ToolCallID is set when Role == RoleTool and references the assistant turn
// that requested the call. ToolCalls is set when Role == RoleAssistant and
// the model decided to invoke one or more tools.
type Message struct {
	Role       Role
	Content    string
	ToolCallID string
	ToolCalls  []ToolCall
}

// ToolCall is a tool invocation requested by the model.
type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

// Request is the input to a Chat or Agent call.
//
// MaxSteps overrides the Agent's configured default for a single request.
// A zero value means "use the agent default" (configured via WithMaxSteps).
// MaxSteps is ignored by Chat.
type Request struct {
	System   string
	Messages []Message
	Tools    []Tool
	MaxSteps int
}

// Response is the output of a non-streaming call.
//
// ToolCalls is the audit trail of every tool invoked during an Agent run, in
// invocation order. For Chat it is empty (Chat does not loop on tools).
// Usage carries token accounting summed across all model calls in the run.
type Response struct {
	Text      string
	ToolCalls []ToolCallTrace
	Usage     Usage
}

// ToolCallTrace records what happened during a single tool invocation.
//
// Err is stored as a string so the trace can be serialized to JSONB / NATS
// without losing information. An empty Err means the tool succeeded.
type ToolCallTrace struct {
	Name       string
	Arguments  string
	Result     string
	DurationMs int64
	Err        string
}

// Usage reports token consumption for a run.
//
// For Agent runs, the counts are summed across every model call in the loop.
// CachedInputTokens is reported by providers that surface prompt-cache hits
// (Anthropic, OpenAI); zero on providers that don't.
type Usage struct {
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
}

// EventKind enumerates the kinds of streaming events emitted by Chat.Stream
// and Agent.Stream.
type EventKind int

const (
	// EventTextDelta is a chunk of model-generated text.
	EventTextDelta EventKind = iota
	// EventThought is reasoning text emitted between two tool calls (only
	// for providers that surface reasoning explicitly).
	EventThought
	// EventStepStart marks the beginning of a new agent iteration.
	// Useful for UIs ("thinking... step 2/5"). Not emitted by Chat.
	EventStepStart
	// EventToolCallStart fires when the model has decided to call a tool,
	// once arguments are fully assembled.
	EventToolCallStart
	// EventToolCallEnd fires when the tool has returned (success or error).
	EventToolCallEnd
	// EventUsage fires once per model call, after the call completes,
	// reporting token consumption.
	EventUsage
	// EventDone is the last event of a successful stream.
	EventDone
	// EventError is a fatal error; no further events follow.
	EventError
)

// Event is one element of a streaming run.
//
// Only the fields relevant to Kind are populated. Step is set on
// EventStepStart, EventToolCallStart, EventToolCallEnd. Usage fields are set
// on EventUsage. Err is set on EventError.
type Event struct {
	Kind         EventKind
	Text         string
	ToolCallID   string
	ToolName     string
	ToolArgs     string
	ToolResult   string
	Step         int
	InputTokens  int
	OutputTokens int
	CachedInputTokens int
	Err          error
}

// Chat is a single model call (one or more messages, optionally with tools
// but without an internal loop). The caller is responsible for handling tool
// calls and feeding results back if needed.
type Chat interface {
	Run(ctx context.Context, req Request) (*Response, error)
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Agent is a multi-turn ReAct loop (model ↔ tools until a final answer is
// produced or MaxSteps is hit).
type Agent interface {
	Run(ctx context.Context, req Request) (*Response, error)
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Callback is a synchronous observability hook fired by the runtime during a
// run. Use it to publish to NATS, write audit logs, or emit metrics.
//
// Callbacks are called synchronously and MUST return quickly: a slow callback
// blocks the run. For expensive work (DB writes, network), do it in a
// goroutine inside the callback.
type Callback interface {
	OnModelStart(ctx context.Context, iteration int, messages []Message)
	OnModelEnd(ctx context.Context, iteration int, resp *Response, durationMs int64)
	OnToolStart(ctx context.Context, iteration int, call ToolCall)
	OnToolEnd(ctx context.Context, iteration int, call ToolCall, result ToolResult, durationMs int64, err error)
	OnThought(ctx context.Context, iteration int, content string)
	OnUsage(ctx context.Context, iteration int, usage Usage)
}

// NewChat builds a Chat bound to the given provider.
func NewChat(p Provider) (Chat, error) {
	m, err := newChatModel(p)
	if err != nil {
		return nil, err
	}
	return &chatImpl{model: wrapWithRetry(m)}, nil
}

// NewAgent builds an Agent bound to the given provider.
//
// Default configuration:
//   - MaxSteps: 5
//   - ToolTimeout: 30s (per individual tool call)
//   - No callback
//
// Override via opts.
func NewAgent(p Provider, opts ...AgentOption) (Agent, error) {
	m, err := newChatModel(p)
	if err != nil {
		return nil, err
	}
	cfg := agentConfig{
		maxSteps:    defaultMaxSteps,
		toolTimeout: defaultToolTimeout,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &agentImpl{model: wrapWithRetry(m), cfg: cfg}, nil
}

// AgentOption configures an Agent at construction time.
type AgentOption func(*agentConfig)

// WithMaxSteps sets the default maximum number of model↔tool iterations.
// Per-request overrides via Request.MaxSteps take precedence when non-zero.
func WithMaxSteps(n int) AgentOption {
	return func(c *agentConfig) { c.maxSteps = n }
}

// WithCallback registers an observability callback. Only one callback is
// supported; to fan out, implement a multiplexer.
func WithCallback(h Callback) AgentOption {
	return func(c *agentConfig) { c.callback = h }
}

// WithToolTimeout sets a per-tool execution timeout. Zero disables the
// timeout. The runtime wraps each Tool.Execute call with a derived context.
func WithToolTimeout(d time.Duration) AgentOption {
	return func(c *agentConfig) { c.toolTimeout = d }
}

// agentConfig is internal — exposed only via AgentOption setters.
type agentConfig struct {
	maxSteps    int
	callback    Callback
	toolTimeout time.Duration
}

