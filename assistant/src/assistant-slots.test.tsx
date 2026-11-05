import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantPanel } from "./index";
import type { AssistantApi, AssistantEvent, AssistantPanelProps, AssistantSendResult } from "./types";

afterEach(cleanup);

function streamApi() {
  let emit!: (e: AssistantEvent) => void;
  let resolve!: (r: AssistantSendResult) => void;
  const api: AssistantApi = {
    send: vi.fn(),
    listConversations: vi.fn(async () => []),
    getConversation: vi.fn(async (id: string) => ({ id, messages: [] })),
    stream: vi.fn(
      (_i, e) =>
        new Promise<AssistantSendResult>((r) => {
          emit = e;
          resolve = r;
        }),
    ),
  };
  return { api, emit: (e: AssistantEvent) => emit(e), resolve: (r: AssistantSendResult) => resolve(r) };
}

function renderPanel(props: Partial<AssistantPanelProps> & { api: AssistantApi }) {
  render(<AssistantPanel scopes={[{ id: "s1", label: "Orbit" }]} scopeId="s1" onClose={() => {}} {...props} />);
}

function ask(text: string) {
  const box = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

describe("panel slots", () => {
  it("renders a tool as a card instead of a line when renderToolCall returns one", async () => {
    const s = streamApi();
    renderPanel({
      api: s.api,
      renderToolCall: (t) => (t.name === "propose_action" ? <button type="button">Envoyer ce mail ?</button> : undefined),
    });
    ask("envoie le mail");
    await waitFor(() => expect(s.api.stream).toHaveBeenCalled());
    act(() => {
      s.emit({ type: "tool_start", id: "t1", name: "search_mail" });
      s.emit({ type: "tool_end", id: "t1", ok: true });
      s.emit({ type: "tool_start", id: "t2", name: "propose_action" });
      s.emit({ type: "tool_end", id: "t2", ok: true });
    });
    await act(async () => s.resolve({ conversationId: "c1", answer: "Voilà le brouillon." }));
    expect(screen.getByRole("button", { name: "Envoyer ce mail ?" })).toBeTruthy();
    expect(screen.getByText(/1 outil/)).toBeTruthy();
  });

  it("passes the whole message to renderMarkdown", async () => {
    const s = streamApi();
    const renderMarkdown = vi.fn((text: string) => <p>{text}</p>);
    renderPanel({ api: s.api, renderMarkdown });
    ask("q");
    await waitFor(() => expect(s.api.stream).toHaveBeenCalled());
    await act(async () => s.resolve({ conversationId: "c1", answer: "réponse", meta: { sources: [1] } }));
    expect(renderMarkdown).toHaveBeenLastCalledWith("réponse", expect.objectContaining({ meta: { sources: [1] } }));
  });

  it("lets a composer accessory insert text and focus the field", () => {
    renderPanel({
      api: streamApi().api,
      renderComposerAccessory: (c) => (
        <button type="button" onClick={() => (c.insert("dicté"), c.focus())}>
          Dicter
        </button>
      ),
    });
    fireEvent.click(screen.getByRole("button", { name: "Dicter" }));
    const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
    expect(box.value).toBe("dicté");
    expect(document.activeElement).toBe(box);
  });

  it("shows a banner above the thread and lets bodyOverride replace it", () => {
    renderPanel({ api: streamApi().api, banner: <p>Connectez-vous</p>, bodyOverride: <p>Réglages</p> });
    expect(screen.getByText("Connectez-vous")).toBeTruthy();
    expect(screen.getByText("Réglages")).toBeTruthy();
    expect(screen.queryByText("Que puis-je faire ?")).toBeNull();
  });
});
