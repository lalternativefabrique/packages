import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureLalterClient } from "./http-client";
import { ChatRequestError, sendChatMessage } from "./chat";

function sseResponse(frames: string[], init?: ResponseInit): Response {
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      const encoder = new TextEncoder();
      for (const frame of frames) {
        controller.enqueue(encoder.encode(`data: ${frame}\n\n`));
      }
      controller.close();
    },
  });
  return new Response(body, { status: 200, ...init });
}

describe("sendChatMessage", () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal("fetch", fetchMock);
    configureLalterClient({ baseURL: "https://lalter.example", apiKey: "lalter_sk_x" });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sends the app key as a bearer token", async () => {
    fetchMock.mockResolvedValue(sseResponse([`{"kind":"done"}`]));

    await sendChatMessage({ message: "hi" }, () => {});

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [, init] = fetchMock.mock.calls[0]!;
    expect(init.headers.Authorization).toBe("Bearer lalter_sk_x");
  });

  it("posts to /api/v1/chat/send", async () => {
    fetchMock.mockResolvedValue(sseResponse([`{"kind":"done"}`]));

    await sendChatMessage({ message: "hi" }, () => {});

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url] = fetchMock.mock.calls[0]!;
    expect(String(url)).toBe("https://lalter.example/api/v1/chat/send");
  });

  it("decodes every event kind lalter's chat runner emits", async () => {
    fetchMock.mockResolvedValue(
      sseResponse([
        `{"kind":"conversation","text":"c-1"}`,
        `{"kind":"delta","text":"the "}`,
        `{"kind":"tool_start","tool":"read_file","args":"{\\"path\\":\\"a.go\\"}"}`,
        `{"kind":"tool_end","tool":"read_file","result":"package main"}`,
        `{"kind":"evict","tokens":512}`,
        `{"kind":"compact_start","tokens":8000}`,
        `{"kind":"compact_end","tokens_before":8000,"tokens_after":2000}`,
        `{"kind":"message","text":"the answer"}`,
        `{"kind":"done"}`,
      ]),
    );

    const events: Array<{ kind: string }> = [];
    await sendChatMessage({ message: "hi" }, (e) => events.push(e));

    expect(events).toHaveLength(9);
    expect(events[0]).toMatchObject({ kind: "conversation", text: "c-1" });
    expect(events[2]).toMatchObject({ kind: "tool_start", tool: "read_file" });
    expect(events[3]).toMatchObject({ kind: "tool_end", result: "package main" });
    expect(events[6]).toMatchObject({
      kind: "compact_end",
      tokensBefore: 8000,
      tokensAfter: 2000,
    });
  });

  // The wire field is "error", same as every other DTO in this API — not
  // "err". Getting this wrong silently drops every error event.
  it("maps the wire's error field onto ChatEvent.err", async () => {
    fetchMock.mockResolvedValue(sseResponse([`{"kind":"error","error":"quota reached"}`]));

    const events: Array<{ err?: string }> = [];
    await sendChatMessage({ message: "hi" }, (e) => events.push(e));

    expect(events[0]?.err).toBe("quota reached");
  });

  it("throws ChatRequestError on a refused request instead of streaming", async () => {
    fetchMock.mockResolvedValue(
      new Response("message is empty", { status: 400 }),
    );

    const onEvent = vi.fn();
    await expect(sendChatMessage({ message: "hi" }, onEvent)).rejects.toThrow(
      ChatRequestError,
    );
    expect(onEvent).not.toHaveBeenCalled();
  });

  it("refuses an empty message before making a request", async () => {
    await expect(sendChatMessage({ message: "  " }, () => {})).rejects.toThrow(
      /message is empty/,
    );
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
