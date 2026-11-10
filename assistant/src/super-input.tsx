import {
  forwardRef,
  useCallback,
  useEffect,
  useId,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ClipboardEvent,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { pastedFiles, useAttachments, type PendingAttachment } from "./attachments";
import { MicBars, useDictation } from "./dictation";
import { Icon } from "./icons";
import { formatSize } from "./format";
import { defaultLabels } from "./labels";
import { ReasoningGauge } from "./reasoning-gauge";
import { lastLine, matchCommands, pushHistory, shellCommand, stepHistory, superInputMode } from "./super-input-mode";
import type { AssistantAttachment, AssistantLabels, AssistantTranscribe, AttachmentLimits, ReasoningEffort } from "./types";

export interface SuperInputCommand {
  name: string;
  description?: string;
  prefill?: boolean;
}

export interface SuperInputShellRun {
  id: string | number;
  command: string;
  output: string;
  running: boolean;
  code: number | null;
  killed?: boolean;
  truncated?: boolean;
  durationMs?: number;
  error?: string;
  handed?: boolean;
}

export interface SuperInputShell {
  runs: SuperInputShellRun[];
  run: (command: string) => void;
  stop: (id: SuperInputShellRun["id"]) => void;
  openInTerminal?: (run: SuperInputShellRun) => void;
  recent?: string[];
  cwd?: string;
}

export interface SuperInputReasoning {
  options: ReasoningEffort[];
  default?: ReasoningEffort;
  value: ReasoningEffort | undefined;
  onChange: (value: ReasoningEffort | undefined) => void;
  display?: "gauge" | "select";
}

export interface SuperInputModels {
  options: { id: string; label: string }[];
  value: string;
  onChange: (id: string) => void;
}

export interface SuperInputContextChip {
  id: string;
  label: string;
  title?: string;
  onRemove?: () => void;
}

export interface SuperInputHandle {
  focus: () => void;
  insert: (text: string) => void;
  setValue: (text: string) => void;
  addFiles: (files: File[]) => void;
}

export type SuperInputAttachments = ReturnType<typeof useAttachments>;

export interface SuperInputProps {
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
  onSend: (text: string, attachments: AssistantAttachment[]) => void | boolean | Promise<void | boolean>;
  canSend?: boolean;
  busy?: boolean;
  onStop?: () => void;
  disabled?: boolean;
  placeholder?: string;
  commands?: SuperInputCommand[];
  onCommand?: (command: SuperInputCommand) => void;
  shell?: SuperInputShell;
  shellElsewhere?: boolean;
  history?: string[];
  onHistoryChange?: (history: string[]) => void;
  reasoning?: SuperInputReasoning;
  models?: SuperInputModels;
  transcribe?: AssistantTranscribe;
  dictationLanguage?: string;
  dictation?: ReactNode;
  overlay?: ReactNode;
  attach?: (file: File) => Promise<AssistantAttachment>;
  attachments?: SuperInputAttachments;
  attachmentLimits?: AttachmentLimits;
  pasteAsFileThreshold?: number;
  onPaste?: (e: ClipboardEvent<HTMLTextAreaElement>) => boolean;
  context?: SuperInputContextChip[];
  chips?: ReactNode;
  accessory?: ReactNode;
  onEscape?: () => boolean;
  highlight?: boolean;
  labels?: Partial<AssistantLabels>;
  inputLabel?: string;
  className?: string;
}

const MAX_HEIGHT = 240;
const PASTED_TEXT_NAME = "texte-colle.txt";

function seconds(ms: number) {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toLocaleString("fr-FR", { maximumFractionDigits: 1 })} s`;
}

export const SuperInput = forwardRef<SuperInputHandle, SuperInputProps>(function SuperInput(
  {
    value,
    defaultValue = "",
    onChange,
    onSend,
    canSend = true,
    busy = false,
    onStop,
    disabled = false,
    placeholder,
    commands = [],
    onCommand,
    shell,
    shellElsewhere = false,
    history: historyProp,
    onHistoryChange,
    reasoning,
    models,
    transcribe,
    dictationLanguage,
    dictation: dictationSlot,
    overlay,
    attach,
    attachments: attachmentsProp,
    attachmentLimits,
    pasteAsFileThreshold,
    onPaste,
    context = [],
    chips,
    accessory,
    onEscape,
    highlight = false,
    labels: labelOverrides,
    inputLabel = "Message",
    className,
  },
  ref,
) {
  const labels: AssistantLabels = { ...defaultLabels, ...labelOverrides };
  const [inner, setInner] = useState(defaultValue);
  const text = value ?? inner;
  const textRef = useRef(text);
  textRef.current = text;
  const setText = useCallback(
    (next: string) => {
      textRef.current = next;
      if (value === undefined) setInner(next);
      onChange?.(next);
    },
    [value, onChange],
  );

  const area = useRef<HTMLTextAreaElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const [expanded, setExpanded] = useState(false);
  const [highlighted, setHighlighted] = useState(0);
  const [menuClosedFor, setMenuClosedFor] = useState<string | null>(null);
  const [history, setHistory] = useState<string[]>(historyProp ?? []);
  useEffect(() => {
    if (historyProp) setHistory(historyProp);
  }, [historyProp]);
  const [recalled, setRecalled] = useState<number | null>(null);
  const draft = useRef("");
  const [dismissed, setDismissed] = useState<Set<SuperInputShellRun["id"]>>(() => new Set());
  const [folded, setFolded] = useState<Record<string, "collapsed" | "expanded">>({});
  const menuId = useId();

  const insert = useCallback(
    (spoken: string) => {
      const prev = textRef.current;
      setText(prev.trim() ? `${prev.trimEnd()} ${spoken}` : spoken);
    },
    [setText],
  );


  const dictation = useDictation(
    transcribe,
    useCallback(
      (spoken: string) => {
        insert(spoken);
        area.current?.focus();
      },
      [insert],
    ),
    labels,
    dictationLanguage,
  );
  const recording = dictation.state === "recording";
  const own = useAttachments({ attach: attach && !attachmentsProp ? (file) => attach(file) : undefined, scopeId: "", limits: attachmentLimits, labels });
  const attachments = attachmentsProp ?? own;
  const addFiles = attachments.add;
  useImperativeHandle(ref, () => ({ focus: () => area.current?.focus(), insert, setValue: setText, addFiles }), [insert, setText, addFiles]);

  const mode = superInputMode(text, shell ? "shell" : shellElsewhere ? "elsewhere" : "text");
  const shellRunning = !!shell?.runs.some((r) => r.running);
  const visibleRuns = (shell?.runs ?? []).filter((r) => !r.handed && !dismissed.has(r.id));
  const dark = mode === "shell" || visibleRuns.some((r) => r.running);

  const recent = (shell?.recent ?? []).map((cmd) => ({ name: cmd }));
  const items: SuperInputCommand[] =
    menuClosedFor === text ? [] : mode === "slash" ? matchCommands(text, commands) : mode === "shell" && text.trim() === "!" ? recent : [];
  const menuOpen = items.length > 0;
  const active = Math.min(highlighted, Math.max(0, items.length - 1));
  const menuIsShell = mode === "shell";

  useLayoutEffect(() => {
    const el = area.current;
    if (!el) return;
    el.style.height = "auto";
    if (expanded) {
      el.style.height = "";
      el.style.overflowY = "auto";
      return;
    }
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
    el.style.overflowY = el.scrollHeight > MAX_HEIGHT ? "auto" : "hidden";
  }, [text, expanded]);

  const remember = (entry: string) => {
    const next = pushHistory(history, entry);
    setRecalled(null);
    if (next === history) return;
    setHistory(next);
    onHistoryChange?.(next);
  };

  const stopShell = () => shell?.runs.filter((r) => r.running).forEach((r) => shell.stop(r.id));

  const submit = () => {
    if (disabled) return;
    const trimmed = text.trim();
    if (mode === "noshell") return;
    if (mode === "shell" && shell) {
      const command = shellCommand(trimmed);
      if (!command) return;
      remember(trimmed);
      setText("");
      shell.run(command);
      return;
    }
    const ready = attachments.done;
    if (!canSend || attachments.uploading || (!trimmed && ready.length === 0)) return;
    remember(trimmed);
    setText("");
    attachments.clear();
    void Promise.resolve(onSend(trimmed, ready)).then((ok) => {
      if (ok === false && !textRef.current) setText(trimmed);
    });
  };

  const pick = (item: SuperInputCommand) => {
    setHighlighted(0);
    area.current?.focus();
    if (menuIsShell) {
      setText(`! ${item.name}`);
      return;
    }
    if (item.prefill || !onCommand) {
      setText(`/${item.name} `);
      return;
    }
    setText("");
    onCommand(item);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (menuOpen) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const step = e.key === "ArrowDown" ? 1 : -1;
        setHighlighted((active + step + items.length) % items.length);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        pick(items[active]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setMenuClosedFor(text);
        return;
      }
    }
    if (shellRunning && e.key === "c" && e.ctrlKey && e.currentTarget.selectionStart === e.currentTarget.selectionEnd) {
      e.preventDefault();
      stopShell();
      return;
    }
    if ((e.key === "ArrowUp" || e.key === "ArrowDown") && history.length > 0 && !text.includes("\n")) {
      if (e.key === "ArrowDown" && recalled === null) return;
      e.preventDefault();
      if (recalled === null) draft.current = text;
      const to = stepHistory(history.length, recalled ?? history.length, e.key === "ArrowUp" ? -1 : 1);
      setRecalled(to === history.length ? null : to);
      setText(to === history.length ? draft.current : history[to]);
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      submit();
      return;
    }
    if (e.key === "Escape") {
      if (expanded) {
        e.preventDefault();
        setExpanded(false);
        return;
      }
      if (onEscape?.() || dictation.cancel()) e.preventDefault();
    }
  };

  const handlePaste = (e: ClipboardEvent<HTMLTextAreaElement>) => {
    if (onPaste?.(e)) {
      e.preventDefault();
      return;
    }
    if (!attachments.enabled) return;
    const files = pastedFiles(e.clipboardData, pasteAsFileThreshold, PASTED_TEXT_NAME);
    if (files.length === 0) return;
    e.preventDefault();
    attachments.add(files);
  };

  const hint = shellRunning
    ? labels.hintRunning
    : {
        prompt: shell ? labels.hintPromptShell : labels.hintPrompt,
        slash: labels.hintSlash,
        command: labels.hintCommand,
        shell: labels.hintShell(shell?.cwd),
        noshell: labels.shellUnavailable,
      }[mode];

  const chip = { prompt: "", slash: labels.modeCommand, command: labels.modeCommand, shell: labels.modeShell, noshell: labels.modeNoShell }[mode];
  const hostOverlay = overlay ?? (recording ? <MicBars /> : dictation.error ? <span className="lalt-assistant-mic-error">{dictation.error}</span> : undefined);
  const hasContent = !!text.trim() || attachments.done.length > 0;

  let primary: { label: string; icon: string; onClick: () => void; disabled?: boolean; tone: string };
  if (shellRunning && (mode === "shell" || !text.trim())) primary = { label: labels.stop, icon: "square", onClick: stopShell, tone: "stop" };
  else if (mode === "shell") primary = { label: labels.run, icon: "play", onClick: submit, disabled: !shellCommand(text), tone: "run" };
  else if (busy && onStop) primary = { label: labels.stop, icon: "square", onClick: onStop, tone: "stop" };
  else
    primary = {
      label: labels.send,
      icon: "arrowUp",
      onClick: submit,
      disabled: disabled || !canSend || mode === "noshell" || attachments.uploading || !hasContent,
      tone: "send",
    };

  return (
    <div
      className={className ? `lalt-assistant lalt-si ${className}` : "lalt-assistant lalt-si"}
      data-mode={mode}
      data-dark={dark || undefined}
      data-expanded={expanded || undefined}
      data-highlight={highlight || undefined}
    >
      {(context.length > 0 || chips || attachments.items.length > 0 || attachments.notice) && (
        <div className="lalt-si-chips">
          {context.map((c) => (
            <span key={c.id} className="lalt-si-context" title={c.title}>
              <span className="lalt-assistant-truncate">{c.label}</span>
              {c.onRemove && (
                <button type="button" onClick={c.onRemove} aria-label={labels.removeOption(c.label)} className="lalt-si-chip-remove">
                  <Icon name="x" size={12} />
                </button>
              )}
            </span>
          ))}
          {chips}
          {attachments.items.map((item) => (
            <FileChip key={item.localId} item={item} labels={labels} onRemove={() => attachments.remove(item.localId)} />
          ))}
          {attachments.notice && <p className="lalt-assistant-error">{attachments.notice}</p>}
        </div>
      )}

      <div
        className="lalt-si-card"
        onMouseDown={(e) => {
          if ((e.target as HTMLElement).closest("button, input, textarea, select, pre, [role=listbox]")) return;
          e.preventDefault();
          area.current?.focus();
        }}
      >
        {menuOpen && (
          <div role="listbox" id={menuId} aria-label={menuIsShell ? labels.recentCommands : labels.commands} className="lalt-si-menu">
            {items.map((item, i) => (
              <button
                key={item.name}
                type="button"
                role="option"
                aria-selected={i === active}
                data-active={i === active || undefined}
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setHighlighted(i)}
                onClick={() => pick(item)}
                className="lalt-si-option"
              >
                <span className="lalt-si-option-name">{menuIsShell ? `! ${item.name}` : `/${item.name}`}</span>
                {item.description && <span className="lalt-si-option-desc">{item.description}</span>}
              </button>
            ))}
          </div>
        )}

        {visibleRuns.length > 0 && (
          <div className="lalt-si-runs">
            {visibleRuns.map((run, i) => (
              <ShellRunView
                key={run.id}
                run={run}
                labels={labels}
                fold={folded[String(run.id)] ?? (i === visibleRuns.length - 1 || run.running ? "auto" : "collapsed")}
                onFold={(fold) => setFolded((f) => ({ ...f, [String(run.id)]: fold }))}
                onStop={() => shell?.stop(run.id)}
                onDismiss={() => setDismissed((d) => new Set(d).add(run.id))}
                onOpenInTerminal={
                  shell?.openInTerminal
                    ? () => {
                        if (run.running) shell.stop(run.id);
                        shell.openInTerminal?.(run);
                        setDismissed((d) => new Set(d).add(run.id));
                      }
                    : undefined
                }
              />
            ))}
          </div>
        )}

        <div className="lalt-si-field">
          {chip && (
            <span className="lalt-si-mode" data-mode={mode}>
              {chip}
            </span>
          )}
          {hostOverlay && <div className="lalt-si-overlay">{hostOverlay}</div>}
          <textarea
            ref={area}
            disabled={disabled}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              setHighlighted(0);
              setMenuClosedFor(null);
            }}
            onKeyDown={onKeyDown}
            onPaste={handlePaste}
            rows={1}
            placeholder={hostOverlay ? "" : placeholder ?? (shell ? labels.placeholderShell : labels.placeholder)}
            aria-label={inputLabel}
            aria-controls={menuOpen ? menuId : undefined}
            aria-expanded={menuOpen}
            aria-autocomplete={commands.length > 0 || shell ? "list" : undefined}
            className="lalt-si-textarea"
          />
          <button
            type="button"
            onClick={() => {
              setExpanded((v) => !v);
              area.current?.focus();
            }}
            aria-label={expanded ? labels.collapseEditor : labels.expandEditor}
            title={expanded ? labels.collapseEditor : labels.expandEditor}
            aria-pressed={expanded}
            className="lalt-si-tool lalt-si-expand"
          >
            <Icon name={expanded ? "minimize" : "maximize"} size={14} />
          </button>
        </div>

        <div className="lalt-si-row">
          {attachments.enabled && (
            <>
              <button type="button" onClick={() => fileInput.current?.click()} aria-label={labels.attach} title={labels.attach} className="lalt-si-tool" disabled={disabled}>
                <Icon name="plus" />
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
          {accessory}
          <span className="lalt-assistant-grow" />
          {hint && (
            <span className="lalt-si-hint" aria-live="polite">
              {hint}
            </span>
          )}
          {models && models.options.length > 1 && (
            <Dropdown
              label={labels.model}
              value={models.value}
              options={models.options.map((m) => ({ value: m.id, label: m.label }))}
              onChange={(v) => v !== undefined && models.onChange(v)}
            />
          )}
          {reasoning && reasoning.options.length > 0 && reasoning.display === "gauge" && (
            <ReasoningGauge options={reasoning.options} fallback={reasoning.default} value={reasoning.value} onChange={reasoning.onChange} labels={labels} />
          )}
          {reasoning && reasoning.options.length > 0 && reasoning.display !== "gauge" && (
            <Dropdown
              label={labels.reasoning}
              value={reasoning.value}
              placeholder={reasoning.default ? `${labels.reasoningDefault} · ${labels.reasoningLevels[reasoning.default]}` : labels.reasoningDefault}
              options={[
                { value: undefined, label: reasoning.default ? `${labels.reasoningDefault} · ${labels.reasoningLevels[reasoning.default]}` : labels.reasoningDefault },
                ...reasoning.options.map((level) => ({ value: level, label: labels.reasoningLevels[level], hint: labels.reasoningHints[level] })),
              ]}
              onChange={(v) => reasoning.onChange(v as ReasoningEffort | undefined)}
            />
          )}
          {dictationSlot ??
            (dictation.supported && (
              <button
                type="button"
                className="lalt-si-tool lalt-assistant-mic"
                data-live={recording || undefined}
                aria-label={recording ? labels.stopDictation : dictation.state === "transcribing" ? labels.transcribing : labels.dictate}
                title={recording ? labels.stopDictation : labels.dictate}
                aria-pressed={recording}
                disabled={dictation.state === "transcribing" || disabled}
                onClick={() => (recording ? dictation.stop() : void dictation.start())}
              >
                {dictation.state === "transcribing" ? <span className="lalt-assistant-spinner" /> : <Icon name="mic" />}
              </button>
            ))}
          <button
            type="button"
            onClick={primary.onClick}
            disabled={primary.disabled}
            aria-label={primary.label}
            title={primary.label}
            className="lalt-si-send"
            data-tone={primary.tone}
          >
            <Icon name={primary.icon} />
          </button>
        </div>
      </div>
    </div>
  );
});

function Dropdown({
  label,
  value,
  options,
  placeholder,
  onChange,
}: {
  label: string;
  value: string | undefined;
  options: { value: string | undefined; label: string; hint?: string }[];
  placeholder?: string;
  onChange: (value: string | undefined) => void;
}) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const current = options.find((o) => o.value === value)?.label ?? placeholder ?? options[0]?.label ?? "";
  const choose = (v: string | undefined) => {
    onChange(v);
    setOpen(false);
  };
  return (
    <div
      className="lalt-si-dropdown"
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false);
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape" && open) {
          e.stopPropagation();
          setOpen(false);
        }
      }}
    >
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={`${label} : ${current}`}
        title={label}
        onClick={() => setOpen((v) => !v)}
        className="lalt-si-pill"
      >
        <span>{current}</span>
        <Icon name="chevronDown" size={12} />
      </button>
      {open && (
        <div role="listbox" id={id} aria-label={label} className="lalt-si-dropdown-menu">
          {options.map((o) => (
            <button
              key={o.value ?? ""}
              type="button"
              role="option"
              aria-selected={o.value === value}
              onClick={() => choose(o.value)}
              className="lalt-si-option"
            >
              <span>{o.label}</span>
              {o.hint && <span className="lalt-si-option-desc">{o.hint}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function ShellRunView({
  run,
  labels,
  fold,
  onFold,
  onStop,
  onDismiss,
  onOpenInTerminal,
}: {
  run: SuperInputShellRun;
  labels: AssistantLabels;
  fold: "auto" | "collapsed" | "expanded";
  onFold: (fold: "collapsed" | "expanded") => void;
  onStop: () => void;
  onDismiss: () => void;
  onOpenInTerminal?: () => void;
}) {
  const out = useRef<HTMLPreElement>(null);
  useLayoutEffect(() => {
    const el = out.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [run.output, fold]);
  const failed = run.killed || (run.code !== null && run.code !== 0) || !!run.error;
  const status = run.running
    ? labels.shellRunning
    : run.error
      ? run.error
      : run.killed
        ? labels.shellKilled
        : labels.shellExit(run.code, run.durationMs !== undefined ? seconds(run.durationMs) : undefined);
  const collapsed = fold === "collapsed";
  return (
    <section className="lalt-si-run" data-fold={fold} data-running={run.running || undefined} aria-label={`$ ${run.command}`}>
      <div className="lalt-si-run-head">
        <span className="lalt-si-run-prompt" aria-hidden="true">
          $
        </span>
        <span className="lalt-si-run-cmd lalt-assistant-truncate">{run.command}</span>
        <span className="lalt-si-run-status" data-failed={(!run.running && failed) || undefined} data-ok={(!run.running && !failed) || undefined}>
          {status}
          {run.truncated && ` · ${labels.shellTruncated}`}
        </span>
        <button
          type="button"
          className="lalt-si-run-btn"
          onClick={() => onFold(collapsed ? "expanded" : "collapsed")}
          aria-label={collapsed ? labels.shellExpand : labels.shellCollapse}
          title={collapsed ? labels.shellExpand : labels.shellCollapse}
          aria-expanded={!collapsed}
        >
          <Icon name={collapsed ? "chevronDown" : "chevronUp"} size={12} />
        </button>
        {onOpenInTerminal && (
          <button type="button" className="lalt-si-run-btn" onClick={onOpenInTerminal} aria-label={labels.shellOpenTerminal} title={labels.shellOpenTerminal}>
            <Icon name="terminal" size={12} />
          </button>
        )}
        {run.running ? (
          <button type="button" className="lalt-si-run-btn" data-tone="stop" onClick={onStop} aria-label={labels.stop} title={labels.stop}>
            <Icon name="square" size={12} />
          </button>
        ) : (
          <button type="button" className="lalt-si-run-btn" onClick={onDismiss} aria-label={labels.shellDismiss} title={labels.shellDismiss}>
            <Icon name="x" size={12} />
          </button>
        )}
      </div>
      {run.output && (
        <pre ref={out} className="lalt-si-run-out" aria-live={run.running ? "polite" : undefined}>
          {collapsed ? lastLine(run.output) : run.output}
        </pre>
      )}
      {!run.running && !run.error && <span className="lalt-si-run-note">{labels.shellPending}</span>}
    </section>
  );
}

function FileChip({ item, labels, onRemove }: { item: PendingAttachment; labels: AssistantLabels; onRemove: () => void }) {
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
