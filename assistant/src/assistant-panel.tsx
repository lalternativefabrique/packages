import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useAttachments, pastedFiles, type PendingAttachment } from "./attachments";
import { errorMessage, formatSize, relativeTime } from "./format";
import { MicBars, useDictation } from "./dictation";
import { Icon } from "./icons";

const RAIL_QUERY = "(min-width: 760px)";

function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() => typeof window !== "undefined" && !!window.matchMedia?.(query).matches);
  useEffect(() => {
    const mq = typeof window !== "undefined" ? window.matchMedia?.(query) : undefined;
    if (!mq) return;
    const on = () => setMatches(mq.matches);
    on();
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, [query]);
  return matches;
}
import { defaultLabels } from "./labels";
import { SafeMarkdown } from "./markdown";
import type {
  AssistantErrorCode,
  AssistantAttachment,
  AssistantSource,
  AssistantConversationSummary,
  AssistantLabels,
  AssistantMessage,
  AssistantPanelProps,
  AssistantEvent,
  AssistantScope,
  AssistantSelection,
  AssistantSendInput,
  AssistantSendResult,
  AssistantToolCall,
  ReasoningEffort,
} from "./types";
import { applyEvent, isAbort } from "./stream";
import { ReasoningGauge } from "./reasoning-gauge";

const MIN_WIDTH = 360;
const DEFAULT_WIDTH = 520;
const PASTED_TEXT_NAME = "texte-colle.txt";
const RESUME_ATTEMPTS = 3;
const RESUME_DELAY_MS = 1000;

function storedWidth(key: string): number {
  try {
    const v = Number(localStorage.getItem(key));
    return v >= MIN_WIDTH ? v : DEFAULT_WIDTH;
  } catch {
    return DEFAULT_WIDTH;
  }
}

function usePanelWidth(key: string) {
  const [width, setWidth] = useState(() => storedWidth(key));
  const [expanded, setExpanded] = useState(false);
  const startResize = (e: ReactPointerEvent) => {
    e.preventDefault();
    setExpanded(false);
    let latest = width;
    const move = (ev: PointerEvent) => {
      latest = Math.max(MIN_WIDTH, Math.min(window.innerWidth - 80, window.innerWidth - ev.clientX));
      setWidth(latest);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      try {
        localStorage.setItem(key, String(latest));
      } catch {
        return;
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return { width, expanded, setExpanded, startResize };
}

export function AssistantPanel({
  api,
  scopes,
  scopeId,
  onScopeChange,
  scopeLocked = false,
  quickActions = [],
  attachmentLimits,
  pasteAsFileThreshold,
  labels: labelOverrides,
  renderMarkdown,
  renderToolCall,
  renderComposerAccessory,
  banner,
  bodyOverride,
  ask,
  draftSeed,
  sendExtra,
  historyRail = false,
  renderHistory,
  aboveComposer,
  subtitle,
  emptyState,
  agentName,
  messageActions = [],
  onApplySuggestion,
  composerOptions = [],
  composerOverlay: hostOverlay,
  onComposerEscape,
  transcribe,
  dictationLanguage,
  singleConversation = false,
  showSources = true,
  disabledReason,
  renderScopePicker,
  renderMessageFooter,
  readScreen,
  widthStorageKey = "lalt.assistant.width",
  onClose,
  initialHistory = false,
  reasoning,
  initialSelection,
  embedded = false,
  initialConversationId,
  headerActions,
  onHeaderPointerDown,
  onRunningChange,
  onConversationChange,
  onAnswered,
}: AssistantPanelProps) {
  const labels: AssistantLabels = { ...defaultLabels, ...labelOverrides };
  const scopeLabel = scopes.find((s) => s.id === scopeId)?.label ?? "";

  const [conversationId, setConversationId] = useState<string | undefined>();
  const [thread, setThread] = useState<AssistantMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [historyOpen, setHistoryOpen] = useState(initialHistory);
  const [menuOpen, setMenuOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [loading, setLoading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [conversations, setConversations] = useState<AssistantConversationSummary[]>([]);
  const [resumable, setResumable] = useState<string | undefined>();
  const conversationIdRef = useRef<string | undefined>(undefined);
  const [optionsOpen, setOptionsOpen] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const textarea = useRef<HTMLTextAreaElement>(null);
  const composer = useMemo(
    () => ({
      insert: (text: string) => setDraft((d) => (d.trim() ? `${d.trimEnd()} ${text}` : text)),
      focus: () => textarea.current?.focus(),
    }),
    [],
  );
  const dictation = useDictation(
    transcribe,
    useCallback(
      (text: string) => {
        composer.insert(text);
        composer.focus();
      },
      [composer],
    ),
    labels,
    dictationLanguage,
  );
  const recording = dictation.state === "recording";
  const composerOverlay =
    hostOverlay ?? (recording ? <MicBars /> : dictation.error ? <span className="lalt-assistant-mic-error">{dictation.error}</span> : undefined);
  const railShown = !!(historyRail || renderHistory) && !singleConversation && !bodyOverride;
  const railFits = useMediaQuery(RAIL_QUERY);
  const endRef = useRef<HTMLDivElement>(null);
  const generation = useRef(0);
  const inFlight = useRef<AbortController | null>(null);
  const [effort, setEffort] = useState<ReasoningEffort | undefined>();
  const [selection, setSelection] = useState<AssistantSelection | undefined>(initialSelection);
  useEffect(() => setSelection(initialSelection), [initialSelection]);

  const attachments = useAttachments({ attach: api.attach?.bind(api), scopeId, limits: attachmentLimits, labels });

  const refreshConversations = useCallback(() => {
    api.listConversations().then(setConversations, (err: unknown) => setError(errorMessage(err, labels.requestFailed)));
  }, [api, labels.requestFailed]);

  useEffect(refreshConversations, [refreshConversations]);

  useEffect(() => {
    endRef.current?.scrollIntoView?.({ block: "end" });
  }, [thread.length, pending]);

  conversationIdRef.current = conversationId;
  const runningChange = useRef(onRunningChange);
  runningChange.current = onRunningChange;
  useEffect(() => runningChange.current?.(pending), [pending]);

  const liveConversation = useRef<string | undefined>(undefined);
  const stop = () => {
    if (!inFlight.current) return;
    inFlight.current.abort();
    const id = liveConversation.current;
    if (id) api.stop?.(id).catch(() => undefined);
  };

  const newConversation = () => {
    stop();
    generation.current++;
    setEffort(undefined);
    setThread([]);
    setConversationId(undefined);
    setError(null);
    setHistoryOpen(false);
    setMenuOpen(false);
    setPending(false);
    setResumable(undefined);
    attachments.clear();
  };

  const openConversation = (c: AssistantConversationSummary) => {
    stop();
    const current = ++generation.current;
    setHistoryOpen(false);
    setMenuOpen(false);
    setError(null);
    setLoading(true);
    api.getConversation(c.id).then(
      (detail) => {
        if (current !== generation.current) return;
        setThread(detail.messages);
        setConversationId(detail.id);
        if (detail.live) resumeLive(detail.id);
        onConversationChange?.(detail.id, c.title);
        if (detail.scopeId && detail.scopeId !== scopeId) onScopeChange?.(detail.scopeId);
        setLoading(false);
      },
      (err: unknown) => {
        if (current !== generation.current) return;
        setError(errorMessage(err, labels.requestFailed));
        setLoading(false);
      },
    );
  };

  const initialConversation = useRef(initialConversationId);
  useEffect(() => {
    const id = initialConversation.current;
    initialConversation.current = undefined;
    if (id) {
      openConversation({ id, title: "" });
      return;
    }
    if (!singleConversation) return;
    api.listConversations().then(
      (list) => {
        const existing = list.find((c) => !c.scopeId || c.scopeId === scopeId);
        if (existing && !conversationIdRef.current) openConversation(existing);
      },
      () => undefined,
    );
  }, []);

  const canSend = !!scopeId && !pending && !attachments.uploading && !disabledReason;

  const runTurn = (
    current: number,
    title: string,
    start: (emit: (event: AssistantEvent) => void, signal: AbortSignal) => Promise<AssistantSendResult>,
    knownConversation: string | undefined,
    attempt = 0,
  ) => {
    setPending(true);
    setResumable(undefined);
    liveConversation.current = knownConversation;
    const controller = new AbortController();
    inFlight.current = controller;
    setThread((t) => (t[t.length - 1]?.streaming ? t : [...t, { role: "assistant", content: "", streaming: true }]));
    const emit = (event: AssistantEvent) => {
      if (current !== generation.current) return;
      setThread((t) => {
        const last = t[t.length - 1];
        return last?.streaming ? [...t.slice(0, -1), applyEvent(last, event, Date.now())] : t;
      });
    };
    const settle = () => {
      if (inFlight.current === controller) inFlight.current = null;
      setPending(false);
    };
    start(emit, controller.signal).then(
      (res) => {
        if (current !== generation.current) return;
        settle();
        liveConversation.current = res.conversationId;
        setConversationId(res.conversationId);
        onConversationChange?.(res.conversationId, title.slice(0, 80));
        onAnswered?.();
        setThread((t) => {
          const last = t[t.length - 1];
          const base: AssistantMessage = last?.streaming ? last : { role: "assistant", content: "" };
          const done: AssistantMessage = {
            ...base,
            role: "assistant",
            content: res.answer,
            meta: res.meta ?? base.meta,
            sources: res.sources ?? base.sources,
            suggestions: res.suggestions ?? base.suggestions,
            attachments: res.attachments ?? base.attachments,
            streaming: false,
            step: undefined,
          };
          return last?.streaming ? [...t.slice(0, -1), done] : [...t, done];
        });
        refreshConversations();
      },
      (err: unknown) => {
        if (current !== generation.current) return;
        settle();
        const code = streamErrorCode(err);
        const live = liveConversation.current;
        if (isAbort(err)) {
          setThread((t) => {
            const last = t[t.length - 1];
            return last?.streaming ? [...t.slice(0, -1), { ...last, streaming: false, stopped: true }] : t;
          });
          return;
        }
        if (code === "nothing_to_stream") {
          setThread((t) => (t[t.length - 1]?.streaming && !t[t.length - 1].content ? t.slice(0, -1) : t));
          return;
        }
        if (code === "connection_lost" && api.resume && live && attempt < RESUME_ATTEMPTS) {
          const resume = api.resume.bind(api);
          setTimeout(() => {
            if (current !== generation.current) return;
            runTurn(current, title, (e, signal) => resume(live, e, signal), live, attempt + 1);
          }, RESUME_DELAY_MS * (attempt + 1));
          return;
        }
        setThread((t) => {
          const last = t[t.length - 1];
          if (!last?.streaming) return t;
          return last.content || last.tools?.length ? [...t.slice(0, -1), { ...last, streaming: false, stopped: true }] : t.slice(0, -1);
        });
        if ((code === "reply_in_progress" || code === "connection_lost") && api.resume && live) setResumable(live);
        setError(
          code === "reply_in_progress" ? labels.replyInProgress : code === "connection_lost" ? labels.connectionLost : errorMessage(err, labels.requestFailed),
        );
      },
    );
  };

  const resumeLive = (id: string) => {
    if (!api.resume) return;
    const resume = api.resume.bind(api);
    setError(null);
    runTurn(generation.current, "", (emit, signal) => resume(id, emit, signal), id);
  };

  const send = (message: string, shownAs: string, askExtra?: Record<string, unknown>, silent = false) => {
    const ready = attachments.done;
    if (!canSend || (!message.trim() && ready.length === 0)) return false;
    const current = generation.current;
    setError(null);
    setHistoryOpen(false);
    const quoted = selection;
    if (!silent) setThread((t) => [...t, { role: "user", content: shownAs, attachments: ready, selection: quoted }]);
    attachments.clear();
    setSelection(undefined);
    const input: AssistantSendInput = {
      scopeId,
      conversationId,
      message,
      attachmentIds: ready.map((a) => a.id),
      screen: readScreen?.(),
      reasoningEffort: effort,
      selection: quoted,
      extra: askExtra ? { ...sendExtra, ...askExtra } : sendExtra,
    };
    const stream = api.stream?.bind(api);
    runTurn(current, shownAs, (emit, signal) => (stream ? stream(input, emit, signal) : api.send(input)), conversationId);
    return true;
  };

  const asked = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!ask || asked.current === ask.id || !canSend) return;
    if (ask.fresh && (conversationId || thread.length > 0)) {
      newConversation();
      return;
    }
    asked.current = ask.id;
    send(ask.text, ask.shownAs ?? ask.text, ask.extra, ask.silent);
  });

  const seeded = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!draftSeed || seeded.current === draftSeed.id) return;
    seeded.current = draftSeed.id;
    if (draftSeed.fresh) newConversation();
    setDraft(draftSeed.text);
    textarea.current?.focus();
  }, [draftSeed]);

  const submit = () => {
    const text = draft.trim();
    if (send(text, text)) setDraft("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
    if (e.key === "Escape" && (onComposerEscape?.() || dictation.cancel())) e.preventDefault();
  };

  const onPaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (!attachments.enabled) return;
    const files = pastedFiles(e.clipboardData, pasteAsFileThreshold, PASTED_TEXT_NAME);
    if (files.length === 0) return;
    e.preventDefault();
    attachments.add(files);
  };

  const onDragOver = (e: DragEvent) => {
    if (!attachments.enabled || !Array.from(e.dataTransfer?.types ?? []).includes("Files")) return;
    e.preventDefault();
    setDragging(true);
  };

  const onDrop = (e: DragEvent) => {
    if (!attachments.enabled) return;
    e.preventDefault();
    setDragging(false);
    attachments.add(Array.from(e.dataTransfer?.files ?? []));
  };

  const historyContext = { currentId: conversationId, open: (id: string) => openConversation({ id, title: "" }), newConversation };
  const streaming = !!thread[thread.length - 1]?.streaming;
  const empty = thread.length === 0 && !pending && !loading;
  const panel = usePanelWidth(widthStorageKey);

  return (
    <aside
      role="dialog"
      aria-label={labels.launcher}
      className={embedded ? "lalt-assistant lalt-assistant-panel lalt-assistant-embedded" : "lalt-assistant lalt-assistant-panel"}
      data-expanded={(!embedded && panel.expanded) || undefined}
      style={embedded ? undefined : { ["--lalt-assistant-width" as string]: panel.expanded ? "min(1100px, 92vw)" : `${panel.width}px` }}
      onDragOver={onDragOver}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false);
      }}
      onDrop={onDrop}
    >
      {!embedded && (
        <div
          onPointerDown={panel.startResize}
          role="separator"
          aria-orientation="vertical"
          aria-label={labels.resize}
          className="lalt-assistant-resize"
        />
      )}
      {dragging && <div className="lalt-assistant-dropzone">{labels.dropHere}</div>}
      <header className="lalt-assistant-header" onPointerDown={onHeaderPointerDown}>
        <div className="lalt-assistant-menu">
          {singleConversation ? (
            <span className="lalt-assistant-title">
              <Icon name="bot" />
              {labels.launcher}
            </span>
          ) : (
            <button type="button" className="lalt-assistant-title" onClick={() => setMenuOpen((v) => !v)} aria-expanded={menuOpen}>
              <Icon name="bot" />
              {labels.launcher}
              <Icon name="chevronDown" size={14} className="lalt-assistant-muted" />
            </button>
          )}
          {subtitle && <span className="lalt-assistant-muted lalt-assistant-small lalt-assistant-subtitle lalt-assistant-truncate">{subtitle}</span>}
          {menuOpen && !singleConversation && (
            <div role="menu" className="lalt-assistant-menu-content">
              <button type="button" role="menuitem" onClick={newConversation}>
                <Icon name="plus" />
                {labels.newConversation}
              </button>
              {conversations.length > 0 && (
                <>
                  <div className="lalt-assistant-menu-label">{labels.recent}</div>
                  {conversations.slice(0, 5).map((c) => (
                    <button key={c.id} type="button" role="menuitem" onClick={() => openConversation(c)}>
                      <span className="lalt-assistant-grow lalt-assistant-truncate">{c.title}</span>
                      <span className="lalt-assistant-muted lalt-assistant-small">{relativeTime(c.updatedAt)}</span>
                    </button>
                  ))}
                </>
              )}
              <div className="lalt-assistant-menu-separator" />
              <button
                type="button"
                role="menuitem"
                onClick={() => {
                  setMenuOpen(false);
                  setHistoryOpen(true);
                }}
              >
                <Icon name="clock" />
                {labels.allHistory}
              </button>
            </div>
          )}
        </div>
        <div className="lalt-assistant-grow" />
        {scopeLocked || scopes.length <= 1 ? (
          <span className="lalt-assistant-muted lalt-assistant-small lalt-assistant-truncate lalt-assistant-scope-label">
            {scopeLabel}
          </span>
        ) : renderScopePicker ? (
          renderScopePicker({
            scopes,
            scopeId,
            label: labels.scope,
            onChange: (id) => {
              onScopeChange?.(id);
              newConversation();
            },
          })
        ) : (
          <select
            value={scopeId}
            onChange={(e) => {
              onScopeChange?.(e.target.value);
              newConversation();
            }}
            aria-label={labels.scope}
            className="lalt-assistant-select"
          >
            {scopes.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        )}
        {!embedded && (
          <button
            type="button"
            onClick={() => panel.setExpanded((v) => !v)}
            aria-label={panel.expanded ? labels.collapse : labels.expand}
            className="lalt-assistant-icon-button lalt-assistant-wide-only"
          >
            <Icon name={panel.expanded ? "minimize" : "maximize"} />
          </button>
        )}
        {headerActions}
        <button type="button" onClick={onClose} aria-label={labels.close} className="lalt-assistant-icon-button">
          <Icon name="x" />
        </button>
      </header>

      <div className="lalt-assistant-main">
      {railShown && (
        <nav aria-label={labels.history} className="lalt-assistant-rail">
          {renderHistory ? (
            renderHistory(historyContext)
          ) : (
            <>
              <button type="button" onClick={newConversation} className="lalt-assistant-text-button">
                <Icon name="plus" size={14} />
                {labels.newConversation}
              </button>
              <HistoryList conversations={conversations} scopes={scopes} labels={labels} onOpen={openConversation} />
            </>
          )}
        </nav>
      )}
      <div className="lalt-assistant-body">
        {banner}
        {bodyOverride ? (
          bodyOverride
        ) : historyOpen && !singleConversation && !(railShown && railFits) ? (
          renderHistory ? (
            renderHistory(historyContext)
          ) : (
            <HistoryList conversations={conversations} scopes={scopes} labels={labels} onOpen={openConversation} />
          )
        ) : disabledReason && empty ? (
          <p className="lalt-assistant-muted lalt-assistant-disabled">{disabledReason}</p>
        ) : empty && emptyState ? (
          emptyState
        ) : empty ? (
          <div>
            <h2 className="lalt-assistant-empty-title">{labels.emptyTitle}</h2>
            {labels.intro && <p className="lalt-assistant-intro">{labels.intro}</p>}
            <div className="lalt-assistant-actions">
              {quickActions.map((a) => (
                <button
                  key={a.id}
                  type="button"
                  onClick={() => send(a.prompt, a.label, a.options)}
                  disabled={!canSend}
                  className="lalt-assistant-action"
                >
                  <div className="lalt-assistant-action-label">{a.label}</div>
                  {a.hint && <div className="lalt-assistant-muted lalt-assistant-small">{a.hint}</div>}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <div className="lalt-assistant-thread">
            {thread.map((m, i) =>
              m.role === "user" ? (
                <div key={i} className="lalt-assistant-user">
                  {m.selection && <blockquote className="lalt-assistant-quote">{m.selection.text}</blockquote>}
                  {m.content}
                  {m.attachments && m.attachments.length > 0 && (
                    <div className="lalt-assistant-sent-files">
                      {m.attachments.map((a) => (
                        <span key={a.id} className="lalt-assistant-sent-file">
                          <Icon name="file" size={12} />
                          {a.filename}
                        </span>
                      ))}
                    </div>
                  )}
                </div>
              ) : (
                <div key={i} className="lalt-assistant-answer" data-streaming={m.streaming || undefined}>
                  {agentName && <span className="lalt-assistant-agent lalt-assistant-small">{agentName}</span>}
                  {m.reasoning && (
                    <details className="lalt-assistant-reasoning">
                      <summary>{labels.reasoningShown}</summary>
                      <div className="lalt-assistant-plain lalt-assistant-muted">{m.reasoning}</div>
                    </details>
                  )}
                  {(() => {
                    const cards = (m.tools ?? []).map((t) => ({ t, node: renderToolCall?.(t, m, { thread, pending }) })).filter((c) => c.node !== undefined);
                    const carded = new Set(cards.map((c) => c.t.id));
                    const lines = (m.tools ?? []).filter((t) => !carded.has(t.id));
                    return (
                      <>
                        {lines.length > 0 && <ToolLines tools={lines} live={!!m.streaming} labels={labels} />}
                        {m.content && (renderMarkdown ? renderMarkdown(m.content, m) : <SafeMarkdown text={m.content} />)}
                        {m.attachments && m.attachments.length > 0 && <AnswerAttachments attachments={m.attachments} />}
                        {cards.map((c) => (
                          <div key={c.t.id} className="lalt-assistant-tool-card">
                            {c.node}
                          </div>
                        ))}
                      </>
                    );
                  })()}
                  {m.streaming && m.step !== undefined && <p className="lalt-assistant-muted lalt-assistant-small">{labels.step(m.step)}</p>}
                  {m.stopped && <p className="lalt-assistant-muted lalt-assistant-small">{labels.stopped}</p>}
                  {!m.streaming && showSources && m.sources && m.sources.length > 0 && <SourceCards sources={m.sources} labels={labels} />}
                  {!m.streaming && m.suggestions && m.suggestions.length > 0 && (
                    <div className="lalt-assistant-suggestions">
                      {m.suggestions.map((sug) => (
                        <button key={sug} type="button" className="lalt-assistant-suggestion" disabled={!onApplySuggestion} onClick={() => onApplySuggestion?.(m, sug)}>
                          {sug}
                        </button>
                      ))}
                    </div>
                  )}
                  {!m.streaming && renderMessageFooter?.(m)}
                  {!m.streaming && messageActions.some((a) => !a.when || a.when(m)) && (
                    <div className="lalt-assistant-message-actions">
                      {messageActions
                        .filter((a) => !a.when || a.when(m))
                        .map((a) => (
                          <button key={a.id} type="button" onClick={() => a.onSelect(m)} aria-label={a.icon ? a.label : undefined} title={a.label} className={a.icon ? "lalt-assistant-icon-button" : "lalt-assistant-text-button"}>
                            {a.icon ?? a.label}
                          </button>
                        ))}
                    </div>
                  )}
                </div>
              ),
            )}
            {(loading || (pending && (!streaming || isSilent(thread[thread.length - 1])))) && (
              <div className="lalt-assistant-muted lalt-assistant-small lalt-assistant-status">
                <span className="lalt-assistant-spinner" />
                {loading ? labels.loading : labels.thinking}
              </div>
            )}
          </div>
        )}
        {error && (
          <p role="alert" className="lalt-assistant-error">
            {error}
            {resumable && (
              <button type="button" onClick={() => resumeLive(resumable)} className="lalt-assistant-text-button">
                {labels.resume}
              </button>
            )}
          </p>
        )}
        <div ref={endRef} />
      </div>
      </div>

      <div className="lalt-assistant-footer">
        {aboveComposer}
        <div className="lalt-assistant-composer">
          {composerOptions.some((o) => o.checked) && (
            <div className="lalt-assistant-chips">
              {composerOptions
                .filter((o) => o.checked)
                .map((o) => (
                  <div key={o.id} className="lalt-assistant-chip" title={o.hint}>
                    <span className="lalt-assistant-chip-text lalt-assistant-truncate">{o.label}</span>
                    <button type="button" onClick={() => o.onToggle(false)} aria-label={labels.removeOption(o.label)} className="lalt-assistant-chip-remove">
                      <Icon name="x" size={12} />
                    </button>
                  </div>
                ))}
            </div>
          )}
          {selection && (
            <div className="lalt-assistant-chips">
              <div className="lalt-assistant-chip lalt-assistant-selection">
                <span className="lalt-assistant-chip-text lalt-assistant-truncate">« {selection.text} »</span>
                <button type="button" onClick={() => setSelection(undefined)} aria-label={labels.removeSelection} className="lalt-assistant-chip-remove">
                  <Icon name="x" size={12} />
                </button>
              </div>
            </div>
          )}
          {(attachments.items.length > 0 || attachments.notice) && (
            <div className="lalt-assistant-chips">
              {attachments.items.map((item) => (
                <AttachmentChip key={item.localId} item={item} labels={labels} onRemove={() => attachments.remove(item.localId)} />
              ))}
              {attachments.notice && <p className="lalt-assistant-error">{attachments.notice}</p>}
            </div>
          )}
          {composerOverlay && <div className="lalt-assistant-composer-overlay">{composerOverlay}</div>}
          <textarea
            ref={textarea}
            disabled={!!disabledReason}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            rows={3}
            placeholder={composerOverlay ? "" : labels.placeholder}
            aria-label="Message"
            className="lalt-assistant-textarea"
          />
          <div className="lalt-assistant-composer-row">
            {composerOptions.length > 0 && (
              <div className="lalt-assistant-options">
                <button type="button" onClick={() => setOptionsOpen((v) => !v)} aria-expanded={optionsOpen} aria-label={labels.options} title={labels.options} className="lalt-assistant-icon-button">
                  <Icon name="plus" />
                </button>
                {optionsOpen && (
                  <div role="menu" className="lalt-assistant-menu-content lalt-assistant-options-menu">
                    {composerOptions.map((o) => (
                      <button
                        key={o.id}
                        type="button"
                        role="menuitemcheckbox"
                        aria-checked={o.checked}
                        onClick={() => {
                          o.onToggle(!o.checked);
                          setOptionsOpen(false);
                        }}
                      >
                        <span className="lalt-assistant-grow">
                          <span>{o.label}</span>
                          {o.hint && <span className="lalt-assistant-muted lalt-assistant-small lalt-assistant-block">{o.hint}</span>}
                        </span>
                        {o.checked && <Icon name="check" size={14} />}
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
            {renderComposerAccessory?.(composer)}
            {dictation.supported && (
              <button
                type="button"
                className="lalt-assistant-icon-button lalt-assistant-mic"
                data-live={recording || undefined}
                aria-label={recording ? labels.stopDictation : dictation.state === "transcribing" ? labels.transcribing : labels.dictate}
                title={recording ? labels.stopDictation : labels.dictate}
                aria-pressed={recording}
                disabled={dictation.state === "transcribing" || !!disabledReason}
                onClick={() => (recording ? dictation.stop() : void dictation.start())}
              >
                {dictation.state === "transcribing" ? <span className="lalt-assistant-spinner" /> : <Icon name="mic" />}
              </button>
            )}
            {attachments.enabled && (
              <>
                <button
                  type="button"
                  onClick={() => fileInput.current?.click()}
                  aria-label={labels.attach}
                  title={labels.attach}
                  className="lalt-assistant-icon-button"
                >
                  <Icon name="paperclip" />
                </button>
                <input
                  ref={fileInput}
                  type="file"
                  multiple
                  hidden
                  data-testid="lalt-assistant-file-input"
                  accept={attachmentLimits ? [...attachmentLimits.imageTypes, ...attachmentLimits.fileTypes].join(",") : undefined}
                  onChange={(e) => {
                    attachments.add(Array.from(e.target.files ?? []));
                    e.target.value = "";
                  }}
                />
              </>
            )}
            {reasoning && reasoning.options.length > 0 && reasoning.display !== "select" && (
              <ReasoningGauge
                options={reasoning.options}
                fallback={reasoning.default}
                value={effort}
                onChange={setEffort}
                labels={labels}
              />
            )}
            {reasoning && reasoning.options.length > 0 && reasoning.display === "select" && (
              <select
                value={effort ?? ""}
                onChange={(e) => setEffort((e.target.value || undefined) as ReasoningEffort | undefined)}
                aria-label={labels.reasoning}
                title={labels.reasoning}
                className="lalt-assistant-select lalt-assistant-reasoning-select"
              >
                <option value="">
                  {reasoning.default ? `${labels.reasoningDefault} · ${labels.reasoningLevels[reasoning.default]}` : labels.reasoningDefault}
                </option>
                {reasoning.options.map((level) => (
                  <option key={level} value={level}>
                    {labels.reasoningLevels[level]}
                  </option>
                ))}
              </select>
            )}
            <div className="lalt-assistant-grow" />
            {streaming ? (
              <button type="button" onClick={stop} aria-label={labels.stop} title={labels.stop} className="lalt-assistant-send">
                <Icon name="square" />
              </button>
            ) : (
            <button
              type="button"
              onClick={submit}
              disabled={!canSend || (!draft.trim() && attachments.done.length === 0)}
              aria-label={labels.send}
              className="lalt-assistant-send"
            >
              <Icon name="arrowUp" />
            </button>
            )}
          </div>
        </div>
      </div>
    </aside>
  );
}

function AttachmentChip({ item, labels, onRemove }: { item: PendingAttachment; labels: AssistantLabels; onRemove: () => void }) {
  return (
    <div className="lalt-assistant-chip" data-status={item.status} title={item.error}>
      {item.previewUrl ? <img src={item.previewUrl} alt="" className="lalt-assistant-chip-preview" /> : <Icon name="file" size={14} />}
      <span className="lalt-assistant-chip-text">
        <span className="lalt-assistant-truncate">{item.file.name}</span>
        <span className="lalt-assistant-muted lalt-assistant-small">
          {item.status === "error" ? item.error : item.status === "uploading" ? labels.uploading : formatSize(item.file.size)}
        </span>
      </span>
      {item.status === "uploading" && <span className="lalt-assistant-spinner" />}
      <button type="button" onClick={onRemove} aria-label={`${labels.removeAttachment} ${item.file.name}`} className="lalt-assistant-chip-remove">
        <Icon name="x" size={12} />
      </button>
    </div>
  );
}

function HistoryList({
  conversations,
  scopes,
  labels,
  onOpen,
}: {
  conversations: AssistantConversationSummary[];
  scopes: AssistantScope[];
  labels: AssistantLabels;
  onOpen: (c: AssistantConversationSummary) => void;
}) {
  if (conversations.length === 0) {
    return <p className="lalt-assistant-muted lalt-assistant-history-empty">{labels.noConversation}</p>;
  }
  return (
    <ul className="lalt-assistant-history">
      {conversations.map((c) => {
        const scope = scopes.find((s) => s.id === c.scopeId)?.label;
        return (
          <li key={c.id}>
            <button type="button" onClick={() => onOpen(c)}>
              <div className="lalt-assistant-truncate">{c.title}</div>
              <div className="lalt-assistant-muted lalt-assistant-small">
                {[scope, relativeTime(c.updatedAt)].filter(Boolean).join(" · ")}
              </div>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

function isSilent(message: AssistantMessage | undefined): boolean {
  return !!message?.streaming && !message.content && !message.tools?.length && !message.reasoning;
}

function ToolLines({ tools, live, labels }: { tools: AssistantToolCall[]; live: boolean; labels: AssistantLabels }) {
  const lines = (
    <ul className="lalt-assistant-tools">
      {tools.map((t) => (
        <li key={t.id} data-ok={t.ok === undefined ? undefined : String(t.ok)}>
          {t.endedAt === undefined ? <span className="lalt-assistant-spinner" /> : <Icon name={t.ok ? "check" : "x"} size={12} />}
          <span className="lalt-assistant-truncate">{t.label ?? t.name}</span>
          {t.summary && <span className="lalt-assistant-muted lalt-assistant-small lalt-assistant-truncate">{t.summary}</span>}
        </li>
      ))}
    </ul>
  );
  if (live) return lines;
  const start = Math.min(...tools.map((t) => t.startedAt));
  const end = Math.max(...tools.map((t) => t.endedAt ?? t.startedAt));
  return (
    <details className="lalt-assistant-tools-done">
      <summary className="lalt-assistant-muted lalt-assistant-small">{labels.toolsDone(tools.length, (end - start) / 1000)}</summary>
      {lines}
    </details>
  );
}

function SourceCards({ sources, labels }: { sources: AssistantSource[]; labels: AssistantLabels }) {
  return (
    <div className="lalt-assistant-sources" aria-label={labels.sources}>
      {sources.map((src) => {
        const href = /^https?:\/\//i.test(src.url) ? src.url : undefined;
        return (
          <a key={src.url} href={href} target="_blank" rel="noreferrer noopener" className="lalt-assistant-source">
            <span className="lalt-assistant-source-title lalt-assistant-truncate">{src.title}</span>
            {src.site && <span className="lalt-assistant-muted lalt-assistant-small">{src.site}</span>}
            {src.excerpt && <span className="lalt-assistant-muted lalt-assistant-small lalt-assistant-source-excerpt">{src.excerpt}</span>}
          </a>
        );
      })}
    </div>
  );
}

function AnswerAttachments({ attachments }: { attachments: AssistantAttachment[] }) {
  return (
    <div className="lalt-assistant-answer-files">
      {attachments.map((a) =>
        a.url && a.mimeType.startsWith("image/") ? (
          <figure key={a.id} className="lalt-assistant-answer-image">
            <img src={a.url} alt={a.sourceTitle ?? a.filename} />
            {a.sourceUrl && /^https?:\/\//i.test(a.sourceUrl) && (
              <figcaption className="lalt-assistant-muted lalt-assistant-small">
                <a href={a.sourceUrl} target="_blank" rel="noreferrer noopener">
                  {a.sourceTitle ?? a.sourceUrl}
                </a>
              </figcaption>
            )}
          </figure>
        ) : (
          <span key={a.id} className="lalt-assistant-sent-file">
            <Icon name="file" size={12} />
            {a.url && /^https?:\/\//i.test(a.url) ? (
              <a href={a.url} target="_blank" rel="noreferrer noopener">
                {a.filename}
              </a>
            ) : (
              a.filename
            )}
          </span>
        ),
      )}
    </div>
  );
}

const STREAM_ERROR_CODES = new Set<AssistantErrorCode>(["reply_in_progress", "connection_lost", "nothing_to_stream"]);

function streamErrorCode(err: unknown): AssistantErrorCode | undefined {
  const code = (err as { code?: unknown } | null)?.code;
  return typeof code === "string" && STREAM_ERROR_CODES.has(code as AssistantErrorCode) ? (code as AssistantErrorCode) : undefined;
}
