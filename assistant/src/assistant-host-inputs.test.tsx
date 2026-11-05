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
