import { useMemo, useState } from "react";

import { codeRows } from "./code";
import type { CodeNote, CodeRow } from "./code";
import { wordParts } from "./diff";

export interface ReviewCodeLabels {
  comment: string;
  send: string;
  cancel: string;
  sent: string;
  commentPlaceholder: string;
  unchanged: (n: number) => string;
  noChange: string;
}

export const defaultCodeLabels: ReviewCodeLabels = {
  comment: "Commenter",
  send: "Envoyer",
  cancel: "Annuler",
  sent: "Envoyé",
  commentPlaceholder: "Ce qui doit changer ici…",
  unchanged: (n) => `${n} ligne${n > 1 ? "s" : ""} inchangée${n > 1 ? "s" : ""}`,
  noChange: "Aucun changement.",
};

export interface CodeComment {
  changeId: string;
  before: string;
  after: string;
  /** First line of the change in the new version, or where it was removed. */
  line: number;
  text: string;
}

export interface ReviewCodeProps {
  before: string;
  after: string;
  /** Unchanged lines kept around each change; longer runs fold. */
  context?: number;
  notes?: CodeNote[];
  /** Sends a comment on one change. Absent: no comment button. */
  onComment?: (comment: CodeComment) => Promise<void>;
  labels?: Partial<ReviewCodeLabels>;
  className?: string;
}

/** A file's change read as code: line numbers, folded context, notes, a comment per change. */
export function ReviewCode({ before, after, context = 3, notes, onComment, labels: overrides, className }: ReviewCodeProps) {
  const labels = { ...defaultCodeLabels, ...overrides };
  const rows = useMemo(() => codeRows(before, after, context, notes), [before, after, context, notes]);
  const [open, setOpen] = useState<Record<string, boolean>>({});

  if (before === after) {
    return <p className={["lalt-review-code", "lalt-review-hint", className].filter(Boolean).join(" ")}>{labels.noChange}</p>;
  }
  return (
    <div className={["lalt-review", "lalt-review-code", className].filter(Boolean).join(" ")}>
      <div className="lalt-review-code-lines">
        {rows.map((r) => {
          if (r.kind === "fold" && open[r.key]) {
            return r.rows.map((s) => <SameLine key={s.key} row={s} />);
          }
          return (
            <Row
              key={r.key}
              row={r}
              labels={labels}
              onUnfold={() => setOpen((all) => ({ ...all, [r.key]: true }))}
              onComment={onComment}
            />
          );
        })}
      </div>
    </div>
  );
}

function SameLine({ row }: { row: Extract<CodeRow, { kind: "same" }> }) {
  return (
    <div className="lalt-review-code-line">
      <span className="lalt-review-code-num">{row.old}</span>
      <span className="lalt-review-code-num">{row.new}</span>
      <span className="lalt-review-code-sign" />
      <span className="lalt-review-code-text">{row.text}</span>
    </div>
  );
}

function numbers(from: number, count: number): string {
  return Array.from({ length: count }, (_, i) => String(from + i)).join("\n");
}

function Row({
  row: r,
  labels,
  onUnfold,
  onComment,
}: {
  row: CodeRow;
  labels: ReviewCodeLabels;
  onUnfold: () => void;
  onComment?: (comment: CodeComment) => Promise<void>;
}) {
  switch (r.kind) {
    case "same":
      return <SameLine row={r} />;
    case "fold":
      return (
        <button type="button" className="lalt-review-code-fold" onClick={onUnfold}>
          ⋯ {labels.unchanged(r.rows.length)}
        </button>
      );
    case "note":
      return (
        <div className="lalt-review-code-note">
          {r.note.author ? <strong>{r.note.author} : </strong> : null}
          {r.note.text}
        </div>
      );
    case "change":
      return <ChangeBlock change={r} labels={labels} onComment={onComment} />;
  }
}

function ChangeBlock({
  change: c,
  labels,
  onComment,
}: {
  change: Extract<CodeRow, { kind: "change" }>;
  labels: ReviewCodeLabels;
  onComment?: (comment: CodeComment) => Promise<void>;
}) {
  const parts = useMemo(() => wordParts(c.before, c.after), [c.before, c.after]);
  const [asking, setAsking] = useState(false);
  const [text, setText] = useState("");
  const [sent, setSent] = useState<string[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const side = (hide: "added" | "removed", mark: "removed" | "added") =>
    parts
      .filter((p) => p.kind !== hide)
      .map((p, i) =>
        p.kind === mark ? (
          <mark key={i} className={`lalt-review-word-${mark}`}>
            {p.text}
          </mark>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      );
  const send = async () => {
    const said = text.trim();
    if (!said || !onComment) return;
    setSending(true);
    setError(null);
    try {
      await onComment({ changeId: c.id, before: c.before, after: c.after, line: c.newFrom, text: said });
      setSent((all) => [...all, said]);
      setText("");
      setAsking(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="lalt-review-code-change">
      {c.oldCount > 0 ? (
        <div className="lalt-review-code-line lalt-review-code-removed">
          <span className="lalt-review-code-num">{numbers(c.oldFrom, c.oldCount)}</span>
          <span className="lalt-review-code-num" />
          <span className="lalt-review-code-sign">−</span>
          <span className="lalt-review-code-text">{side("added", "removed")}</span>
        </div>
      ) : null}
      {c.newCount > 0 ? (
        <div className="lalt-review-code-line lalt-review-code-added">
          <span className="lalt-review-code-num" />
          <span className="lalt-review-code-num">{numbers(c.newFrom, c.newCount)}</span>
          <span className="lalt-review-code-sign">+</span>
          <span className="lalt-review-code-text">{side("removed", "added")}</span>
        </div>
      ) : null}
      {sent.map((s, i) => (
        <div key={i} className="lalt-review-code-note lalt-review-code-sent">
          <strong>{labels.sent} : </strong>
          {s}
        </div>
      ))}
      {onComment ? (
        asking ? (
          <div className="lalt-review-ask">
            <textarea
              autoFocus
              rows={2}
              value={text}
              aria-label={labels.comment}
              placeholder={labels.commentPlaceholder}
              onChange={(e) => setText(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  void send();
                }
                if (e.key === "Escape") setAsking(false);
              }}
            />
            {error ? <div className="lalt-review-error">{error}</div> : null}
            <span className="lalt-review-toolbar-actions">
              <button type="button" className="lalt-review-btn" onClick={() => setAsking(false)}>
                {labels.cancel}
              </button>
              <button
                type="button"
                className="lalt-review-btn lalt-review-btn-primary"
                disabled={sending || !text.trim()}
                onClick={() => void send()}
              >
                {labels.send}
              </button>
            </span>
          </div>
        ) : (
          <div className="lalt-review-code-actions">
            <button type="button" className="lalt-review-btn" onClick={() => setAsking(true)}>
              {labels.comment}
            </button>
          </div>
        )
      ) : null}
    </div>
  );
}
