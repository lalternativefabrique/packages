import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AssistantPanel } from "./index";
import type { AssistantApi } from "./types";

afterEach(cleanup);

class FakeRecorder {
  static isTypeSupported = () => true;
  mimeType = "audio/webm";
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  constructor(public stream: MediaStream) {}
  start() {}
  stop() {
    this.ondataavailable?.({ data: new Blob(["x"], { type: "audio/webm" }) });
    this.onstop?.();
  }
}

beforeEach(() => {
  vi.stubGlobal("MediaRecorder", FakeRecorder);
  Object.defineProperty(navigator, "mediaDevices", {
    configurable: true,
    value: { getUserMedia: vi.fn(async () => ({ getTracks: () => [{ stop() {} }] })) },
  });
});

const api: AssistantApi = {
  send: vi.fn(),
  listConversations: vi.fn(async () => []),
  getConversation: vi.fn(async (id: string) => ({ id, messages: [] })),
};

describe("dictation", () => {
  it("hides the mic without a transcriber", () => {
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} />);
    expect(screen.queryByRole("button", { name: "Dicter" })).toBeNull();
  });

  it("records, transcribes and appends the text to the draft", async () => {
    const transcribe = vi.fn(async () => "bonjour le monde");
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} transcribe={transcribe} />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Dicter" })));
    expect(screen.getByRole("status")).toBeTruthy();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Arrêter la dictée" })));
    await waitFor(() => expect((screen.getByLabelText("Message") as HTMLTextAreaElement).value).toBe("bonjour le monde"));
    expect(transcribe).toHaveBeenCalledWith(expect.any(Blob), "fr");
  });

  it("drops the recording on Escape", async () => {
    const transcribe = vi.fn(async () => "x");
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} transcribe={transcribe} />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Dicter" })));
    await act(async () => fireEvent.keyDown(screen.getByLabelText("Message"), { key: "Escape" }));
    expect(transcribe).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Dicter" })).toBeTruthy();
  });

  it("shows the host notice when the audio is kept for later", async () => {
    render(<AssistantPanel api={api} scopes={[]} scopeId="" onClose={() => {}} transcribe={async () => ({ notice: "Gardée hors ligne." })} />);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Dicter" })));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Arrêter la dictée" })));
    await waitFor(() => expect(screen.getByText("Gardée hors ligne.")).toBeTruthy());
  });
});
