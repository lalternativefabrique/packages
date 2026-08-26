package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func ctx() context.Context { return context.Background() }

type recorded struct {
	path   string
	method string
	auth   string
	body   map[string]any
}

func server(t *testing.T, status int, payload any) (*httptest.Server, *recorded) {
	t.Helper()
	rec := &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.RequestURI()
		rec.method = r.Method
		rec.auth = r.Header.Get("Authorization")
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&rec.body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			_ = json.NewEncoder(w).Encode(payload)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestCreateTask_SendsTheAppKeyAndTheBody(t *testing.T) {
	srv, rec := server(t, 202, map[string]any{
		"id": "t-1", "kind": "fix", "status": "queued",
	})
	c := New(srv.URL, "lalter_sk_x")

	got, err := c.CreateTask(ctx(), CreateTaskInput{
		Kind: "fix", Prompt: "the ledger double-credits a self-transfer",
		RepoURL: "https://x-access-token:pat@github.com/acme/app.git",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.ID != "t-1" || got.Status != "queued" {
		t.Fatalf("task = %+v", got)
	}
	if rec.auth != "Bearer lalter_sk_x" {
		t.Fatalf("auth = %q, want a bearer app key", rec.auth)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/tasks" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	if rec.body["repo_url"] == nil {
		t.Fatal("repo_url was not sent")
	}
}

func TestCreateTask_SendsMCPServerNames(t *testing.T) {
	srv, rec := server(t, 202, map[string]any{"id": "t-1", "status": "queued"})
	c := New(srv.URL, "k")

	if _, err := c.CreateTask(ctx(), CreateTaskInput{
		Kind: "fix", Prompt: "p", RepoURL: "https://example.test/r.git",
		MCPServers: []string{"skalpai-logs"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, ok := rec.body["mcp_servers"].([]any)
	if !ok || len(got) != 1 || got[0] != "skalpai-logs" {
		t.Fatalf("mcp_servers sent = %v", rec.body["mcp_servers"])
	}
}

// A request that names no MCP server must not send the field at all — an
// omitted field and an explicit empty list read the same to lalter, but
// omitting it is what every other zero-value optional field in this SDK
// already does.
func TestCreateTask_OmitsMCPServersWhenNoneRequested(t *testing.T) {
	srv, rec := server(t, 202, map[string]any{"id": "t-1", "status": "queued"})
	c := New(srv.URL, "k")

	if _, err := c.CreateTask(ctx(), CreateTaskInput{
		Kind: "fix", Prompt: "p", RepoURL: "https://example.test/r.git",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, sent := rec.body["mcp_servers"]; sent {
		t.Fatal("mcp_servers was sent despite none being requested")
	}
}

// A registry that rejects the name reads back through the same error path as
// any other 400 — a caller must not have to special-case this refusal.
func TestCreateTask_RefusesAnUnregisteredMCPServerName(t *testing.T) {
	srv, _ := server(t, http.StatusBadRequest, map[string]string{
		"error": `unknown MCP server "not-registered"`,
	})
	c := New(srv.URL, "k")

	_, err := c.CreateTask(ctx(), CreateTaskInput{
		Kind: "fix", Prompt: "p", RepoURL: "https://example.test/r.git",
		MCPServers: []string{"not-registered"},
	})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestGetTask_ReportsTheMCPServersGranted(t *testing.T) {
	srv, _ := server(t, 200, map[string]any{
		"id": "t-1", "status": "running", "mcp_servers": []string{"skalpai-logs"},
	})
	c := New(srv.URL, "k")

	got, err := c.GetTask(ctx(), "t-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.MCPServers) != 1 || got.MCPServers[0] != "skalpai-logs" {
		t.Fatalf("mcp servers = %v", got.MCPServers)
	}
}

func TestCreateTask_RefusesIncompleteInput(t *testing.T) {
	c := New("http://x", "k")

	if _, err := c.CreateTask(ctx(), CreateTaskInput{Prompt: "p", RepoURL: "r"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing kind: err = %v", err)
	}
	if _, err := c.CreateTask(ctx(), CreateTaskInput{Kind: "fix", RepoURL: "r"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing prompt: err = %v", err)
	}
	if _, err := c.CreateTask(ctx(), CreateTaskInput{Kind: "fix", Prompt: "p"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing repo url: err = %v", err)
	}
}

func TestGetTask_MapsNotFound(t *testing.T) {
	srv, _ := server(t, http.StatusNotFound, map[string]string{"error": "task not found"})
	c := New(srv.URL, "k")

	_, err := c.GetTask(ctx(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListTasks_ReturnsAnEmptySliceNotNil(t *testing.T) {
	srv, _ := server(t, 200, []any{})
	c := New(srv.URL, "k")

	got, err := c.ListTasks(ctx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("ListTasks returned nil, want an empty slice")
	}
}

func TestGetTaskSteps_ConvertsDurationToInt64(t *testing.T) {
	srv, _ := server(t, 200, []map[string]any{
		{"seq": 1, "tool": "read_file", "duration_ms": 42},
	})
	c := New(srv.URL, "k")

	got, err := c.GetTaskSteps(ctx(), "t-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].DurationMs != 42 {
		t.Fatalf("steps = %+v", got)
	}
}

// A rejected app key must never be confused with a normal "not found": one is
// an operator mistake, the other says nothing about whether the task exists.
func TestTask_RejectedKeyIsNotNotFound(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := server(t, status, nil)
		c := New(srv.URL, "wrong")

		_, err := c.GetTask(ctx(), "t-1")
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("status %d: err = %v, want ErrUnauthorized", status, err)
		}
		if errors.Is(err, ErrNotFound) {
			t.Fatal("a rejected key must not read as a missing task")
		}
	}
}

func TestClient_RefusesToCallWhenUnconfigured(t *testing.T) {
	if _, err := New("", "k").GetTask(ctx(), "t"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no base URL: err = %v", err)
	}
	if _, err := New("http://x", "").GetTask(ctx(), "t"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("no app key: err = %v", err)
	}
	if err := New("", "k").SendChatMessage(ctx(), SendChatMessageInput{Message: "hi"}, func(ChatEvent) {}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("SendChatMessage, no base URL: err = %v", err)
	}
}

func TestListConversations_SendsTheAppKey(t *testing.T) {
	srv, rec := server(t, 200, []map[string]any{
		{"id": "c-1", "title": "fix the ledger"},
	})
	c := New(srv.URL, "lalter_sk_x")

	got, err := c.ListConversations(ctx())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "c-1" {
		t.Fatalf("conversations = %+v", got)
	}
	if rec.auth != "Bearer lalter_sk_x" {
		t.Fatalf("auth = %q", rec.auth)
	}
}

func TestGetConversationMessages_RefusesAnEmptyID(t *testing.T) {
	c := New("http://x", "k")
	if _, err := c.GetConversationMessages(ctx(), ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestSendChatMessage_RefusesAnEmptyMessage(t *testing.T) {
	c := New("http://x", "k")
	err := c.SendChatMessage(ctx(), SendChatMessageInput{}, func(ChatEvent) {})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestSendChatMessage_StreamsEachEventKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		frames := []string{
			`{"kind":"conversation","text":"c-1"}`,
			`{"kind":"delta","text":"the "}`,
			`{"kind":"delta","text":"answer"}`,
			`{"kind":"tool_start","tool":"read_file","args":"{\"path\":\"a.go\"}"}`,
			`{"kind":"tool_end","tool":"read_file","result":"package main"}`,
			`{"kind":"evict","tokens":512}`,
			`{"kind":"compact_start","tokens":8000}`,
			`{"kind":"compact_end","tokens_before":8000,"tokens_after":2000}`,
			`{"kind":"message","text":"the answer"}`,
			`{"kind":"done"}`,
		}
		for _, f := range frames {
			_, _ = w.Write([]byte("data: " + f + "\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	var got []ChatEvent
	err := c.SendChatMessage(ctx(), SendChatMessageInput{Message: "hi"}, func(e ChatEvent) {
		got = append(got, e)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 10 {
		t.Fatalf("got %d events, want 10: %+v", len(got), got)
	}
	if got[0].Kind != ChatEventConversation || got[0].Text != "c-1" {
		t.Fatalf("event 0 = %+v", got[0])
	}
	if got[3].Kind != ChatEventToolStart || got[3].Tool != "read_file" {
		t.Fatalf("event 3 = %+v", got[3])
	}
	if got[4].Kind != ChatEventToolEnd || got[4].Result != "package main" {
		t.Fatalf("event 4 = %+v", got[4])
	}
	if got[5].Kind != ChatEventEvict || got[5].Tokens != 512 {
		t.Fatalf("event 5 = %+v", got[5])
	}
	if got[7].Kind != ChatEventCompactEnd || got[7].TokensBefore != 8000 || got[7].TokensAfter != 2000 {
		t.Fatalf("event 7 = %+v", got[7])
	}
	if got[8].Kind != ChatEventMessage || got[8].Text != "the answer" {
		t.Fatalf("event 8 = %+v", got[8])
	}
	if got[9].Kind != ChatEventDone {
		t.Fatalf("event 9 = %+v", got[9])
	}
}

// The chat error field is "error" on the wire, same as every other DTO in
// this API — not "err". Getting this wrong silently drops every error event.
func TestSendChatMessage_DecodesTheErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`data: {"kind":"error","error":"quota reached"}` + "\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	var got ChatEvent
	err := c.SendChatMessage(ctx(), SendChatMessageInput{Message: "hi"}, func(e ChatEvent) { got = e })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Kind != ChatEventError || got.Err != "quota reached" {
		t.Fatalf("event = %+v", got)
	}
}

func TestSendChatMessage_RefusedRequestNeverStreams(t *testing.T) {
	srv, _ := server(t, http.StatusBadRequest, map[string]string{"error": "message is empty"})
	c := New(srv.URL, "k")

	calls := 0
	err := c.SendChatMessage(ctx(), SendChatMessageInput{Message: "hi"}, func(ChatEvent) { calls++ })
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if calls != 0 {
		t.Fatalf("onEvent called %d times on a refused request, want 0", calls)
	}
}

// Every path this SDK calls must carry the version segment: nothing in the
// generated contract pins it, so a route renamed to drop it would otherwise
// only surface as a 404 in production.
func TestEveryOperationIsVersioned(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		if got == "/api/v1/chat/send" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if got == "/api/v1/tasks" && r.Method == http.MethodPost || got == "/api/v1/tasks/t-1" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")

	for _, tc := range []struct {
		name string
		call func() error
		want string
	}{
		{"list tasks", func() error { _, err := c.ListTasks(ctx()); return err }, "/api/v1/tasks"},
		{"create task", func() error {
			_, err := c.CreateTask(ctx(), CreateTaskInput{Kind: "fix", Prompt: "p", RepoURL: "r"})
			return err
		}, "/api/v1/tasks"},
		{"get task", func() error { _, err := c.GetTask(ctx(), "t-1"); return err }, "/api/v1/tasks/t-1"},
		{"get task steps", func() error { _, err := c.GetTaskSteps(ctx(), "t-1"); return err }, "/api/v1/tasks/t-1/steps"},
		{"list conversations", func() error { _, err := c.ListConversations(ctx()); return err }, "/api/v1/chat/conversations"},
		{"get conversation messages", func() error {
			_, err := c.GetConversationMessages(ctx(), "c-1")
			return err
		}, "/api/v1/chat/conversations/c-1/messages"},
		{"send chat message", func() error {
			return c.SendChatMessage(ctx(), SendChatMessageInput{Message: "hi"}, func(ChatEvent) {})
		}, "/api/v1/chat/send"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}
