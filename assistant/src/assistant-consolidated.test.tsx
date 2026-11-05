import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  AssistantPanel,
  AssistantStreamError,
  AssistantWorkspace,
  SafeMarkdown,
  useAssistantWorkspace,
  type AssistantWorkspaceApi,
} from "./index";
import type { AssistantApi, AssistantEvent, AssistantPanelProps, AssistantSendResult } from "./types";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});
beforeEach(() => sessionStorage.clear());

type Turn = { emit: (e: AssistantEvent) => void; resolve: (r: AssistantSendResult) => void; reject: (e: unknown) => void };

function controlledApi(extra: Partial<AssistantApi> = {}) {
  const turns: Turn[] = [];
  const open = (e: (ev: AssistantEvent) => void, signal: AbortSignal) =>
    new Promise<AssistantSendResult>((resolve, reject) => {
      turns.push({ emit: e, resolve, reject });
      signal.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")));
    });
  const api: AssistantApi = {
    send: vi.fn(),
    listConversations: vi.fn(async () => []),
    getConversation: vi.fn(async (id: string) => ({ id, messages: [] })),
    stream: vi.fn((_i, e, signal) => open(e, signal)),
    stop: vi.fn(async () => {}),
    resume: vi.fn((_id, e, signal) => open(e, signal)),
    ...extra,
  };
  return { api, turns };
}

function Panel(props: Partial<AssistantPanelProps> & { api: AssistantApi }) {
  return <AssistantPanel scopes={[{ id: "s1", label: "Orbit" }]} scopeId="s1" onClose={() => {}} {...props} />;
}

function ask(text: string) {
  const box = screen.getByRole("textbox", { name: "Message" });
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

describe("server-side stop and resume", () => {
  it("stops the reply on the server too", async () => {
    const { api, turns } = controlledApi();
    render(<Panel api={api} initialConversationId="c1" />);
    await waitFor(() => expect(api.getConversation).toHaveBeenCalled());
    ask("long");
    await waitFor(() => expect(turns).toHaveLength(1));
    act(() => turns[0].emit({ type: "text", delta: "début" }));
    fireEvent.click(screen.getByRole("button", { name: "Arrêter" }));
    await screen.findByText("Réponse interrompue.");
    expect(api.stop).toHaveBeenCalledWith("c1");
  });

  it("reattaches to a reply still being written when a conversation opens", async () => {
    const { api, turns } = controlledApi({ getConversation: vi.fn(async (id: string) => ({ id, live: true, messages: [{ role: "user" as const, content: "q" }] })) });
    render(<Panel api={api} initialConversationId="c1" />);
    await waitFor(() => expect(api.resume).toHaveBeenCalledWith("c1", expect.any(Function), expect.any(AbortSignal)));
    act(() => turns[0].emit({ type: "text", delta: "suite" }));
    await act(async () => turns[0].resolve({ conversationId: "c1", answer: "suite et fin" }));
    expect(screen.getByText("suite et fin")).toBeTruthy();
  });

  it("retries through resume when the connection drops", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { api, turns } = controlledApi();
    render(<Panel api={api} initialConversationId="c1" />);
    await waitFor(() => expect(api.getConversation).toHaveBeenCalled());
    ask("q");
    await waitFor(() => expect(turns).toHaveLength(1));
    await act(async () => turns[0].reject(new AssistantStreamError("connection_lost")));
    await act(async () => vi.advanceTimersByTime(1100));
    await waitFor(() => expect(api.resume).toHaveBeenCalledTimes(1));
    await act(async () => turns[1].resolve({ conversationId: "c1", answer: "reprise" }));
    expect(screen.getByText("reprise")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("offers to resume a reply already in progress", async () => {
    const { api, turns } = controlledApi();
    render(<Panel api={api} initialConversationId="c1" />);
    await waitFor(() => expect(api.getConversation).toHaveBeenCalled());
    ask("q");
    await waitFor(() => expect(turns).toHaveLength(1));
    await act(async () => turns[0].reject(Object.assign(new Error("409"), { code: "reply_in_progress" })));
    expect(screen.getByRole("alert").textContent).toContain("encore en cours");
    fireEvent.click(screen.getByRole("button", { name: "Reprendre" }));
    await waitFor(() => expect(api.resume).toHaveBeenCalledWith("c1", expect.any(Function), expect.any(AbortSignal)));
  });

  it("stays silent when there is nothing to stream", async () => {
    const { api, turns } = controlledApi();
    render(<Panel api={api} />);
    ask("q");
    await waitFor(() => expect(turns).toHaveLength(1));
    await act(async () => turns[0].reject(new AssistantStreamError("nothing_to_stream")));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("answer parts and actions", () => {
  it("renders steps, sources, suggestions, answer images and host actions", async () => {
    const { api, turns } = controlledApi();
    const apply = vi.fn();
    const insert = vi.fn();
    render(
      <Panel
        api={api}
        agentName="Station"
        onApplySuggestion={apply}
        messageActions={[{ id: "ins", label: "Insérer dans l'article", onSelect: insert }]}
      />,
    );
    ask("synonymes de rapide");
    await waitFor(() => expect(turns).toHaveLength(1));
    act(() => turns[0].emit({ type: "step", step: 2 }));
    expect(screen.getByText("Étape 2")).toBeTruthy();
    act(() => turns[0].emit({ type: "attachment", attachment: { id: "i1", filename: "a.png", mimeType: "image/png", sizeBytes: 1, url: "https://x/a.png", sourceUrl: "https://src", sourceTitle: "Src" } }));
    await act(async () =>
      turns[0].resolve({
        conversationId: "c1",
        answer: "Voici.",
        suggestions: ["vif", "prompt"],
        sources: [{ title: "Larousse", url: "https://larousse.fr", site: "larousse.fr" }],
      }),
    );
    expect(screen.getByText("Station")).toBeTruthy();
    expect(screen.queryByText("Étape 2")).toBeNull();
    expect(screen.getByRole("link", { name: /Larousse/ }).getAttribute("href")).toBe("https://larousse.fr");
    expect(screen.getByRole("img").getAttribute("src")).toBe("https://x/a.png");
    fireEvent.click(screen.getByRole("button", { name: "vif" }));
    expect(apply).toHaveBeenCalledWith(expect.objectContaining({ content: "Voici." }), "vif");
    fireEvent.click(screen.getByRole("button", { name: "Insérer dans l'article" }));
    expect(insert).toHaveBeenCalledWith(expect.objectContaining({ content: "Voici." }));
  });

  it("sends a silent turn without a user bubble", async () => {
    const send = vi.fn(async () => ({ conversationId: "c1", answer: "C'est envoyé." }));
    render(<Panel api={{ send, listConversations: async () => [], getConversation: async (id) => ({ id, messages: [] }) }} ask={{ id: "a", text: "Confirmé.", silent: true, extra: { confirmAction: "x" } }} />);
    await screen.findByText("C'est envoyé.");
    expect(screen.queryByText("Confirmé.")).toBeNull();
    expect(send).toHaveBeenCalledWith(expect.objectContaining({ extra: { confirmAction: "x" } }));
  });

  it("carries quick action options into the turn", async () => {
    const send = vi.fn(async () => ({ conversationId: "c1", answer: "ok" }));
    render(
      <Panel
        api={{ send, listConversations: async () => [], getConversation: async (id) => ({ id, messages: [] }) }}
        quickActions={[{ id: "d", label: "Définir", prompt: "Définis", options: { mode: "define" } }]}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Définir/ }));
    await waitFor(() => expect(send).toHaveBeenCalledWith(expect.objectContaining({ extra: { mode: "define" } })));
  });
});

describe("composer and layout slots", () => {
  it("toggles host options from the + menu and shows them as removable chips", () => {
    const toggle = vi.fn();
    const { rerender } = render(<Panel api={controlledApi().api} composerOptions={[{ id: "deep", label: "Deep search", checked: false, onToggle: toggle }]} />);
    fireEvent.click(screen.getByRole("button", { name: "Options" }));
    fireEvent.click(screen.getByRole("menuitemcheckbox", { name: /Deep search/ }));
    expect(toggle).toHaveBeenCalledWith(true);
    rerender(<Panel api={controlledApi().api} composerOptions={[{ id: "deep", label: "Deep search", checked: true, onToggle: toggle }]} />);
    fireEvent.click(screen.getByRole("button", { name: "Retirer Deep search" }));
    expect(toggle).toHaveBeenLastCalledWith(false);
  });

  it("renders the host history, subtitle, empty state and the block above the composer", () => {
    const { api } = controlledApi();
    render(
      <Panel
        api={api}
        renderHistory={(h) => (
          <button type="button" onClick={() => h.open("c7")}>
            Mes conversations
          </button>
        )}
        subtitle="· Travail · sur cette machine"
        emptyState={<p>Synthétiser cette page</p>}
        aboveComposer={<p>Plan de recherche prêt</p>}
      />,
    );
    expect(screen.getByText("· Travail · sur cette machine")).toBeTruthy();
    expect(screen.getByText("Synthétiser cette page")).toBeTruthy();
    expect(screen.queryByText("Que puis-je faire ?")).toBeNull();
    expect(screen.getByText("Plan de recherche prêt")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Mes conversations" }));
    expect(api.getConversation).toHaveBeenCalledWith("c7");
  });

  it("shows the recording overlay and lets the host take Escape", () => {
    const escape = vi.fn(() => true);
    render(<Panel api={controlledApi().api} composerOverlay={<span>Enregistrement…</span>} onComposerEscape={escape} />);
    expect(screen.getByText("Enregistrement…")).toBeTruthy();
    const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
    expect(box.placeholder).toBe("");
    fireEvent.keyDown(box, { key: "Escape" });
    expect(escape).toHaveBeenCalled();
  });

  it("opens disabled with the host's reason", () => {
    render(<Panel api={controlledApi().api} disabledReason="Enregistre l'article pour discuter." />);
    expect(screen.getByText("Enregistre l'article pour discuter.")).toBeTruthy();
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).disabled).toBe(true);
  });

  it("keeps a single conversation per scope and reopens it", async () => {
    const { api } = controlledApi({ listConversations: vi.fn(async () => [{ id: "only", title: "Article", scopeId: "s1" }]) });
    render(<Panel api={api} singleConversation />);
    await waitFor(() => expect(api.getConversation).toHaveBeenCalledWith("only"));
    expect(screen.queryByRole("button", { expanded: false, name: /Assistant/ })).toBeNull();
  });
});

describe("SafeMarkdown", () => {
  it("renders lists, emphasis and code, and drops non-http links", () => {
    render(<SafeMarkdown text={"**Gras** et `code`\n\n- un\n- deux\n\n[ok](https://a.fr) [ko](javascript:alert(1))"} />);
    expect(screen.getByText("Gras").tagName).toBe("STRONG");
    expect(screen.getByText("code").tagName).toBe("CODE");
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(screen.getByRole("link", { name: "ok" }).getAttribute("href")).toBe("https://a.fr");
    expect(screen.queryByRole("link", { name: "ko" })).toBeNull();
  });
});

describe("workspace", () => {
  let ws!: AssistantWorkspaceApi;
  function Grab() {
    ws = useAssistantWorkspace();
    return null;
  }

  it("carries open() options into the session's turns and updates its selection", async () => {
    const send = vi.fn(async () => ({ conversationId: "c1", answer: "ok" }));
    render(
      <AssistantWorkspace api={{ send, listConversations: async () => [], getConversation: async (id) => ({ id, messages: [] }) }} scopes={[{ id: "s1", label: "A" }]} scopeId="s1">
        <Grab />
      </AssistantWorkspace>,
    );
    act(() => void ws.open({ options: { mode: "critique" }, selection: { text: "premier" } }));
    expect(screen.getByText("« premier »")).toBeTruthy();
    act(() => ws.setSelection(ws.sessions[0].id, { text: "second" }));
    expect(screen.getByText("« second »")).toBeTruthy();
    ask("critique ça");
    await waitFor(() =>
      expect(send).toHaveBeenCalledWith(expect.objectContaining({ extra: { mode: "critique" }, selection: { text: "second" } })),
    );
  });
});

describe("showSources", () => {
  it("lets a host that cites inline hide the source cards", async () => {
    const send = vi.fn(async () => ({ conversationId: "c1", answer: "ok", sources: [{ title: "Larousse", url: "https://larousse.fr" }] }));
    render(<Panel api={{ send, listConversations: async () => [], getConversation: async (id) => ({ id, messages: [] }) }} showSources={false} ask={{ id: "q", text: "q" }} />);
    await screen.findByText("ok");
    expect(screen.queryByRole("link", { name: /Larousse/ })).toBeNull();
  });
});
