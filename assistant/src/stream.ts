import type { AssistantEvent, AssistantMessage } from "./types";

export function applyEvent(message: AssistantMessage, event: AssistantEvent, now: number): AssistantMessage {
  switch (event.type) {
    case "text":
      return { ...message, content: message.content + event.delta };
    case "reasoning":
      return { ...message, reasoning: (message.reasoning ?? "") + event.delta };
    case "tool_start":
      return {
        ...message,
        tools: [
          ...(message.tools ?? []),
          { id: event.id, name: event.name, label: event.label, args: event.args, startedAt: now },
        ],
      };
    case "tool_end":
      return {
        ...message,
        tools: (message.tools ?? []).map((t) =>
          t.id === event.id ? { ...t, ok: event.ok, summary: event.summary, meta: event.meta, endedAt: now } : t,
        ),
      };
    case "meta":
      return { ...message, meta: event.meta };
  }
}

export function isAbort(err: unknown): boolean {
  return err instanceof DOMException ? err.name === "AbortError" : (err as { name?: string } | null)?.name === "AbortError";
}
