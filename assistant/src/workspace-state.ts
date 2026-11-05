export type SessionMode = "docked" | "floating" | "minimized";
export type SessionStatus = "idle" | "running" | "error";

export interface WindowRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface AssistantSession {
  id: string;
  scopeId: string;
  conversationId?: string;
  title: string;
  mode: SessionMode;
  restoreTo: Exclude<SessionMode, "minimized">;
  rect?: WindowRect;
  status: SessionStatus;
  unread: number;
  z: number;
}

export interface WorkspaceState {
  sessions: AssistantSession[];
  nextZ: number;
}

export type WorkspaceAction =
  | { type: "open"; id: string; scopeId: string; conversationId?: string; mode?: "docked" | "floating"; rect?: WindowRect }
  | { type: "close"; id: string }
  | { type: "focus"; id: string }
  | { type: "minimize"; id: string }
  | { type: "minimizeAll" }
  | { type: "restore"; id: string }
  | { type: "restoreAll" }
  | { type: "detach"; id: string; rect: WindowRect }
  | { type: "dock"; id: string }
  | { type: "move"; id: string; rect: WindowRect }
  | { type: "status"; id: string; status: SessionStatus }
  | { type: "conversation"; id: string; conversationId: string; title?: string }
  | { type: "answered"; id: string };

export const emptyWorkspace: WorkspaceState = { sessions: [], nextZ: 1 };

function update(state: WorkspaceState, id: string, patch: (s: AssistantSession) => AssistantSession): WorkspaceState {
  return { ...state, sessions: state.sessions.map((s) => (s.id === id ? patch(s) : s)) };
}

function raise(state: WorkspaceState, id: string): WorkspaceState {
  const z = state.nextZ;
  return { ...update(state, id, (s) => ({ ...s, z })), nextZ: z + 1 };
}

function visible(s: AssistantSession): boolean {
  return s.mode !== "minimized";
}

// Docked columns beyond maxDocked fall to the dock, oldest focus first, so
// the page never ends up covered by a wall of columns.
export function capDocked(state: WorkspaceState, maxDocked: number): WorkspaceState {
  const docked = state.sessions.filter((s) => s.mode === "docked").sort((a, b) => b.z - a.z);
  const overflow = new Set(docked.slice(Math.max(0, maxDocked)).map((s) => s.id));
  if (overflow.size === 0) return state;
  return {
    ...state,
    sessions: state.sessions.map((s) => (overflow.has(s.id) ? { ...s, mode: "minimized", restoreTo: "docked" } : s)),
  };
}

export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  switch (action.type) {
    case "open": {
      const existing = action.conversationId
        ? state.sessions.find((s) => s.conversationId === action.conversationId)
        : undefined;
      if (existing) return workspaceReducer(state, { type: "restore", id: existing.id });
      const mode = action.mode ?? "docked";
      const session: AssistantSession = {
        id: action.id,
        scopeId: action.scopeId,
        conversationId: action.conversationId,
        title: "",
        mode,
        restoreTo: mode,
        rect: action.rect,
        status: "idle",
        unread: 0,
        z: state.nextZ,
      };
      return { sessions: [...state.sessions, session], nextZ: state.nextZ + 1 };
    }
    case "close":
      return { ...state, sessions: state.sessions.filter((s) => s.id !== action.id) };
    case "focus":
      return raise(state, action.id);
    case "minimize":
      return update(state, action.id, (s) =>
        visible(s) ? { ...s, mode: "minimized", restoreTo: s.mode as AssistantSession["restoreTo"] } : s,
      );
    case "minimizeAll":
      return {
        ...state,
        sessions: state.sessions.map((s) =>
          visible(s) ? { ...s, mode: "minimized", restoreTo: s.mode as AssistantSession["restoreTo"] } : s,
        ),
      };
    case "restore":
      return raise(
        update(state, action.id, (s) => ({ ...s, mode: s.mode === "minimized" ? s.restoreTo : s.mode, unread: 0 })),
        action.id,
      );
    case "restoreAll":
      return {
        ...state,
        sessions: state.sessions.map((s) => (s.mode === "minimized" ? { ...s, mode: s.restoreTo, unread: 0 } : s)),
      };
    case "detach":
      return raise(
        update(state, action.id, (s) => ({ ...s, mode: "floating", restoreTo: "floating", rect: s.rect ?? action.rect })),
        action.id,
      );
    case "dock":
      return raise(update(state, action.id, (s) => ({ ...s, mode: "docked", restoreTo: "docked" })), action.id);
    case "move":
      return update(state, action.id, (s) => ({ ...s, rect: action.rect }));
    case "status":
      return update(state, action.id, (s) => ({ ...s, status: action.status }));
    case "conversation":
      return update(state, action.id, (s) => ({
        ...s,
        conversationId: action.conversationId,
        title: s.title || action.title || "",
      }));
    case "answered":
      return update(state, action.id, (s) => ({ ...s, unread: s.mode === "minimized" ? s.unread + 1 : 0 }));
  }
}

export interface WorkspaceStore {
  load(): WorkspaceState | undefined;
  save(state: WorkspaceState): void;
}

export function sessionStorageStore(key: string): WorkspaceStore {
  return {
    load() {
      try {
        const raw = sessionStorage.getItem(key);
        if (!raw) return undefined;
        const parsed = JSON.parse(raw) as WorkspaceState;
        if (!Array.isArray(parsed.sessions)) return undefined;
        return { ...parsed, sessions: parsed.sessions.map((s) => ({ ...s, status: "idle" })) };
      } catch {
        return undefined;
      }
    },
    save(state) {
      try {
        sessionStorage.setItem(key, JSON.stringify(state));
      } catch {
        return;
      }
    },
  };
}

const GAP = 12;
const MARGIN = 16;

// First position of a window opened from a selection: to its right if it
// fits, else to its left, else below; always kept inside the viewport.
export function placeNear(
  anchor: WindowRect,
  size: { width: number; height: number },
  viewport: { width: number; height: number },
): WindowRect {
  const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(v, Math.max(lo, hi)));
  let x: number;
  let y = anchor.y;
  if (anchor.x + anchor.width + GAP + size.width <= viewport.width - MARGIN) {
    x = anchor.x + anchor.width + GAP;
  } else if (anchor.x - GAP - size.width >= MARGIN) {
    x = anchor.x - GAP - size.width;
  } else {
    x = anchor.x;
    y = anchor.y + anchor.height + GAP;
  }
  return {
    x: clamp(x, MARGIN, viewport.width - MARGIN - size.width),
    y: clamp(y, MARGIN, viewport.height - MARGIN - size.height),
    width: size.width,
    height: size.height,
  };
}
