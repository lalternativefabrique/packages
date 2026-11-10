import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SuperInput, type SuperInputShell, type SuperInputShellRun } from "./super-input";
import { lastLine, matchCommands, pushHistory, shellCommand, stepHistory, superInputMode } from "./super-input-mode";

afterEach(cleanup);

describe("superInputMode", () => {
  it("tells prompt, slash, command and shell apart", () => {
    expect(superInputMode("bonjour", true)).toBe("prompt");
    expect(superInputMode("/", true)).toBe("slash");
    expect(superInputMode("/rev", true)).toBe("slash");
    expect(superInputMode("/review le diff", true)).toBe("command");
    expect(superInputMode("  ! ls", true)).toBe("shell");
    expect(superInputMode("! ls", false)).toBe("noshell");
  });

  it("extracts the shell command and filters commands by prefix", () => {
    expect(shellCommand("!  git status ")).toBe("git status");
    expect(shellCommand("!")).toBe("");
    const commands = [{ name: "review" }, { name: "publish" }, { name: "Revert" }];
    expect(matchCommands("/re", commands).map((c) => c.name)).toEqual(["review", "Revert"]);
    expect(matchCommands("/re x", commands)).toEqual([]);
  });

  it("keeps history deduplicated, bounded and steps within it", () => {
    expect(pushHistory(["a"], "a")).toEqual(["a"]);
    expect(pushHistory(["a"], "  ")).toEqual(["a"]);
    expect(pushHistory(["a", "b"], "c", 2)).toEqual(["b", "c"]);
    expect(stepHistory(3, 3, -1)).toBe(2);
    expect(stepHistory(3, 0, -1)).toBe(0);
    expect(stepHistory(3, 3, 1)).toBe(3);
    expect(lastLine("a\nb\n")).toBe("b");
  });
});

const box = () => screen.getByRole("textbox", { name: "Message" });

describe("SuperInput", () => {
  it("navigates the slash menu with the keyboard and runs the picked command", () => {
    const onCommand = vi.fn();
    render(<SuperInput onSend={vi.fn()} commands={[{ name: "review", description: "Relire" }, { name: "publish" }, { name: "clear" }]} onCommand={onCommand} />);
    fireEvent.change(box(), { target: { value: "/" } });
    expect(screen.getByRole("listbox", { name: "Commandes" })).toBeTruthy();
    expect(screen.getAllByRole("option")).toHaveLength(3);
    fireEvent.keyDown(box(), { key: "ArrowDown" });
    expect(screen.getByRole("option", { name: /publish/ }).getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(box(), { key: "ArrowUp" });
    fireEvent.keyDown(box(), { key: "ArrowUp" });
    expect(screen.getByRole("option", { name: /clear/ }).getAttribute("aria-selected")).toBe("true");
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onCommand).toHaveBeenCalledWith(expect.objectContaining({ name: "clear" }));
    expect((box() as HTMLTextAreaElement).value).toBe("");
  });

  it("fills the field with a prefill command and closes the menu on Escape", () => {
    render(<SuperInput onSend={vi.fn()} commands={[{ name: "browser", prefill: true }]} onCommand={vi.fn()} />);
    fireEvent.change(box(), { target: { value: "/b" } });
    fireEvent.keyDown(box(), { key: "Tab" });
    expect((box() as HTMLTextAreaElement).value).toBe("/browser ");
    fireEvent.change(box(), { target: { value: "/b" } });
    fireEvent.keyDown(box(), { key: "Escape" });
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("without a shell, ! shows the shell is elsewhere and sends nothing", () => {
    const onSend = vi.fn();
    render(<SuperInput onSend={onSend} labels={{ shellUnavailable: "Le shell est dans kapal" }} />);
    fireEvent.change(box(), { target: { value: "! ls" } });
    expect(screen.getByText("Le shell est dans kapal")).toBeTruthy();
    expect(screen.getByText("Shell indisponible")).toBeTruthy();
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onSend).not.toHaveBeenCalled();
  });

  it("with a shell, runs the command, renders the output inside and stops on Ctrl+C", () => {
    const run = vi.fn();
    const stop = vi.fn();
    const onSend = vi.fn();
    const shell = (runs: SuperInputShellRun[]): SuperInputShell => ({ runs, run, stop, recent: ["git status"], cwd: "~/code" });
    const { rerender, container } = render(<SuperInput onSend={onSend} shell={shell([])} />);
    fireEvent.change(box(), { target: { value: "!" } });
    expect(screen.getByRole("listbox", { name: "Commandes récentes" })).toBeTruthy();
    fireEvent.change(box(), { target: { value: "! ls -la" } });
    expect(container.querySelector(".lalt-si")?.getAttribute("data-dark")).toBe("true");
    expect(screen.getByText("Entrée exécute dans ~/code")).toBeTruthy();
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(run).toHaveBeenCalledWith("ls -la");
    expect(onSend).not.toHaveBeenCalled();

    rerender(<SuperInput onSend={onSend} shell={shell([{ id: 1, command: "ls -la", output: "a\nb\n", running: true, code: null }])} />);
    expect(screen.getByText(/a\s+b/)).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "Arrêter" })).toHaveLength(2);
    fireEvent.keyDown(box(), { key: "c", ctrlKey: true });
    expect(stop).toHaveBeenCalledWith(1);

    rerender(<SuperInput onSend={onSend} shell={shell([{ id: 1, command: "ls -la", output: "a\nb\n", running: false, code: 0, durationMs: 1200 }])} />);
    expect(screen.getByText(/code 0/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Réduire à la dernière ligne" }));
    expect(container.querySelector(".lalt-si-run-out")?.textContent).toBe("b");
    fireEvent.click(screen.getByRole("button", { name: "Fermer la sortie" }));
    expect(container.querySelector(".lalt-si-run")).toBeNull();
  });

  it("recalls history with Up and Down, then restores the draft", () => {
    const onHistoryChange = vi.fn();
    render(<SuperInput onSend={vi.fn()} history={["premier", "second"]} onHistoryChange={onHistoryChange} />);
    fireEvent.change(box(), { target: { value: "brouillon" } });
    fireEvent.keyDown(box(), { key: "ArrowUp" });
    expect((box() as HTMLTextAreaElement).value).toBe("second");
    fireEvent.keyDown(box(), { key: "ArrowUp" });
    expect((box() as HTMLTextAreaElement).value).toBe("premier");
    fireEvent.keyDown(box(), { key: "ArrowDown" });
    fireEvent.keyDown(box(), { key: "ArrowDown" });
    expect((box() as HTMLTextAreaElement).value).toBe("brouillon");
    fireEvent.keyDown(box(), { key: "Enter" });
    expect(onHistoryChange).toHaveBeenCalledWith(["premier", "second", "brouillon"]);
  });

  it("puts the text back when the host refuses it", async () => {
    render(<SuperInput onSend={() => Promise.resolve(false)} />);
    fireEvent.change(box(), { target: { value: "refusé" } });
    fireEvent.click(screen.getByRole("button", { name: "Envoyer" }));
    expect((box() as HTMLTextAreaElement).value).toBe("");
    await waitFor(() => expect((box() as HTMLTextAreaElement).value).toBe("refusé"));
  });

  it("opens the tall editor and picks a reasoning level from the dropdown", () => {
    const onChange = vi.fn();
    const { container } = render(<SuperInput onSend={vi.fn()} reasoning={{ options: ["none", "low", "medium", "high"], value: "medium", onChange }} />);
    fireEvent.click(screen.getByRole("button", { name: "Agrandir l'éditeur" }));
    expect(container.querySelector(".lalt-si")?.getAttribute("data-expanded")).toBe("true");
    fireEvent.click(screen.getByRole("button", { name: "Réflexion : Moyenne" }));
    fireEvent.click(screen.getByRole("option", { name: /Poussée/ }));
    expect(onChange).toHaveBeenCalledWith("high");
  });
});
