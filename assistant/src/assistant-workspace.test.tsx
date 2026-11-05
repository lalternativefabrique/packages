import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantWorkspace, useAssistantWorkspace, type AssistantWorkspaceApi, type NativeWindowHost } from "./index";
import type { AssistantApi, AssistantEvent, AssistantSendResult } from "./types";

afterEach(cleanup);
beforeEach(() => sessionStorage.clear());

function deferredStreamApi() {
  let resolve!: (r: AssistantSendResult) => void;
  let emit!: (e: AssistantEvent) => void;
  const api: AssistantApi = {
    send: vi.fn(),
    listConversations: vi.fn(async () => []),
    getConversation: vi.fn(async (id: string) => ({ id, messages: [] })),
    stream: vi.fn(
      (_input, e) =>
        new Promise<AssistantSendResult>((r) => {
          emit = e;
          resolve = r;
        }),
    ),
  };
  return { api, emit: (e: AssistantEvent) => emit(e), resolve: (r: AssistantSendResult) => resolve(r) };
}

let ws!: AssistantWorkspaceApi;
function Grab() {
  ws = useAssistantWorkspace();
  return null;
}

function renderWorkspace(api: AssistantApi, windowHost?: NativeWindowHost) {
  render(
    <AssistantWorkspace api={api} scopes={[{ id: "s1", label: "Synthiz" }]} scopeId="s1" windowHost={windowHost}>
      <Grab />
    </AssistantWorkspace>,
  );
}

function ask(text: string) {
  const box = screen.getAllByRole("textbox").at(-1) as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
}

describe("AssistantWorkspace", () => {
  it("shows only the launcher until an assistant is opened", () => {
    renderWorkspace(deferredStreamApi().api);
    expect(screen.getByRole("navigation", { name: "Assistants réduits" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Assistant/ }));
    expect(ws.sessions).toHaveLength(1);
    expect(ws.sessions[0].mode).toBe("docked");
  });

  it("keeps streaming a minimized assistant and flags its answer in the dock", async () => {
    const s = deferredStreamApi();
    renderWorkspace(s.api);
    act(() => void ws.open());
    ask("Pourquoi la CI échoue ?");
    await waitFor(() => expect(s.api.stream).toHaveBeenCalled());

    act(() => ws.minimize(ws.sessions[0].id));
    await waitFor(() => expect(ws.sessions[0].status).toBe("running"));
    expect(screen.getByText("en cours")).toBeTruthy();

    act(() => s.emit({ type: "text", delta: "Le run" }));
    await act(async () => s.resolve({ conversationId: "c1", answer: "Le run #4812 échoue." }));

    await waitFor(() => expect(ws.sessions[0].unread).toBe(1));
    expect(screen.getByText("réponse prête")).toBeTruthy();
    expect(ws.sessions[0].title).toBe("Pourquoi la CI échoue ?");

    fireEvent.click(screen.getByRole("button", { name: /Pourquoi la CI échoue/ }));
    expect(ws.sessions[0]).toMatchObject({ mode: "docked", unread: 0 });
    expect(screen.getByText("Le run #4812 échoue.")).toBeTruthy();
  });

  it("minimizes every assistant at once and restores them where they were", () => {
    renderWorkspace(deferredStreamApi().api);
    act(() => {
      ws.open();
      ws.open({ mode: "floating" });
    });
    fireEvent.click(screen.getAllByRole("button", { name: "Tout réduire" })[0]);
    expect(ws.sessions.map((s) => s.mode)).toEqual(["minimized", "minimized"]);
    fireEvent.click(screen.getByRole("button", { name: "Tout rouvrir" }));
    expect(ws.sessions.map((s) => s.mode)).toEqual(["docked", "floating"]);
  });

  it("detaches a column into a window and docks it back", () => {
    renderWorkspace(deferredStreamApi().api);
    act(() => void ws.open());
    fireEvent.click(screen.getByRole("button", { name: "Détacher en fenêtre" }));
    expect(ws.sessions[0].mode).toBe("floating");
    expect(ws.sessions[0].rect).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Ancrer à droite" }));
    expect(ws.sessions[0].mode).toBe("docked");
  });

  it("sends the oldest column to the dock past two docked columns", async () => {
    renderWorkspace(deferredStreamApi().api);
    act(() => {
      ws.open();
      ws.open();
      ws.open();
    });
    await waitFor(() => expect(ws.sessions.filter((s) => s.mode === "docked")).toHaveLength(2));
    expect(ws.sessions[0].mode).toBe("minimized");
  });

  it("hands floating sessions to a native window host", () => {
    const host: NativeWindowHost = { kind: "native", open: vi.fn(), move: vi.fn(), close: vi.fn() };
    renderWorkspace(deferredStreamApi().api, host);
    act(() => void ws.open({ mode: "floating" }));
    expect(host.open).toHaveBeenCalledWith(expect.objectContaining({ mode: "floating" }));
    act(() => ws.dock(ws.sessions[0].id));
    expect(host.close).toHaveBeenCalledWith(ws.sessions[0].id);
  });

  it("restores the workspace from the store after a reload", () => {
    renderWorkspace(deferredStreamApi().api);
    act(() => void ws.open({ mode: "floating" }));
    cleanup();
    renderWorkspace(deferredStreamApi().api);
    expect(ws.sessions).toHaveLength(1);
    expect(ws.sessions[0].mode).toBe("floating");
  });
});
