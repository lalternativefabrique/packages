import { describe, expect, it } from "vitest";
import { capDocked, emptyWorkspace, placeNear, workspaceReducer, type WorkspaceAction, type WorkspaceState } from "./workspace-state";

const rect = { x: 100, y: 80, width: 480, height: 640 };

function run(...actions: WorkspaceAction[]): WorkspaceState {
  return actions.reduce(workspaceReducer, emptyWorkspace);
}

const modes = (s: WorkspaceState) => Object.fromEntries(s.sessions.map((x) => [x.id, x.mode]));

describe("workspaceReducer", () => {
  it("opens docked by default and floating on request", () => {
    const s = run({ type: "open", id: "a", scopeId: "p" }, { type: "open", id: "b", scopeId: "p", mode: "floating", rect });
    expect(modes(s)).toEqual({ a: "docked", b: "floating" });
  });

  it("reopens the session already showing a conversation instead of duplicating it", () => {
    const s = run(
      { type: "open", id: "a", scopeId: "p", conversationId: "c1" },
      { type: "minimize", id: "a" },
      { type: "open", id: "b", scopeId: "p", conversationId: "c1" },
    );
    expect(s.sessions).toHaveLength(1);
    expect(s.sessions[0].mode).toBe("docked");
  });

  it("minimizes all and restores each to where it was", () => {
    const s = run(
      { type: "open", id: "a", scopeId: "p" },
      { type: "open", id: "b", scopeId: "p", mode: "floating", rect },
      { type: "minimizeAll" },
    );
    expect(modes(s)).toEqual({ a: "minimized", b: "minimized" });
    expect(modes(workspaceReducer(s, { type: "restoreAll" }))).toEqual({ a: "docked", b: "floating" });
  });

  it("counts answers that land while minimized and clears them on restore", () => {
    let s = run({ type: "open", id: "a", scopeId: "p" }, { type: "minimize", id: "a" }, { type: "answered", id: "a" });
    expect(s.sessions[0].unread).toBe(1);
    s = workspaceReducer(s, { type: "restore", id: "a" });
    expect(s.sessions[0].unread).toBe(0);
  });

  it("detaches to a floating window and docks back", () => {
    let s = run({ type: "open", id: "a", scopeId: "p" }, { type: "detach", id: "a", rect });
    expect(s.sessions[0]).toMatchObject({ mode: "floating", rect });
    s = workspaceReducer(s, { type: "dock", id: "a" });
    expect(s.sessions[0].mode).toBe("docked");
  });

  it("raises the focused window above the others", () => {
    const s = run(
      { type: "open", id: "a", scopeId: "p", mode: "floating", rect },
      { type: "open", id: "b", scopeId: "p", mode: "floating", rect },
      { type: "focus", id: "a" },
    );
    const [a, b] = s.sessions;
    expect(a.z).toBeGreaterThan(b.z);
  });
});

describe("capDocked", () => {
  it("sends the least recently focused docked columns to the dock", () => {
    const s = run(
      { type: "open", id: "a", scopeId: "p" },
      { type: "open", id: "b", scopeId: "p" },
      { type: "open", id: "c", scopeId: "p" },
      { type: "focus", id: "a" },
    );
    expect(modes(capDocked(s, 2))).toEqual({ a: "docked", b: "minimized", c: "docked" });
  });
});

describe("placeNear", () => {
  const size = { width: 400, height: 500 };
  const viewport = { width: 1440, height: 900 };

  it("opens right of the selection when it fits", () => {
    expect(placeNear({ x: 200, y: 100, width: 300, height: 20 }, size, viewport)).toMatchObject({ x: 512, y: 100 });
  });

  it("falls back to the left near the right edge", () => {
    expect(placeNear({ x: 1000, y: 100, width: 300, height: 20 }, size, viewport).x).toBe(588);
  });

  it("stays inside the viewport at the bottom", () => {
    expect(placeNear({ x: 200, y: 850, width: 300, height: 20 }, size, viewport).y).toBe(384);
  });
});
