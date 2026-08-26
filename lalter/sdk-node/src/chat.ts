import { getLalterAuthHeader, getLalterClient } from "./http-client";

/**
 * Kinds lalter's chat stream emits. Kept as a union of literals so a typo in
 * a comparison is a type error rather than a case that silently never
 * matches.
 */
export type ChatEventKind =
  | "conversation" // Text carries the conversation id, once, as the first event of a new thread.
  | "delta" // Text carries one fragment of the reply, as it is produced.
  | "tool_start" // Tool/Args describe one tool call about to run.
  | "tool_end" // Tool/Result (and Meta, when structured) describe what it returned.
  | "message" // Text carries the whole turn's reply, once, after streaming completes.
  | "error" // Err carries the failure text. Last event of the stream when it fires.
  | "done"
  | "evict" // A stale tool result was dropped from history to free context; Tokens is how many.
  | "compact_start" // Tokens is usage against the threshold that triggered compaction.
  | "compact_end"; // TokensBefore/TokensAfter bracket what compaction freed.

/** One Server-Sent Event from POST /chat/send. Only the fields relevant to `kind` are set. */
export interface ChatEvent {
  kind: ChatEventKind;
  text?: string;
  tool?: string;
  args?: string;
  result?: string;
  meta?: string;
  err?: string;
  tokens?: number;
  tokensBefore?: number;
  tokensAfter?: number;
}

export interface SendChatMessageInput {
  /** Empty to open a new thread — the reply's first event carries the id lalter assigned it. */
  conversationId?: string;
  message: string;
  /**
   * Points a new conversation at a repository, credentials included.
   * Ignored once the thread exists — a thread works on one repository
   * throughout its history.
   */
  repoUrl?: string;
  baseRef?: string;
}

/** Raised when lalter refuses the request (4xx/5xx) before any event streams. */
export class ChatRequestError extends Error {
  constructor(
    public readonly status: number,
    body: string,
  ) {
    super(`lalter chat request failed with status ${status}: ${body}`);
    this.name = "ChatRequestError";
  }
}

/**
 * Sends a message and streams the reply, calling onEvent for each event as
 * it arrives.
 *
 * orval has no notion of Server-Sent Events — it generates request/response
 * JSON calls, not a body meant to be read incrementally — so this function
 * bypasses the generated client and reads the raw stream itself. It still
 * goes through getLalterClient() so the base URL and auth header stay in
 * one place (configureLalterClient), rather than duplicated here.
 *
 * lalter's own event JSON uses snake_case keys (tool_name, tokens_before,
 * ...); this function is what converts them to the camelCase shape this
 * package's other calls use, so a field rename on lalter's side surfaces
 * here as a broken test instead of a silently-dropped value in every
 * consumer.
 *
 * Bound the call with `signal` if the caller needs a timeout: a chat turn can
 * run for as long as the agent takes, which is exactly what a fixed timeout
 * would cut off mid-reply.
 */
export async function sendChatMessage(
  input: SendChatMessageInput,
  onEvent: (event: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  if (!input.message.trim()) {
    throw new Error("lalter: message is empty");
  }

  const client = getLalterClient();
  const baseURL = client.defaults.baseURL;
  if (!baseURL) {
    throw new Error(
      "lalter SDK is not configured — call configureLalterClient() first",
    );
  }
  const authHeader = getLalterAuthHeader();

  const response = await fetch(new URL("/api/v1/chat/send", baseURL), {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      Accept: "text/event-stream",
      ...(authHeader ? { Authorization: authHeader } : {}),
    },
    body: JSON.stringify({
      conversation_id: input.conversationId,
      message: input.message,
      repo_url: input.repoUrl,
      base_ref: input.baseRef,
    }),
    signal,
  });

  if (!response.ok || !response.body) {
    const body = await response.text().catch(() => "");
    throw new ChatRequestError(response.status, body.slice(0, 512));
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let frameEnd: number;
    while ((frameEnd = buffer.indexOf("\n\n")) !== -1) {
      const frame = buffer.slice(0, frameEnd);
      buffer = buffer.slice(frameEnd + 2);
      const event = parseFrame(frame);
      if (event) onEvent(event);
    }
  }
}

function parseFrame(frame: string): ChatEvent | null {
  const line = frame.split("\n").find((l) => l.startsWith("data: "));
  if (!line) return null;

  let raw: {
    kind?: string;
    text?: string;
    tool?: string;
    args?: string;
    result?: string;
    meta?: string;
    error?: string;
    tokens?: number;
    tokens_before?: number;
    tokens_after?: number;
  };
  try {
    raw = JSON.parse(line.slice("data: ".length));
  } catch {
    return null;
  }
  if (!raw.kind) return null;

  return {
    kind: raw.kind as ChatEventKind,
    text: raw.text,
    tool: raw.tool,
    args: raw.args,
    result: raw.result,
    meta: raw.meta,
    err: raw.error,
    tokens: raw.tokens,
    tokensBefore: raw.tokens_before,
    tokensAfter: raw.tokens_after,
  };
}
