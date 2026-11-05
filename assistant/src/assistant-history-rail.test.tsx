import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantPanel } from "./index";
import type { AssistantApi } from "./types";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const api: AssistantApi = {
  send: vi.fn(),
  listConversations: vi.fn(async () => []),
  getConversation: vi.fn(async (id: string) => ({ id, messages: [] })),
};

function stubWidth(wide: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: wide, media: q, addEventListener() {}, removeEventListener() {} }));
}


describe("history rail", () => {
  it("shows the host history once when the rail is visible", () => {
    stubWidth(true);
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} renderHistory={() => <p>Liste</p>} initialHistory />);
    expect(screen.getAllByText("Liste")).toHaveLength(1);
  });

  it("opens the history in the body when the rail is hidden", () => {
    stubWidth(false);
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} renderHistory={() => <p>Liste</p>} initialHistory />);
    expect(screen.getAllByText("Liste")).toHaveLength(2);
  });
});
