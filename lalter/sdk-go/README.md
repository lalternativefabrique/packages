# lalter/sdk-go

Go client for the lalter agent API's app-key-facing surface.

```bash
go get github.com/lalternative/packages/lalter/sdk-go
```

## Why it exists

lalter speaks HTTP and published no client for the surface an external caller
uses without a browser session — queuing tasks, reading their progress,
driving chat. skalpai's core is the first consumer; without this package it
would hand-roll the same auth header, status handling and event-stream parsing
a second consumer would later re-derive from reading lalter's source.

## Where the transport comes from

`internal/wire` is generated from `openapi/lalter.json` — lalter's own
contract, fetched from the API, scoped to the `tasks` and `chat` tags. It owns
**every path, method and parameter** for those two contexts, so none is typed
by hand: a route renamed upstream is a compile error here, not a 404 in
production. Fields work the same way — one added or renamed surfaces at build
time instead of as a value silently never read.

It is `internal` because it is not this package's API. Its methods return raw
`*http.Response` and generated pointer types; everything exported wraps them.

The generated pointers stop at that boundary. swag emits OpenAPI 2.0, which
has no `required`, so every generated field is a `*T`. A nil `Task.Status`
would be indistinguishable from an empty one at a call site that must branch
on it, so every conversion here flattens to the zero value on `nil` — the
degrading direction, never a fabricated status.

One string is still hand-written: the `/api/v1/` prefix. Nothing in the
contract pins it, so `TestEveryOperationIsVersioned` asserts it against every
operation.

### Updating after an API change

lalter serves its own contract at `/openapi.json`, so refreshing it needs no
checkout and no credentials — the contract describes the app-facing API and is
not a secret:

```bash
./refresh-contract.sh                          # or: ./refresh-contract.sh http://localhost:4100
go test ./...                                  # reconcile whatever the new shape breaks
```

Fetching does not make it automatic — it makes it *possible* without a token.
lalter's pipeline still holds no write credential for this repo.

## Why only tasks and chat

lalter's app-key-authenticated API also serves notes, reminders, the LLM
catalogue, credentials and voice — none of them has an external consumer
today. `internal/wire/oapi-codegen.yaml` names `include-tags: [tasks, chat]`
as an allow list rather than a deny list of everything else: adding a third
context to this SDK is then a deliberate one-line edit made by whoever is
adding that context to the external surface, not something that arrives
silently the next time the contract is regenerated.

## Queuing a task

```go
client := sdk.New(os.Getenv("LALTER_BASE_URL"), os.Getenv("LALTER_APP_KEY"))

task, err := client.CreateTask(ctx, sdk.CreateTaskInput{
    Kind:    "fix",
    Prompt:  "the ledger double-credits a self-transfer",
    RepoURL: "https://x-access-token:" + pat + "@github.com/acme/app.git",
    BaseRef: "main",
})
switch {
case errors.Is(err, sdk.ErrUnauthorized):
    // Operator mistake: a bad app key.
case errors.Is(err, sdk.ErrBadRequest):
    // Missing kind/prompt/repo_url — a caller bug.
case err != nil:
    // ErrNotConfigured / ErrUnavailable.
}
```

`CreateTask` answers as soon as the task is queued, not when it finishes: a
run takes minutes, and waiting here would time out on work that later
succeeded.

## Polling a task

```go
t, err := client.GetTask(ctx, task.ID)
if errors.Is(err, sdk.ErrNotFound) {
    // No such task — a wrong id, not a transient failure.
}

switch t.Status {
case sdk.TaskStatusDone:
    fmt.Println(t.Diff, t.Summary)
case sdk.TaskStatusFailed:
    fmt.Println(t.Error)
}
```

`ListTasks` lists the caller's tasks, most recent first. `GetTaskSteps` reads
what the agent did, one entry per tool call — useful for showing progress on a
task still running.

## Chat

```go
err := client.SendChatMessage(ctx, sdk.SendChatMessageInput{
    Message: "what changed in the last release?",
}, func(e sdk.ChatEvent) {
    switch e.Kind {
    case sdk.ChatEventConversation:
        conversationID = e.Text // present on a new thread's first event
    case sdk.ChatEventDelta:
        fmt.Print(e.Text) // one fragment, as it is produced
    case sdk.ChatEventToolStart:
        fmt.Println("running", e.Tool, e.Args)
    case sdk.ChatEventToolEnd:
        fmt.Println(e.Tool, "->", e.Result)
    case sdk.ChatEventMessage:
        fullReply = e.Text // the whole turn, once streaming completes
    case sdk.ChatEventError:
        log.Println("chat error:", e.Err)
    }
})
```

`ChatEventEvict` and `ChatEventCompactStart`/`ChatEventCompactEnd` report
context-window housekeeping (a stale tool result dropped, or older turns
summarized) — surfaced so a caller showing the stream live can account for
them instead of a gap the model appears to explain nothing for.

`SendChatMessage` streams `text/event-stream`, not JSON — a chat turn takes
seconds and arrives in pieces, so `onEvent` is called once per event rather
than once the whole reply is in. It has no fixed timeout: bound it with `ctx`
if the caller needs one, since a fixed timeout would cut off a slow reply
mid-stream.

`ConversationID` is empty on the first message of a thread; the reply's first
event carries the id lalter assigned it. `ListConversations` and
`GetConversationMessages` read a thread's history back.

## Notes

- The app key identifies the **app**, and lalter resolves the user it was
  issued for — nothing about which user travels as a caller-supplied
  parameter, unlike a service where an app acts on behalf of many of its own
  users.
- Pass the API root without the version segment (`https://lalter.example`) —
  paths are appended here.
- Default timeout is 5s for everything except `SendChatMessage`, which has
  none (`WithTimeout` changes the former).
