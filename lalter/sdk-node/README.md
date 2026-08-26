# @lalternative/lalter-sdk

Typed TypeScript client for lalter's app-key-facing API: queuing and reading
agent tasks, and driving chat, without a browser session.

## Install

```sh
pnpm add @lalternative/lalter-sdk
```

## Why it exists

lalter speaks HTTP and published no client for the surface an external caller
uses without a browser session. skalpai's core is the first consumer;
without this package it would hand-roll the same auth header, status
handling and event-stream parsing a second consumer would later re-derive
from reading lalter's source.

## Configure

```ts
import { configureLalterClient, listTasks, createTask } from "@lalternative/lalter-sdk";

configureLalterClient({
  baseURL: process.env.LALTER_BASE_URL!, // no default — pass your deployment
  apiKey: process.env.LALTER_APP_KEY!,
});
```

`configureLalterClient(opts)` accepts:

| Option    | Required | Notes |
|-----------|----------|-------|
| `baseURL` | yes\*    | lalter has no single public deployment, unlike some SDKs in this monorepo — always pass it explicitly. |
| `apiKey`  | yes\*    | Sent as `Authorization: Bearer <apiKey>`. Issued from lalter's console under **App keys**. |
| `axios`   | no       | Inject your own `AxiosInstance` (interceptors, retry, telemetry). When set, `baseURL` and `apiKey` are ignored — wire them into your instance directly, and `sendChatMessage` will not know the auth header to attach to its own streamed request. |

\* Not required only when passing a custom `axios` instance instead.

## Queuing a task

```ts
const task = await createTask({
  kind: "fix",
  prompt: "the ledger double-credits a self-transfer",
  repo_url: `https://x-access-token:${pat}@github.com/acme/app.git`,
  base_ref: "main",
});
```

`createTask` answers as soon as the task is queued, not when it finishes: a
run takes minutes, and waiting here would time out on work that later
succeeded.

## Polling a task

```ts
const task = await getTask(taskId);

switch (task.status) {
  case "done":
    console.log(task.diff, task.summary);
    break;
  case "failed":
    console.error(task.error);
    break;
}
```

`listTasks()` lists the caller's tasks, most recent first. `getTaskSteps(id)`
reads what the agent did, one entry per tool call — useful for showing
progress on a task still running.

## Chat

```ts
import { sendChatMessage } from "@lalternative/lalter-sdk";

let conversationId: string | undefined;
let reply = "";

await sendChatMessage({ message: "what changed in the last release?" }, (event) => {
  switch (event.kind) {
    case "conversation":
      conversationId = event.text; // present on a new thread's first event
      break;
    case "delta":
      process.stdout.write(event.text ?? ""); // one fragment, as it is produced
      break;
    case "tool_start":
      console.log("running", event.tool, event.args);
      break;
    case "tool_end":
      console.log(event.tool, "->", event.result);
      break;
    case "message":
      reply = event.text ?? ""; // the whole turn, once streaming completes
      break;
    case "error":
      console.error("chat error:", event.err);
      break;
  }
});
```

`sendChatMessage` is hand-written rather than generated: orval has no notion
of Server-Sent Events, so this reads `POST /chat/send`'s `text/event-stream`
body itself rather than decoding it as one JSON value — everything else
(`createTask`, `listTasks`, `getTask`, `getTaskSteps`, `listConversations`,
`getConversationMessages`) is generated straight from the contract.

`event.kind === "evict"` and `"compact_start"`/`"compact_end"` report
context-window housekeeping (a stale tool result dropped, or older turns
summarized) — surfaced so a caller showing the stream live can account for
them instead of a gap the model appears to explain nothing for.

`conversationId` is empty to open a new thread; the reply's first event
carries the id lalter assigned it. `listConversations()` and
`getConversationMessages(id)` read a thread's history back.

Pass `signal` (an `AbortSignal`) to `sendChatMessage` if the caller needs a
timeout — there is no fixed one, since a chat turn can run for as long as the
agent takes and a fixed timeout would cut off a slow reply mid-stream.

## Why only tasks and chat

lalter's app-key-authenticated API also serves notes, reminders, the LLM
catalogue, credentials and voice — none of them has an external consumer
today. `scripts/filter-openapi.mjs` (run by `pnpm generate`, before orval
ever sees the document) keeps only the `tasks` and `chat` tags' paths and the
schemas they reach — an allow list, deliberately: adding a third context to
this SDK is then a one-line edit to `ALLOWED_TAGS`, made by whoever is
adding that context to the external surface, not something that arrives
silently the next time the contract is regenerated. `orval.config.ts`'s own
`filters.tags` is kept alongside it as a second line of defense on the
paths, but it alone would still emit every schema in the document — orval's
split mode does not prune a schema for having no surviving path.

## Regenerate

The generated code lives under `src/generated/`. Refresh
`openapi.full.json` from lalter's own `/openapi.json` (public, no
credentials needed — see `lalternative/packages/lalter/sdk-go`'s
`refresh-contract.sh` for the equivalent Go-side fetch), then:

```sh
pnpm generate   # filters openapi.full.json -> openapi.json, then runs orval
```

## Build

```sh
pnpm build
```
