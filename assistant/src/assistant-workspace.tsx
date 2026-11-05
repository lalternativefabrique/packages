import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  type PointerEvent,
  type ReactNode,
} from "react";
import { AssistantPanel } from "./assistant-panel";
import { Icon } from "./icons";
import { defaultLabels } from "./labels";
import type { AssistantLabels, AssistantPanelProps, AssistantSelection } from "./types";
import {
  capDocked,
  emptyWorkspace,
  placeNear,
  sessionStorageStore,
  workspaceReducer,
  type AssistantSession,
  type WindowRect,
  type WorkspaceStore,
} from "./workspace-state";

export interface OpenAssistantOptions {
  scopeId?: string;
  conversationId?: string;
  selection?: AssistantSelection;
  mode?: "docked" | "floating";
  anchor?: WindowRect;
}

export interface AssistantWorkspaceApi {
  sessions: AssistantSession[];
  open(options?: OpenAssistantOptions): string;
  close(id: string): void;
  focus(id: string): void;
  minimize(id: string): void;
  minimizeAll(): void;
  restore(id: string): void;
  restoreAll(): void;
  detach(id: string): void;
  dock(id: string): void;
}

// A native shell (Orbit) shows floating sessions in its own OS windows; the
// lib then renders only docked columns and the dock.
export interface NativeWindowHost {
  kind: "native";
  open(session: AssistantSession): void;
  move(id: string, rect: WindowRect): void;
  close(id: string): void;
}

export type WindowHost = { kind: "inline" } | NativeWindowHost;

export type AssistantWorkspaceProps = Omit<
  AssistantPanelProps,
  "onClose" | "initialHistory" | "initialSelection" | "embedded" | "initialConversationId"
> & {
  children?: ReactNode;
  windowHost?: WindowHost;
  store?: WorkspaceStore;
  maxDocked?: number;
  showLauncher?: boolean;
};

const WorkspaceContext = createContext<AssistantWorkspaceApi | null>(null);

export function useAssistantWorkspace(): AssistantWorkspaceApi {
  const ws = useContext(WorkspaceContext);
  if (!ws) throw new Error("useAssistantWorkspace must be used inside <AssistantWorkspace>");
  return ws;
}

const FLOATING_SIZE = { width: 480, height: 640 };

function viewport() {
  return typeof window === "undefined"
    ? { width: 1440, height: 900 }
    : { width: window.innerWidth, height: window.innerHeight };
}

function defaultRect(offset: number): WindowRect {
  const v = viewport();
  const step = 32 * (offset % 6);
  return {
    x: Math.max(16, v.width - FLOATING_SIZE.width - 48 - step),
    y: Math.max(16, 72 + step),
    ...FLOATING_SIZE,
  };
}

let sequence = 0;
function newId(): string {
  sequence += 1;
  return `a${Date.now().toString(36)}${sequence}`;
}

function useDragRect(onRect: (rect: WindowRect) => void) {
  return (rect: WindowRect, kind: "move" | "resize") => (e: PointerEvent<HTMLElement>) => {
    if (e.button !== 0) return;
    if (kind === "move" && (e.target as HTMLElement).closest("button, select, input, textarea, a")) return;
    e.preventDefault();
    const start = { x: e.clientX, y: e.clientY };
    const move = (ev: globalThis.PointerEvent) => {
      const dx = ev.clientX - start.x;
      const dy = ev.clientY - start.y;
      const v = viewport();
      onRect(
        kind === "move"
          ? {
              ...rect,
              x: Math.min(Math.max(0, rect.x + dx), v.width - 80),
              y: Math.min(Math.max(0, rect.y + dy), v.height - 48),
            }
          : { ...rect, width: Math.max(360, rect.width + dx), height: Math.max(320, rect.height + dy) },
      );
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
}

export function AssistantWorkspace({
  children,
  windowHost = { kind: "inline" },
  store: storeProp,
  maxDocked = 2,
  showLauncher = true,
  ...panelProps
}: AssistantWorkspaceProps) {
  const labels: AssistantLabels = { ...defaultLabels, ...panelProps.labels };
  const store = useMemo(() => storeProp ?? sessionStorageStore("lalt.assistant.workspace"), [storeProp]);
  const [state, dispatch] = useReducer(workspaceReducer, undefined, () => store.load() ?? emptyWorkspace);
  const selections = useRef(new Map<string, AssistantSelection>());

  useEffect(() => store.save(state), [store, state]);

  const docked = state.sessions.filter((s) => s.mode === "docked").length;
  useEffect(() => {
    if (docked > maxDocked) {
      const capped = capDocked(state, maxDocked);
      for (const s of capped.sessions) {
        const before = state.sessions.find((x) => x.id === s.id);
        if (before?.mode === "docked" && s.mode === "minimized") dispatch({ type: "minimize", id: s.id });
      }
    }
  }, [docked, maxDocked, state]);

  const native = windowHost.kind === "native" ? windowHost : null;
  const nativeOpen = useRef(new Set<string>());
  useEffect(() => {
    if (!native) return;
    const floating = new Set(state.sessions.filter((s) => s.mode === "floating").map((s) => s.id));
    for (const s of state.sessions) {
      if (floating.has(s.id) && !nativeOpen.current.has(s.id)) {
        native.open(s);
        nativeOpen.current.add(s.id);
      }
    }
    for (const id of nativeOpen.current) {
      if (!floating.has(id)) {
        native.close(id);
        nativeOpen.current.delete(id);
      }
    }
  }, [native, state.sessions]);

  const api = useMemo<AssistantWorkspaceApi>(
    () => ({
      sessions: state.sessions,
      open(options = {}) {
        const id = newId();
        if (options.selection) selections.current.set(id, options.selection);
        const mode = options.mode ?? "docked";
        const rect =
          mode === "floating"
            ? options.anchor
              ? placeNear(options.anchor, FLOATING_SIZE, viewport())
              : defaultRect(state.sessions.length)
            : undefined;
        dispatch({
          type: "open",
          id,
          scopeId: options.scopeId ?? panelProps.scopeId,
          conversationId: options.conversationId,
          mode,
          rect,
        });
        return id;
      },
      close: (id) => dispatch({ type: "close", id }),
      focus: (id) => dispatch({ type: "focus", id }),
      minimize: (id) => dispatch({ type: "minimize", id }),
      minimizeAll: () => dispatch({ type: "minimizeAll" }),
      restore: (id) => dispatch({ type: "restore", id }),
      restoreAll: () => dispatch({ type: "restoreAll" }),
      detach: (id) => dispatch({ type: "detach", id, rect: defaultRect(state.sessions.length) }),
      dock: (id) => dispatch({ type: "dock", id }),
    }),
    [state.sessions, panelProps.scopeId],
  );

  const setRect = useCallback(
    (id: string) => (rect: WindowRect) => {
      dispatch({ type: "move", id, rect });
      native?.move(id, rect);
    },
    [native],
  );

  const dockedSessions = state.sessions.filter((s) => s.mode === "docked");
  const minimized = state.sessions.filter((s) => s.mode === "minimized");
  const many = state.sessions.filter((s) => s.mode !== "minimized").length > 1;

  const panelFor = (s: AssistantSession, actions: ReactNode, onHeaderPointerDown?: AssistantPanelProps["onHeaderPointerDown"]) => (
    <AssistantPanel
      {...panelProps}
      embedded
      scopeId={s.scopeId}
      initialConversationId={s.conversationId}
      initialSelection={selections.current.get(s.id)}
      onClose={() => api.close(s.id)}
      headerActions={actions}
      onHeaderPointerDown={onHeaderPointerDown}
      onRunningChange={(running) => dispatch({ type: "status", id: s.id, status: running ? "running" : "idle" })}
      onConversationChange={(conversationId, title) => dispatch({ type: "conversation", id: s.id, conversationId, title })}
      onAnswered={() => dispatch({ type: "answered", id: s.id })}
    />
  );

  const headerActions = (s: AssistantSession) => (
    <>
      {many && (
        <button type="button" onClick={api.minimizeAll} className="lalt-assistant-text-button">
          {labels.minimizeAll}
        </button>
      )}
      <button
        type="button"
        onClick={() => (s.mode === "floating" ? api.dock(s.id) : api.detach(s.id))}
        aria-label={s.mode === "floating" ? labels.dock : labels.detach}
        className="lalt-assistant-icon-button"
      >
        <Icon name={s.mode === "floating" ? "dock" : "detach"} />
      </button>
      <button type="button" onClick={() => api.minimize(s.id)} aria-label={labels.minimize} className="lalt-assistant-icon-button">
        <Icon name="dash" />
      </button>
    </>
  );

  const drag = useDragRect;

  return (
    <WorkspaceContext.Provider value={api}>
      {children}
      {state.sessions.map((s) => {
        const hiddenHere = s.mode === "minimized" || (native !== null && s.mode === "floating");
        const column = dockedSessions.indexOf(s);
        const rect = s.rect ?? defaultRect(0);
        const onRect = setRect(s.id);
        const style =
          s.mode === "docked"
            ? { right: `calc(${column} * var(--lalt-assistant-column, 480px))`, zIndex: 50 }
            : s.mode === "floating"
              ? { left: rect.x, top: rect.y, width: rect.width, height: rect.height, zIndex: 60 + s.z }
              : undefined;
        return (
          <section
            key={s.id}
            hidden={hiddenHere}
            aria-label={s.title || labels.untitled}
            className={`lalt-assistant lalt-assistant-${s.mode === "floating" ? "window" : "column"}`}
            style={style}
            onPointerDown={() => api.focus(s.id)}
          >
            {panelFor(s, hiddenHere ? null : headerActions(s), s.mode === "floating" ? drag(onRect)(rect, "move") : undefined)}
            {s.mode === "floating" && (
              <span
                className="lalt-assistant-window-resize"
                role="separator"
                aria-label={labels.resize}
                onPointerDown={drag(onRect)(rect, "resize")}
              />
            )}
          </section>
        );
      })}
      {(minimized.length > 0 || (showLauncher && state.sessions.length === 0)) && (
        <nav aria-label={labels.dockRegion} className="lalt-assistant lalt-assistant-dock">
          {minimized.map((s) => (
            <button key={s.id} type="button" onClick={() => api.restore(s.id)} className="lalt-assistant-dock-chip" data-unread={s.unread > 0 || undefined}>
              {s.status === "running" ? <span className="lalt-assistant-spinner" /> : <Icon name="bot" size={14} />}
              <span className="lalt-assistant-dock-text">
                <span className="lalt-assistant-dock-title">{s.title || labels.untitled}</span>
                <span className="lalt-assistant-muted lalt-assistant-small">
                  {s.status === "running" ? labels.running : s.unread > 0 ? labels.ready : ""}
                </span>
              </span>
              {s.unread > 0 && <span className="lalt-assistant-badge">{s.unread}</span>}
            </button>
          ))}
          {minimized.length > 1 && (
            <button type="button" onClick={api.restoreAll} className="lalt-assistant-text-button">
              {labels.restoreAll}
            </button>
          )}
          <button type="button" onClick={() => api.open()} className="lalt-assistant-launch lalt-assistant-launch-primary">
            <Icon name="plus" />
            {labels.newAssistant}
          </button>
        </nav>
      )}
    </WorkspaceContext.Provider>
  );
}
