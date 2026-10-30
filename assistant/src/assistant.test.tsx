import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AssistantLauncher, AssistantPanel, relativeTime } from "./index";
import type { AssistantApi, AssistantPanelProps, AttachmentLimits } from "./types";

afterEach(cleanup);

const limits: AttachmentLimits = {
  maxCount: 2,
  maxImageBytes: 100,
  maxFileBytes: 50,
  imageTypes: ["image/png"],
  fileTypes: ["text/plain", "text/markdown"],
};

function fakeApi(overrides: Partial<AssistantApi> = {}): AssistantApi {
  let n = 0;
  return {
    send: vi.fn(async (input) => ({ conversationId: input.conversationId ?? "c1", answer: `answer to ${input.message}` })),
    listConversations: vi.fn(async () => []),
    getConversation: vi.fn(async (id: string) => ({ id, scopeId: "s2", messages: [{ role: "user" as const, content: "old question" }] })),
    attach: vi.fn(async (file: File) => ({ id: `att-${++n}`, filename: file.name, mimeType: file.type, sizeBytes: file.size })),
    ...overrides,
  };
}

function renderPanel(props: Partial<AssistantPanelProps> = {}) {
  const api = props.api ?? fakeApi();
  render(
    <AssistantPanel
      api={api}
      scopes={[
        { id: "s1", label: "Synthiz" },
        { id: "s2", label: "Vvaves" },
      ]}
      scopeId="s1"
      attachmentLimits={limits}
      quickActions={[{ id: "q1", label: "Expliquer", hint: "hint", prompt: "Explain the screen" }]}
      onClose={() => {}}
      {...props}
    />,
  );
  return { api, composer: screen.getByLabelText("Message") };
}

function clipboard(files: File[], text = ""): Partial<DataTransfer> {
  return {
    items: files.map((f) => ({ kind: "file", type: f.type, getAsFile: () => f })) as unknown as DataTransferItemList,
    getData: () => text,
  };
}

const file = (name: string, size: number, type: string) => new File(["x".repeat(size)], name, { type });

describe("AssistantLauncher", () => {
  it("shows the history and assistant buttons and opens the panel", async () => {
    render(<AssistantLauncher api={fakeApi()} scopes={[{ id: "s1", label: "Synthiz" }]} scopeId="s1" />);
    fireEvent.click(screen.getByText("Historique"));
    expect(await screen.findByText("Aucune conversation pour l'instant.")).toBeTruthy();
  });
});

describe("send flow", () => {
  it("sends on Enter with the scope, the screen and keeps the conversation id", async () => {
    const readScreen = () => ({ path: "/apps/x", title: "X", text: "screen" });
    const { api, composer } = renderPanel({ readScreen });
    fireEvent.change(composer, { target: { value: "Bonjour" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    expect(await screen.findByText("answer to Bonjour")).toBeTruthy();
    expect(api.send).toHaveBeenCalledWith({ signal: expect.any(AbortSignal), scopeId: "s1", conversationId: undefined, message: "Bonjour", attachmentIds: [], screen: readScreen() });
    fireEvent.change(composer, { target: { value: "Encore" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    await screen.findByText("answer to Encore");
    expect(vi.mocked(api.send).mock.calls[1][0].conversationId).toBe("c1");
    expect(api.listConversations).toHaveBeenCalledTimes(3);
  });

  it("keeps Shift+Enter as a newline", () => {
    const { api, composer } = renderPanel();
    fireEvent.change(composer, { target: { value: "a" } });
    fireEvent.keyDown(composer, { key: "Enter", shiftKey: true });
    expect(api.send).not.toHaveBeenCalled();
  });

  it("sends a quick action's prompt and shows its label", async () => {
    const { api } = renderPanel();
    fireEvent.click(screen.getByText("Expliquer"));
    await screen.findByText("answer to Explain the screen");
    expect(vi.mocked(api.send).mock.calls[0][0].message).toBe("Explain the screen");
    expect(screen.getByText("Expliquer")).toBeTruthy();
  });

  it("shows the error the host's send rejects with", async () => {
    const { composer } = renderPanel({ api: fakeApi({ send: async () => Promise.reject({ message: "boom" }) }) });
    fireEvent.change(composer, { target: { value: "x" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    expect((await screen.findByRole("alert")).textContent).toBe("boom");
  });

  it("loads a past conversation and moves to its scope", async () => {
    const onScopeChange = vi.fn();
    renderPanel({
      api: fakeApi({ listConversations: async () => [{ id: "old", title: "Ancienne", scopeId: "s2" }] }),
      initialHistory: true,
      onScopeChange,
    });
    fireEvent.click(await screen.findByText("Ancienne"));
    expect(await screen.findByText("old question")).toBeTruthy();
    expect(onScopeChange).toHaveBeenCalledWith("s2");
  });
});

describe("attachments", () => {
  it("uploads every pasted file and sends their ids, with an empty text", async () => {
    const { api, composer } = renderPanel();
    const shot = file("image.png", 10, "image/png");
    const notes = file("notes.txt", 5, "text/plain");
    fireEvent.paste(composer, { clipboardData: clipboard([shot, notes]) });
    await waitFor(() => expect(screen.getAllByText(/\d+ o$/)).toHaveLength(2));
    expect(api.attach).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByLabelText("Envoyer"));
    await waitFor(() => expect(api.send).toHaveBeenCalled());
    expect(vi.mocked(api.send).mock.calls[0][0].attachmentIds).toEqual(["att-1", "att-2"]);
    expect(screen.queryByLabelText("Retirer image.png")).toBeNull();
  });

  it("lets plain text paste through", () => {
    const { api, composer } = renderPanel();
    const event = fireEvent.paste(composer, { clipboardData: clipboard([], "short") });
    expect(event).toBe(true);
    expect(api.attach).not.toHaveBeenCalled();
  });

  it("turns a long pasted text into a file past the threshold", async () => {
    const { api, composer } = renderPanel({ pasteAsFileThreshold: 10 });
    fireEvent.paste(composer, { clipboardData: clipboard([], "a long pasted log line") });
    await waitFor(() => expect(api.attach).toHaveBeenCalledTimes(1));
    const sent = vi.mocked(api.attach!).mock.calls[0][0];
    expect(sent.name).toBe("texte-colle.txt");
    expect(sent.type).toBe("text/plain");
  });

  it("accepts dropped files and the paperclip's", async () => {
    const { api } = renderPanel();
    fireEvent.drop(screen.getByRole("dialog"), { dataTransfer: { files: [file("a.md", 3, "text/markdown")], types: ["Files"] } });
    fireEvent.change(screen.getByTestId("lalt-assistant-file-input"), { target: { files: [file("b.txt", 3, "")] } });
    await waitFor(() => expect(api.attach).toHaveBeenCalledTimes(2));
    expect(vi.mocked(api.attach!).mock.calls[1][0].type).toBe("text/plain");
  });

  it("refuses files over the limits without uploading them", async () => {
    const { api, composer } = renderPanel();
    fireEvent.paste(composer, {
      clipboardData: clipboard([file("big.txt", 51, "text/plain"), file("a.zip", 1, "application/zip")]),
    });
    expect(await screen.findByText("big.txt dépasse 50 o.")).toBeTruthy();
    expect(screen.getByText("a.zip : type de fichier non accepté.")).toBeTruthy();
    expect(api.attach).not.toHaveBeenCalled();
    expect((screen.getByLabelText("Envoyer") as HTMLButtonElement).disabled).toBe(true);
  });

  it("caps the number of files and frees room on remove", async () => {
    const { api, composer } = renderPanel();
    fireEvent.paste(composer, {
      clipboardData: clipboard([file("1.txt", 1, "text/plain"), file("2.txt", 1, "text/plain"), file("3.txt", 1, "text/plain")]),
    });
    expect(await screen.findByText("Au plus 2 fichiers par message.")).toBeTruthy();
    expect(api.attach).toHaveBeenCalledTimes(2);
    fireEvent.click(screen.getByLabelText("Retirer 1.txt"));
    expect(screen.queryByText("Au plus 2 fichiers par message.")).toBeNull();
    fireEvent.paste(composer, { clipboardData: clipboard([file("3.txt", 1, "text/plain")]) });
    await waitFor(() => expect(api.attach).toHaveBeenCalledTimes(3));
  });

  it("blocks sending while a file uploads and shows an upload error", async () => {
    let finish: (v: never) => void = () => {};
    let fail: (e: unknown) => void = () => {};
    const attach = vi
      .fn()
      .mockImplementationOnce(() => new Promise((resolve) => (finish = resolve)))
      .mockImplementationOnce(() => new Promise((_, reject) => (fail = reject)));
    const { composer } = renderPanel({ api: fakeApi({ attach }) });
    fireEvent.change(composer, { target: { value: "hi" } });
    fireEvent.paste(composer, { clipboardData: clipboard([file("a.txt", 1, "text/plain")]) });
    const send = screen.getByLabelText("Envoyer") as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    await act(async () => finish({ id: "a", filename: "a.txt", mimeType: "text/plain", sizeBytes: 1 } as never));
    expect(send.disabled).toBe(false);
    fireEvent.paste(composer, { clipboardData: clipboard([file("b.txt", 1, "text/plain")]) });
    await act(async () => fail({ message: "too big for the server" }));
    expect(screen.getByText("too big for the server")).toBeTruthy();
  });

  it("stops a running request and gives the message back", async () => {
    const api = fakeApi();
    let signal: AbortSignal | undefined;
    api.send = (input) => {
      signal = input.signal;
      return new Promise(() => {});
    };
    const { composer } = renderPanel({ api });
    fireEvent.change(composer, { target: { value: "bonjour" } });
    fireEvent.keyDown(composer, { key: "Enter" });
    fireEvent.click(await screen.findByLabelText("Arrêter"));
    expect(signal?.aborted).toBe(true);
    expect((composer as HTMLTextAreaElement).value).toBe("bonjour");
    expect(screen.queryByLabelText("Arrêter")).toBeNull();
  });

  it("disables attachments without an attach adapter", () => {
    const api = fakeApi();
    delete api.attach;
    const { composer } = renderPanel({ api });
    expect(screen.queryByLabelText("Joindre un fichier")).toBeNull();
    expect(fireEvent.paste(composer, { clipboardData: clipboard([file("a.txt", 1, "text/plain")]) })).toBe(true);
  });
});

describe("relativeTime", () => {
  const now = Date.parse("2026-10-10T12:00:00Z");
  it("renders minutes, hours and days in French", () => {
    expect(relativeTime("2026-10-10T11:55:00Z", now)).toBe("il y a 5 min");
    expect(relativeTime("2026-10-09T22:00:00Z", now)).toBe("il y a 14 heures");
    expect(relativeTime("2026-10-08T12:00:00Z", now)).toBe("il y a 2 jours");
  });
});
