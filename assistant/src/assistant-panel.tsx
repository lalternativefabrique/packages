import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type DragEvent,
  type KeyboardEvent,
  type PointerEvent as ReactPointerEvent,
} from "react";
import { useAttachments, pastedFiles, type PendingAttachment } from "./attachments";
import { errorMessage, formatSize, relativeTime } from "./format";
import { Icon } from "./icons";
import { defaultLabels } from "./labels";
import type {
  AssistantConversationSummary,
  AssistantLabels,
  AssistantMessage,
  AssistantPanelProps,
  AssistantScope,
} from "./types";

const MIN_WIDTH = 360;
const DEFAULT_WIDTH = 520;
const PASTED_TEXT_NAME = "texte-colle.txt";

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
  renderMessageFooter,
  readScreen,
  widthStorageKey = "lalt.assistant.width",
  onClose,
  initialHistory = false,
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
  const fileInput = useRef<HTMLInputElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const generation = useRef(0);

  const attachments = useAttachments({ attach: api.attach?.bind(api), scopeId, limits: attachmentLimits, labels });

  const refreshConversations = useCallback(() => {
    api.listConversations().then(setConversations, (err: unknown) => setError(errorMessage(err, labels.requestFailed)));
  }, [api, labels.requestFailed]);

  useEffect(refreshConversations, [refreshConversations]);

  useEffect(() => {
    endRef.current?.scrollIntoView?.({ block: "end" });
  }, [thread.length, pending]);

  const newConversation = () => {
    generation.current++;
    setThread([]);
    setConversationId(undefined);
    setError(null);
    setHistoryOpen(false);
    setMenuOpen(false);
    setPending(false);
    attachments.clear();
  };

  const openConversation = (c: AssistantConversationSummary) => {
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

  const canSend = !!scopeId && !pending && !attachments.uploading;

  const send = (message: string, shownAs: string) => {
    const ready = attachments.done;
    if (!canSend || (!message.trim() && ready.length === 0)) return false;
    const current = generation.current;
    setError(null);
    setHistoryOpen(false);
    setPending(true);
    setThread((t) => [...t, { role: "user", content: shownAs, attachments: ready }]);
    attachments.clear();
    api
      .send({
        scopeId,
        conversationId,
        message,
        attachmentIds: ready.map((a) => a.id),
        screen: readScreen?.(),
      })
      .then(
        (res) => {
          if (current !== generation.current) return;
          setConversationId(res.conversationId);
          setThread((t) => [...t, { role: "assistant", content: res.answer, meta: res.meta }]);
          setPending(false);
          refreshConversations();
        },
        (err: unknown) => {
          if (current !== generation.current) return;
          setError(errorMessage(err, labels.requestFailed));
          setPending(false);
        },
      );
    return true;
  };

  const submit = () => {
    const text = draft.trim();
    if (send(text, text)) setDraft("");
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
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

  const empty = thread.length === 0 && !pending && !loading;
  const panel = usePanelWidth(widthStorageKey);

  return (
    <aside
      role="dialog"
      aria-label={labels.launcher}
      className="lalt-assistant lalt-assistant-panel"
      data-expanded={panel.expanded || undefined}
      style={{ ["--lalt-assistant-width" as string]: panel.expanded ? "min(1100px, 92vw)" : `${panel.width}px` }}
      onDragOver={onDragOver}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setDragging(false);
      }}
      onDrop={onDrop}
    >
      <div
        onPointerDown={panel.startResize}
        role="separator"
        aria-orientation="vertical"
        aria-label={labels.resize}
        className="lalt-assistant-resize"
      />
      {dragging && <div className="lalt-assistant-dropzone">{labels.dropHere}</div>}
      <header className="lalt-assistant-header">
        <div className="lalt-assistant-menu">
          <button type="button" className="lalt-assistant-title" onClick={() => setMenuOpen((v) => !v)} aria-expanded={menuOpen}>
            <Icon name="bot" />
            {labels.launcher}
            <Icon name="chevronDown" size={14} className="lalt-assistant-muted" />
          </button>
          {menuOpen && (
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
        <button
          type="button"
          onClick={() => panel.setExpanded((v) => !v)}
          aria-label={panel.expanded ? labels.collapse : labels.expand}
          className="lalt-assistant-icon-button lalt-assistant-wide-only"
        >
          <Icon name={panel.expanded ? "minimize" : "maximize"} />
        </button>
        <button type="button" onClick={onClose} aria-label={labels.close} className="lalt-assistant-icon-button">
          <Icon name="x" />
        </button>
      </header>

      <div className="lalt-assistant-body">
        {historyOpen ? (
          <HistoryList conversations={conversations} scopes={scopes} labels={labels} onOpen={openConversation} />
        ) : empty ? (
          <div>
            <h2 className="lalt-assistant-empty-title">{labels.emptyTitle}</h2>
            {labels.intro && <p className="lalt-assistant-intro">{labels.intro}</p>}
            <div className="lalt-assistant-actions">
              {quickActions.map((a) => (
                <button
                  key={a.id}
                  type="button"
                  onClick={() => send(a.prompt, a.label)}
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
                <div key={i} className="lalt-assistant-answer">
                  {renderMarkdown ? renderMarkdown(m.content) : <div className="lalt-assistant-plain">{m.content}</div>}
                  {renderMessageFooter?.(m)}
                </div>
              ),
            )}
            {(pending || loading) && (
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
          </p>
        )}
        <div ref={endRef} />
      </div>

      <div className="lalt-assistant-footer">
        <div className="lalt-assistant-composer">
          {(attachments.items.length > 0 || attachments.notice) && (
            <div className="lalt-assistant-chips">
              {attachments.items.map((item) => (
                <AttachmentChip key={item.localId} item={item} labels={labels} onRemove={() => attachments.remove(item.localId)} />
              ))}
              {attachments.notice && <p className="lalt-assistant-error">{attachments.notice}</p>}
            </div>
          )}
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            rows={3}
            placeholder={labels.placeholder}
            aria-label="Message"
            className="lalt-assistant-textarea"
          />
          <div className="lalt-assistant-composer-row">
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
            <div className="lalt-assistant-grow" />
            <button
              type="button"
              onClick={submit}
              disabled={!canSend || (!draft.trim() && attachments.done.length === 0)}
              aria-label={labels.send}
              className="lalt-assistant-send"
            >
              <Icon name="arrowUp" />
            </button>
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
