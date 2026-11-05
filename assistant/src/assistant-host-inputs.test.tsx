import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantPanel } from "./index";
import type { AssistantApi, AssistantPanelProps } from "./types";

afterEach(cleanup);

function fakeApi(): AssistantApi {
  return {
    send: vi.fn(async (input) => ({ conversationId: "c1", answer: `ok ${input.message}` })),
    listConversations: vi.fn(async () => [{ id: "c9", title: "Ancienne conversation" }]),
    getConversation: vi.fn(async (id: string) => ({ id, messages: [{ role: "user" as const, content: "vieille question" }] })),
  };
}

function Panel(props: Partial<AssistantPanelProps> & { api: AssistantApi }) {
  return <AssistantPanel scopes={[{ id: "s1", label: "Orbit" }]} scopeId="s1" onClose={() => {}} {...props} />;
}

describe("host inputs", () => {
  it("sends a question asked from outside once per id", async () => {
    const api = fakeApi();
    const { rerender } = render(<Panel api={api} ask={{ id: "q1", text: "Résume la page", shownAs: "Résumer" }} />);
    await waitFor(() => expect(api.send).toHaveBeenCalledTimes(1));
    expect(api.send).toHaveBeenCalledWith(expect.objectContaining({ message: "Résume la page" }));
    expect(await screen.findByText("Résumer")).toBeTruthy();
    rerender(<Panel api={api} ask={{ id: "q1", text: "Résume la page" }} />);
    await screen.findByText("ok Résume la page");
    expect(api.send).toHaveBeenCalledTimes(1);
  });

  it("prefills the composer from a seed without sending", () => {
    const api = fakeApi();
    render(<Panel api={api} draftSeed={{ id: "p1", text: "Élément de la page :" }} />);
    const box = screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement;
    expect(box.value).toBe("Élément de la page :");
    expect(document.activeElement).toBe(box);
    expect(api.send).not.toHaveBeenCalled();
  });

  it("carries sendExtra with every turn", async () => {
    const api = fakeApi();
    render(<Panel api={api} sendExtra={{ deep: true }} />);
    const box = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(box, { target: { value: "cherche" } });
    fireEvent.keyDown(box, { key: "Enter" });
    await waitFor(() => expect(api.send).toHaveBeenCalledWith(expect.objectContaining({ extra: { deep: true } })));
  });

  it("lists conversations in a side rail and opens one from it", async () => {
    const api = fakeApi();
    render(<Panel api={api} historyRail />);
    const rail = await screen.findByRole("navigation", { name: "Historique" });
    fireEvent.click(await screen.findByText("Ancienne conversation", undefined, { container: rail } as never));
    await screen.findByText("vieille question");
    expect(api.getConversation).toHaveBeenCalledWith("c9");
  });
});

describe("host actions", () => {
  it("merges per-question extra into the turn and gives tool cards the thread", async () => {
    const api = fakeApi();
    const seen: number[] = [];
    const { rerender } = render(
      <Panel
        api={api}
        sendExtra={{ page: "p" }}
        renderToolCall={(_t, _m, turn) => {
          seen.push(turn.thread.length);
          return undefined;
        }}
      />,
    );
    rerender(<Panel api={api} sendExtra={{ page: "p" }} ask={{ id: "a1", text: "Confirmé.", extra: { confirmAction: "x1" } }} />);
    await waitFor(() =>
      expect(api.send).toHaveBeenCalledWith(expect.objectContaining({ extra: { page: "p", confirmAction: "x1" } })),
    );
  });

  it("starts a fresh conversation before a fresh question", async () => {
    const api = fakeApi();
    const { rerender } = render(<Panel api={api} ask={{ id: "q1", text: "première" }} />);
    await screen.findByText("ok première");
    rerender(<Panel api={api} ask={{ id: "q2", text: "seconde", fresh: true }} />);
    await screen.findByText("ok seconde");
    expect(screen.queryByText("ok première")).toBeNull();
    expect((api.send as ReturnType<typeof vi.fn>).mock.calls[1][0].conversationId).toBeUndefined();
  });

  it("seeds a fresh conversation from a reply", async () => {
    const api = fakeApi();
    const { rerender } = render(<Panel api={api} ask={{ id: "q1", text: "question" }} />);
    await screen.findByText("ok question");
    rerender(<Panel api={api} draftSeed={{ id: "b1", text: "> ok question\n\n", fresh: true }} />);
    expect(screen.queryByText("ok question")).toBeNull();
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).value).toBe("> ok question\n\n");
  });
});
