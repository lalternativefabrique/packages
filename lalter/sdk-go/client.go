// Package sdk is the Go client for the lalter API's app-key-facing surface:
// queuing and reading agent tasks, and driving chat, without a browser
// session.
//
// It exists because every consumer would otherwise write this by hand from
// reading lalter's source — skalpai's core is the first to reach lalter this
// way, and a second consumer would have re-derived the same auth header, the
// same status handling and the same event-stream parsing.
//
// # Where the transport comes from
//
// internal/wire is generated from openapi/lalter.json, lalter's own contract,
// scoped to the tasks and chat tags. It owns every path, method and parameter
// for those two contexts, so none of them is typed by hand here: a route
// renamed upstream becomes a compile error rather than a 404 in production,
// and a field added or renamed surfaces the same way instead of as a value
// silently never read.
//
// It is internal because it is not this package's API. Its methods return raw
// *http.Response and generated pointer types; what is exported wraps them with
// typed errors and the pointer-to-value conversions that keep "task not
// found" distinguishable from "no answer".
//
// Updating after an API change is ./refresh-contract.sh, which fetches
// /openapi.json from a running lalter and regenerates — then reconcile any
// compile error the new shape causes.
package sdk

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/lalter/sdk-go/internal/wire"
)

// DefaultTimeout bounds a single non-streaming call.
//
// Tasks and chat history are read on request paths, so an unbounded wait
// would turn a slow lalter into a slow product. Five seconds is far past a
// healthy response and short enough that a hung backend degrades instead of
// hanging. SendChatMessage does not use it — see its own doc comment.
const DefaultTimeout = 5 * time.Second

// Client talks to a lalter deployment on behalf of ONE app.
//
// The app API key identifies the calling application, never a user: lalter
// resolves the user from whatever the key was issued for, scoped the same way
// the console scopes it.
type Client struct {
	baseURL string
	appKey  string
	http    *http.Client
	// wire is the generated transport: it owns every path and method, so none
	// is written by hand here. Nil when baseURL is blank, which every method
	// already guards with ErrNotConfigured.
	wire *wire.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient supplies the underlying client — for a custom transport, or a
// test server. It replaces the timeout, so set one on the client you pass.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithTimeout overrides DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.http = &http.Client{Timeout: d} }
}

// New builds a client for a lalter deployment.
//
// baseURL is the API root WITHOUT the version segment (https://lalter.example);
// the paths are appended by this package, so a caller cannot pin a version by
// accident and then be surprised when the SDK moves.
//
// A blank baseURL or appKey yields ErrNotConfigured on every call rather than a
// nil client: a deployment that has not wired lalter yet should degrade, not
// panic at boot.
func New(baseURL, appKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		appKey:  appKey,
		http:    &http.Client{Timeout: DefaultTimeout},
	}
	for _, o := range opts {
		o(c)
	}
	c.wire = newWire(c.baseURL, c.appKey, c.http)
	return c
}

// newWire builds the generated transport. Its error is dropped on purpose: it
// can only come from a ClientOption, and none is passed here — surfacing it
// would force New to return an error for a case that cannot arise, when a
// blank baseURL is already handled by every method returning ErrNotConfigured.
//
// Unlike lungor/sdk-go, the version segment is NOT appended here: lalter's
// swag annotations already write `@Router /api/v1/tasks` (lungor's write
// `@Router /entitlements`), so the generated paths in openapi/lalter.json
// already carry /api/v1 — appending it again would call /api/v1/api/v1/tasks.
// TestEveryOperationIsVersioned asserts the resulting paths directly, so a
// change to either side shows up as a test failure rather than a 404.
func newWire(baseURL, appKey string, doer wire.HttpRequestDoer) *wire.Client {
	if baseURL == "" {
		return nil
	}
	// Authentication is attached once, here, rather than per call: the app key
	// identifies the app on every request, and a method that forgot it would
	// read as a rejected key rather than as the omission it is.
	auth := wire.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+appKey)
		req.Header.Set("Accept", "application/json")
		return nil
	})
	w, err := wire.NewClient(baseURL, wire.WithHTTPClient(doer), auth)
	if err != nil {
		return nil
	}
	return w
}

// Errors the caller is expected to branch on.
var (
	// ErrNotConfigured — no base URL or no app key. The call was never made.
	ErrNotConfigured = errors.New("lalter: not configured")
	// ErrUnauthorized — the app key was rejected (401/403). Operator error, not
	// a statement about the task or conversation asked for.
	ErrUnauthorized = errors.New("lalter: unauthorized")
	// ErrBadRequest — lalter refused the arguments (400). A bug in the caller.
	ErrBadRequest = errors.New("lalter: bad request")
	// ErrUnavailable — transport failure or a 5xx. Transient; retrying later is
	// reasonable, and degrading is safer than failing the caller's request.
	ErrUnavailable = errors.New("lalter: unavailable")
	// ErrNotFound — no task or conversation with that id.
	ErrNotFound = errors.New("lalter: not found")
)

// Usage is what a task or a chat turn cost.
type Usage struct {
	InputTokens       int64
	CachedInputTokens int64
	OutputTokens      int64
	Steps             int
}

// TaskStatus values lalter reports. Kept as plain strings rather than an enum:
// lalter is the single authority on what a task's lifecycle looks like, and
// pinning a closed set here would make a new status a compile error in every
// consumer instead of a value they can still read and log.
const (
	TaskStatusQueued  = "queued"
	TaskStatusRunning = "running"
	TaskStatusDone    = "done"
	TaskStatusFailed  = "failed"
)

// Task is an agent run as an API consumer sees it.
type Task struct {
	ID        string
	Kind      string
	Prompt    string
	BaseRef   string
	Status    string
	Model     string
	Diff      string
	Summary   string
	Error     string
	Usage     Usage
	CreatedAt string
	StartedAt string
	SettledAt string
}

// CreateTaskInput asks for one agent run.
//
// RepoURL carries its own credentials — https://x-access-token:<pat>@github.com/…
// — which is how the caller grants access to a private repository without
// lalter holding anyone's token. It is never echoed back by Task.
type CreateTaskInput struct {
	Kind    string
	Prompt  string
	RepoURL string
	BaseRef string
}

// CreateTask queues an agent run over a repository and returns as soon as it
// is queued — a run takes minutes, and waiting for it here would time out on
// work that later succeeded. Poll GetTask, or read Chat instead if a live
// account already streams events.
func (c *Client) CreateTask(ctx context.Context, in CreateTaskInput) (Task, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Task{}, ErrNotConfigured
	}
	if in.Kind == "" || in.Prompt == "" || in.RepoURL == "" {
		return Task{}, fmt.Errorf("%w: kind, prompt and repo_url are required", ErrBadRequest)
	}

	body := wire.CreateTaskJSONRequestBody{
		Kind:    &in.Kind,
		Prompt:  &in.Prompt,
		RepoUrl: &in.RepoURL,
	}
	if in.BaseRef != "" {
		body.BaseRef = &in.BaseRef
	}

	var out wire.TaskTaskDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.CreateTask(ctx, body)
	}); err != nil {
		return Task{}, err
	}
	return taskFrom(out), nil
}

// GetTask reads one task by id.
func (c *Client) GetTask(ctx context.Context, id string) (Task, error) {
	if c.baseURL == "" || c.appKey == "" {
		return Task{}, ErrNotConfigured
	}
	if id == "" {
		return Task{}, fmt.Errorf("%w: empty task id", ErrBadRequest)
	}

	var out wire.TaskTaskDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetTask(ctx, id)
	}); err != nil {
		return Task{}, err
	}
	return taskFrom(out), nil
}

// ListTasks lists the caller's tasks, most recent first.
func (c *Client) ListTasks(ctx context.Context) ([]Task, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}

	var out []wire.TaskTaskDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.ListTasks(ctx)
	}); err != nil {
		return nil, err
	}
	tasks := make([]Task, 0, len(out))
	for _, t := range out {
		tasks = append(tasks, taskFrom(t))
	}
	return tasks, nil
}

// Step is one tool call the agent made while running a task.
type Step struct {
	Seq        int
	Tool       string
	Arguments  string
	Result     string
	Error      string
	DurationMs int64
	At         string
}

// GetTaskSteps reads what the agent did for one task, one entry per tool call.
func (c *Client) GetTaskSteps(ctx context.Context, id string) ([]Step, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}
	if id == "" {
		return nil, fmt.Errorf("%w: empty task id", ErrBadRequest)
	}

	var out []wire.TaskStepDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetTaskSteps(ctx, id)
	}); err != nil {
		return nil, err
	}
	steps := make([]Step, 0, len(out))
	for _, s := range out {
		steps = append(steps, stepFrom(s))
	}
	return steps, nil
}

// taskFrom converts the generated wire type into the one callers use.
//
// swag emits OpenAPI 2.0, which has no `required`, so every generated field is
// a pointer. Those pointers must not reach the caller: a nil Status would be
// indistinguishable from an empty one at the call site, where only one of them
// is a real answer.
func taskFrom(w wire.TaskTaskDTO) Task {
	out := Task{}
	if w.Id != nil {
		out.ID = *w.Id
	}
	if w.Kind != nil {
		out.Kind = *w.Kind
	}
	if w.Prompt != nil {
		out.Prompt = *w.Prompt
	}
	if w.BaseRef != nil {
		out.BaseRef = *w.BaseRef
	}
	if w.Status != nil {
		out.Status = *w.Status
	}
	if w.Model != nil {
		out.Model = *w.Model
	}
	if w.Diff != nil {
		out.Diff = *w.Diff
	}
	if w.Summary != nil {
		out.Summary = *w.Summary
	}
	if w.Error != nil {
		out.Error = *w.Error
	}
	if w.Usage != nil {
		if w.Usage.InputTokens != nil {
			out.Usage.InputTokens = int64(*w.Usage.InputTokens)
		}
		if w.Usage.CachedInputTokens != nil {
			out.Usage.CachedInputTokens = int64(*w.Usage.CachedInputTokens)
		}
		if w.Usage.OutputTokens != nil {
			out.Usage.OutputTokens = int64(*w.Usage.OutputTokens)
		}
		if w.Usage.Steps != nil {
			out.Usage.Steps = *w.Usage.Steps
		}
	}
	if w.CreatedAt != nil {
		out.CreatedAt = *w.CreatedAt
	}
	if w.StartedAt != nil {
		out.StartedAt = *w.StartedAt
	}
	if w.SettledAt != nil {
		out.SettledAt = *w.SettledAt
	}
	return out
}

func stepFrom(w wire.TaskStepDTO) Step {
	out := Step{}
	if w.Seq != nil {
		out.Seq = *w.Seq
	}
	if w.Tool != nil {
		out.Tool = *w.Tool
	}
	if w.Arguments != nil {
		out.Arguments = *w.Arguments
	}
	if w.Result != nil {
		out.Result = *w.Result
	}
	if w.Error != nil {
		out.Error = *w.Error
	}
	if w.DurationMs != nil {
		out.DurationMs = int64(*w.DurationMs)
	}
	if w.At != nil {
		out.At = *w.At
	}
	return out
}

// Conversation is a chat thread as an API consumer sees it.
type Conversation struct {
	ID        string
	Title     string
	HasRepo   bool
	BaseRef   string
	CreatedAt string
	UpdatedAt string
}

// Message is one turn in a conversation.
type Message struct {
	ID       string
	Role     string
	Content  string
	ToolName string
	ToolArgs string
	ToolMeta string
	At       string
}

// ListConversations lists the caller's conversations, most recent first.
func (c *Client) ListConversations(ctx context.Context) ([]Conversation, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}

	var out []wire.ChatConversationDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.ListConversations(ctx)
	}); err != nil {
		return nil, err
	}
	list := make([]Conversation, 0, len(out))
	for _, conv := range out {
		list = append(list, conversationFrom(conv))
	}
	return list, nil
}

// GetConversationMessages reads a conversation's turns, oldest first.
func (c *Client) GetConversationMessages(ctx context.Context, conversationID string) ([]Message, error) {
	if c.baseURL == "" || c.appKey == "" {
		return nil, ErrNotConfigured
	}
	if conversationID == "" {
		return nil, fmt.Errorf("%w: empty conversation id", ErrBadRequest)
	}

	var out []wire.ChatMessageDTO
	if err := c.send(ctx, &out, func() (*http.Response, error) {
		return c.wire.GetConversationMessages(ctx, conversationID)
	}); err != nil {
		return nil, err
	}
	list := make([]Message, 0, len(out))
	for _, m := range out {
		list = append(list, messageFrom(m))
	}
	return list, nil
}

func conversationFrom(w wire.ChatConversationDTO) Conversation {
	out := Conversation{}
	if w.Id != nil {
		out.ID = *w.Id
	}
	if w.Title != nil {
		out.Title = *w.Title
	}
	if w.HasRepo != nil {
		out.HasRepo = *w.HasRepo
	}
	if w.BaseRef != nil {
		out.BaseRef = *w.BaseRef
	}
	if w.CreatedAt != nil {
		out.CreatedAt = *w.CreatedAt
	}
	if w.UpdatedAt != nil {
		out.UpdatedAt = *w.UpdatedAt
	}
	return out
}

func messageFrom(w wire.ChatMessageDTO) Message {
	out := Message{}
	if w.Id != nil {
		out.ID = *w.Id
	}
	if w.Role != nil {
		out.Role = *w.Role
	}
	if w.Content != nil {
		out.Content = *w.Content
	}
	if w.ToolName != nil {
		out.ToolName = *w.ToolName
	}
	if w.ToolArgs != nil {
		out.ToolArgs = *w.ToolArgs
	}
	if w.ToolMeta != nil {
		out.ToolMeta = *w.ToolMeta
	}
	if w.At != nil {
		out.At = *w.At
	}
	return out
}

// SendChatMessageInput is one turn from the caller.
//
// ConversationID is empty to open a new thread — the reply's first event
// carries the id lalter assigned it.
type SendChatMessageInput struct {
	ConversationID string
	Message        string
	// RepoURL points a new conversation at a repository, credentials
	// included. Ignored once the thread exists — a thread works on one
	// repository throughout its history.
	RepoURL string
	BaseRef string
}

// ChatEventKind names what a streamed ChatEvent carries.
type ChatEventKind string

// Kinds lalter's chat stream emits. Kept as constants so a typo in a
// comparison is a compile error rather than a case that silently never
// matches.
const (
	// ChatEventConversation carries the conversation id in Text, once, as the
	// first event of a new thread.
	ChatEventConversation ChatEventKind = "conversation"
	// ChatEventDelta carries one fragment of the reply in Text, as it is
	// produced.
	ChatEventDelta ChatEventKind = "delta"
	// ChatEventToolStart/ChatEventToolEnd bracket one tool call: Tool names
	// it, Args is its input, and ChatEventToolEnd additionally carries Result
	// (and Meta when the tool returned structured metadata for a card instead
	// of raw text).
	ChatEventToolStart ChatEventKind = "tool_start"
	ChatEventToolEnd   ChatEventKind = "tool_end"
	// ChatEventMessage carries the whole turn's reply in Text, once, after
	// streaming completes.
	ChatEventMessage ChatEventKind = "message"
	// ChatEventError carries the failure text in Err. Last event of the
	// stream when it fires.
	ChatEventError ChatEventKind = "error"
	ChatEventDone  ChatEventKind = "done"
	// ChatEventEvict fires when a stale tool result is dropped from history
	// to free context; Tokens is how many were freed.
	ChatEventEvict ChatEventKind = "evict"
	// ChatEventCompactStart/ChatEventCompactEnd bracket a compaction: the
	// model summarizes older turns because the conversation is approaching
	// its context window. CompactStart carries Tokens (usage against the
	// threshold); CompactEnd carries TokensBefore/TokensAfter.
	ChatEventCompactStart ChatEventKind = "compact_start"
	ChatEventCompactEnd   ChatEventKind = "compact_end"
)

// ChatEvent is one Server-Sent Event from POST /chat/send. Only the fields
// relevant to Kind are set; the rest are zero.
type ChatEvent struct {
	Kind ChatEventKind
	// Text carries the conversation id, a reply fragment, or the whole reply,
	// depending on Kind.
	Text string
	// Tool, Args and Result are set on ChatEventToolStart/ChatEventToolEnd.
	// Meta is set on ChatEventToolEnd when the tool returned structured
	// metadata.
	Tool   string
	Args   string
	Result string
	Meta   string
	// Err carries the failure text when Kind == ChatEventError.
	Err string
	// Tokens is set on ChatEventEvict and ChatEventCompactStart.
	Tokens int
	// TokensBefore/TokensAfter are set on ChatEventCompactEnd.
	TokensBefore int
	TokensAfter  int
}

// SendChatMessage sends a message and streams the reply, calling onEvent for
// each event as it arrives.
//
// oapi-codegen has no notion of Server-Sent Events — it generates a client for
// request/response JSON, not for a body meant to be read incrementally — so
// this method reads the generated call's raw *http.Response body itself
// rather than decoding it as one JSON value. Everything else about the
// request (path, method, auth) still comes from the generated transport.
//
// It ignores DefaultTimeout: a chat turn can run for as long as the agent
// takes, which is exactly what a fixed timeout would cut off mid-reply. Bound
// it with ctx instead if the caller needs one.
func (c *Client) SendChatMessage(ctx context.Context, in SendChatMessageInput, onEvent func(ChatEvent)) error {
	if c.baseURL == "" || c.appKey == "" {
		return ErrNotConfigured
	}
	if strings.TrimSpace(in.Message) == "" {
		return fmt.Errorf("%w: message is empty", ErrBadRequest)
	}

	body := wire.SendChatMessageJSONRequestBody{Message: &in.Message}
	if in.ConversationID != "" {
		body.ConversationId = &in.ConversationID
	}
	if in.RepoURL != "" {
		body.RepoUrl = &in.RepoURL
	}
	if in.BaseRef != "" {
		body.BaseRef = &in.BaseRef
	}

	if c.wire == nil {
		return ErrNotConfigured
	}
	resp, err := c.wire.SendChatMessage(ctx, body)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if mapped := mapStatus(resp.StatusCode); mapped != nil {
		return fmt.Errorf("%w: %s", mapped, snippet(resp.Body))
	}

	return readSSE(resp.Body, onEvent)
}

// readSSE parses a text/event-stream body one "data: <json>\n\n" frame at a
// time, decoding each frame into the shape lalter's chat handler emits.
//
// Decoding into named fields here, rather than exposing the raw bytes, is
// what lets a field rename on lalter's side become a compile error in this
// package instead of a silently-dropped value in every consumer.
func readSSE(body io.Reader, onEvent func(ChatEvent)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var raw struct {
			Kind         string `json:"kind"`
			Text         string `json:"text,omitempty"`
			Tool         string `json:"tool,omitempty"`
			Args         string `json:"args,omitempty"`
			Result       string `json:"result,omitempty"`
			Meta         string `json:"meta,omitempty"`
			Err          string `json:"error,omitempty"`
			Tokens       int    `json:"tokens,omitempty"`
			TokensBefore int    `json:"tokens_before,omitempty"`
			TokensAfter  int    `json:"tokens_after,omitempty"`
		}
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}
		onEvent(ChatEvent{
			Kind: ChatEventKind(raw.Kind), Text: raw.Text,
			Tool: raw.Tool, Args: raw.Args, Result: raw.Result, Meta: raw.Meta,
			Err: raw.Err, Tokens: raw.Tokens,
			TokensBefore: raw.TokensBefore, TokensAfter: raw.TokensAfter,
		})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: reading event stream: %v", ErrUnavailable, err)
	}
	return nil
}

// send issues one request and decodes the response.
//
// Every non-2xx is mapped onto one of this package's errors, so callers
// branch on meaning rather than on status codes.
func (c *Client) send(ctx context.Context, out any, call func() (*http.Response, error)) error {
	if c.wire == nil {
		return ErrNotConfigured
	}

	resp, err := call()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	if mapped := mapStatus(resp.StatusCode); mapped != nil {
		return fmt.Errorf("%w: %s", mapped, snippet(resp.Body))
	}

	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: decoding response: %v", ErrUnavailable, err)
	}
	return nil
}

// mapStatus reports the package error a status code means, or nil for a
// successful response.
func mapStatus(status int) error {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return ErrUnauthorized
	case status == http.StatusNotFound:
		return ErrNotFound
	case status == http.StatusBadRequest:
		return ErrBadRequest
	case status >= 500:
		return fmt.Errorf("%w: status %d", ErrUnavailable, status)
	case status >= 300:
		return fmt.Errorf("%w: unexpected status %d", ErrUnavailable, status)
	}
	return nil
}

// snippet reads a bounded prefix of an error body, so a misbehaving server
// cannot put an unbounded string into the caller's logs.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil || len(b) == 0 {
		return "no detail"
	}
	return strings.TrimSpace(string(b))
}
