# reminder

Ask to be told something later, and be told it: storage, a delivery-time
poller, and dispatch to whichever channels a reminder names (Slack, email).

Extracted from Lalter, which runs it in production.

## What it gives you

- **Storage** — a `reminders` table, a due time either absolute or "in 2h/45m/3d".
- **Delivery-time polling** — a `time.Ticker` loop claims what's due with
  `FOR UPDATE SKIP LOCKED`, so several replicas never deliver the same
  reminder twice.
- **Dispatch** — an optional NATS queue-group subscriber (`reminder-dispatcher`)
  fans a fired reminder out to its `Channels` (Slack webhook, email). A
  reminder with no channels is still stored and fires — it's just not pushed
  anywhere outside the API.
- **REST API** — create / list / cancel / mark-done, mountable on any Echo group.

## Using it

```go
svc, err := reminder.NewService(ctx, reminder.ServiceDeps{
    Pool:   pool,
    NC:     nc,
    UserID: func(c echo.Context) string { return middleware.GetUser(c).ID },
    Channels: []domain.Channel{
        infrastructure.NewSlackChannel(),
        infrastructure.NewEmailChannel(myEmailSender),
    },
})
svc.RegisterRoutes(apiGroup)
```

`Channels` is optional — omit it (or pass none) to keep reminders API/chat-only,
as Lalter did before dispatch existed. `UserID` is optional too; a nil func
leaves every reminder unscoped, visible to any caller.

A reminder created with `channels: [{"type": "slack", "target": "https://hooks.slack.com/..."}]`
is pushed there the moment it fires, in addition to sitting in `GET /reminders`.

Requires the `reminders` table (see `migrations/`).

## EmailSender

`infrastructure.NewEmailChannel` takes a narrow `EmailSender` interface
(`Send(ctx, to, subject, htmlBody) error`) rather than importing a specific
mail package — wrap whatever the host app already sends mail through.

## Delivery guarantees

At-least-once for polling: a reminder that fired but whose NATS publish
failed is still marked fired — claiming and publishing cannot be made atomic
across two systems, and delivering twice is worse than logging a miss.
Dispatch is best-effort per channel: one channel failing to send doesn't
retry and doesn't block the others.

## Read access from another context

`Service.Repository()` exposes the `domain.Repository`, so another bounded
context (e.g. a chat agent's tools) can read/write reminders without a second
connection to the same rows.
