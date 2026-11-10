import { useMemo, useRef, useState } from "react";

import { assemble, segments, surroundings, wordParts } from "./diff";
import type { Decision, Granularity, Segment } from "./diff";
import { defaultLabels } from "./labels";
import type { ReviewLabels } from "./labels";

export interface ReviewField {
  id: string;
  label?: string;
  before: string;
  after: string;
}

export interface ReworkRequest {
  fieldId: string;
  changeId: string;
  /** The text the change replaces, as it stands in the original. */
  original: string;
  /** What is proposed now: the first proposal, or the last rework of it. */
  proposal: string;
  instruction: string;
  context: { before: string; after: string };
}

export interface ReviewChangesProps {
  fields: ReviewField[];
  /** Reworks one passage; resolves with its new proposal. Absent: no rework button. */
  onRework?: (request: ReworkRequest) => Promise<string>;
  /** Receives each field's final text, decided passage by passage. */
  onApply: (texts: Record<string, string>) => void | Promise<void>;
  onDiscard?: () => void;
  applying?: boolean;
  labels?: Partial<ReviewLabels>;
  className?: string;
  /** Side by side, before on the left and after on the right; or one column. */
  layout?: "split" | "inline";
  /** How finely passages are cut; by line in the split view unless told otherwise. */
  granularity?: Granularity;
}

type Key = string;

const keyOf = (fieldId: string, changeId: string): Key => `${fieldId}\u0000${changeId}`;

export function ReviewChanges({
  fields,
  onRework,
  onApply,
  onDiscard,
  applying,
  labels: overrides,
  className,
  layout = "split",
  granularity,
}: ReviewChangesProps) {
  const cut = granularity ?? (layout === "split" ? "line" : "word");
  const labels = { ...defaultLabels, ...overrides };
  const parsed = useMemo(
    () =>
      fields
        .filter((f) => f.before !== f.after)
        .map((f) => ({ field: f, segs: segments(f.before, f.after, "c", cut) })),
    [fields, cut],
  );
  const [decisions, setDecisions] = useState<Record<Key, Decision>>({});
  const [proposals, setProposals] = useState<Record<Key, string>>({});
  const [reworked, setReworked] = useState<Record<Key, boolean>>({});
  const refs = useRef<Record<Key, HTMLDivElement | null>>({});

  const changes = parsed.flatMap(({ field, segs }) =>
    segs.flatMap((s) => (s.kind === "change" ? [{ field, change: s, key: keyOf(field.id, s.id) }] : [])),
  );
  const statusOf = (k: Key) => decisions[k]?.status ?? "pending";
  const count = (status: Decision["status"]) => changes.filter((c) => statusOf(c.key) === status).length;
  const busy = changes.some((c) => statusOf(c.key) === "reworking");

  const decide = (k: Key, d: Decision) => setDecisions((all) => ({ ...all, [k]: d }));
  const decideAll = (status: "accepted" | "rejected") =>
    setDecisions((all) => {
      const next = { ...all };
      for (const c of changes) if (statusOf(c.key) !== "reworking") next[c.key] = { status };
      return next;
    });

  const rework = async (fieldId: string, segs: Segment[], change: Extract<Segment, { kind: "change" }>, instruction: string) => {
    const k = keyOf(fieldId, change.id);
    if (!onRework) return;
    decide(k, { status: "reworking", instruction });
    try {
      const text = await onRework({
        fieldId,
        changeId: change.id,
        original: change.before,
        proposal: proposals[k] ?? change.after,
        instruction,
        context: surroundings(segs, change.id),
      });
      setProposals((all) => ({ ...all, [k]: text }));
      setReworked((all) => ({ ...all, [k]: true }));
      decide(k, { status: "pending" });
    } catch (e) {
      decide(k, { status: "failed", error: e instanceof Error && e.message ? e.message : labels.reworkFailed });
    }
  };

  const nextPending = () => {
    const next = changes.find((c) => statusOf(c.key) === "pending" || statusOf(c.key) === "failed");
    refs.current[next?.key ?? ""]?.scrollIntoView({ block: "center", behavior: "smooth" });
    refs.current[next?.key ?? ""]?.focus();
  };

  const apply = () => {
    const texts: Record<string, string> = {};
    for (const { field, segs } of parsed) {
      const fieldDecisions: Record<string, Decision> = {};
      const fieldProposals: Record<string, string> = {};
      for (const s of segs) {
        if (s.kind !== "change") continue;
        const k = keyOf(field.id, s.id);
        if (decisions[k]) fieldDecisions[s.id] = decisions[k];
        if (proposals[k] !== undefined) fieldProposals[s.id] = proposals[k];
      }
      texts[field.id] = assemble(segs, fieldDecisions, fieldProposals);
    }
    return onApply(texts);
  };

  const pending = count("pending") + count("failed");
  return (
    <div className={["lalt-review", `lalt-review-${layout}`, className].filter(Boolean).join(" ")}>
      <div className="lalt-review-toolbar">
        <span className="lalt-review-counts">
          {changes.length === 0
            ? labels.noChange
            : [labels.pending(pending), labels.accepted(count("accepted")), labels.rejected(count("rejected"))].join(" · ")}
        </span>
        {changes.length > 0 ? (
          <span className="lalt-review-toolbar-actions">
            <button type="button" className="lalt-review-btn" disabled={pending === 0} onClick={nextPending}>
              {labels.nextPending}
            </button>
            <button type="button" className="lalt-review-btn" onClick={() => decideAll("rejected")}>
              {labels.rejectAll}
            </button>
            <button type="button" className="lalt-review-btn" onClick={() => decideAll("accepted")}>
              {labels.acceptAll}
            </button>
          </span>
        ) : null}
      </div>

      <div className="lalt-review-body">
        {parsed.map(({ field, segs }) => (
          <section key={field.id} className="lalt-review-field">
            {field.label ? <h4 className="lalt-review-label">{field.label}</h4> : null}
            {layout === "split" ? (
              <div className="lalt-review-columns" aria-hidden>
                <span>{labels.before}</span>
                <span>{labels.after}</span>
              </div>
            ) : null}
            <div className="lalt-review-text">
              {segs.map((s, i) =>
                s.kind === "same" ? (
                  layout === "split" ? (
                    <div key={i} className="lalt-review-row lalt-review-same">
                      <div>{s.text}</div>
                      <div>{s.text}</div>
                    </div>
                  ) : (
                    <span key={i}>{s.text}</span>
                  )
                ) : (
                  <Change
                    key={s.id}
                    layout={layout}
                    refCb={(el) => {
                      refs.current[keyOf(field.id, s.id)] = el;
                    }}
                    change={s}
                    decision={decisions[keyOf(field.id, s.id)] ?? { status: "pending" }}
                    proposal={proposals[keyOf(field.id, s.id)] ?? s.after}
                    reworked={!!reworked[keyOf(field.id, s.id)]}
                    canRework={!!onRework}
                    labels={labels}
                    onDecide={(d) => decide(keyOf(field.id, s.id), d)}
                    onRework={(instruction) => void rework(field.id, segs, s, instruction)}
                  />
                ),
              )}
            </div>
          </section>
        ))}
      </div>

      <div className="lalt-review-footer">
        {pending > 0 ? <span className="lalt-review-hint">{labels.keptOriginal}</span> : <span />}
        <span className="lalt-review-toolbar-actions">
          {onDiscard ? (
            <button type="button" className="lalt-review-btn" disabled={applying} onClick={onDiscard}>
              {labels.discard}
            </button>
          ) : null}
          <button
            type="button"
            className="lalt-review-btn lalt-review-btn-primary"
            disabled={applying || busy}
            onClick={() => void apply()}
          >
            {labels.apply}
          </button>
        </span>
      </div>
    </div>
  );
}

function Change({
  layout,
  change,
  decision,
  proposal,
  reworked,
  canRework,
  labels,
  onDecide,
  onRework,
  refCb,
}: {
  layout: "split" | "inline";
  change: Extract<Segment, { kind: "change" }>;
  decision: Decision;
  proposal: string;
  reworked: boolean;
  canRework: boolean;
  labels: ReviewLabels;
  onDecide: (d: Decision) => void;
  onRework: (instruction: string) => void;
  refCb: (el: HTMLDivElement | null) => void;
}) {
  const [asking, setAsking] = useState(false);
  const [instruction, setInstruction] = useState("");

  if (decision.status === "accepted" || decision.status === "rejected") {
    const kept = decision.status === "accepted" ? proposal : change.before;
    if (layout === "split") {
      return (
        <div className={`lalt-review-row lalt-review-decided lalt-review-decided-${decision.status}`}>
          <div className={decision.status === "accepted" ? "lalt-review-dropped" : undefined}>{change.before}</div>
          <div>
            {decision.status === "accepted" ? proposal : <span className="lalt-review-dropped">{proposal}</span>}
            <button
              type="button"
              className="lalt-review-undo"
              title={labels.undo}
              aria-label={labels.undo}
              onClick={() => onDecide({ status: "pending" })}
            >
              ↺ {decision.status === "accepted" ? labels.accept : labels.reject}
            </button>
          </div>
        </div>
      );
    }
    return (
      <span className={`lalt-review-decided lalt-review-decided-${decision.status}`}>
        {kept}
        <button
          type="button"
          className="lalt-review-undo"
          title={labels.undo}
          aria-label={labels.undo}
          onClick={() => onDecide({ status: "pending" })}
        >
          ↺
        </button>
      </span>
    );
  }

  const reworking = decision.status === "reworking";
  const send = () => {
    const text = instruction.trim();
    if (!text) return;
    setAsking(false);
    setInstruction("");
    onRework(text);
  };

  return (
    <div ref={refCb} tabIndex={-1} className="lalt-review-change" data-status={decision.status}>
      {layout === "split" ? (
        <SideBySide before={change.before} after={proposal} badge={reworked && !reworking ? labels.reworked : null} />
      ) : (
        <div className="lalt-review-diff">
          {change.before ? <del className="lalt-review-removed">{change.before}</del> : null}
          {proposal ? <ins className="lalt-review-added">{proposal}</ins> : null}
          {reworked && !reworking ? <span className="lalt-review-badge">{labels.reworked}</span> : null}
        </div>
      )}
      {reworking ? (
        <div className="lalt-review-status">
          <span className="lalt-review-spinner" aria-hidden />
          {labels.reworking} « {decision.instruction} »
        </div>
      ) : null}
      {decision.status === "failed" ? <div className="lalt-review-error">{decision.error}</div> : null}
      {asking ? (
        <div className="lalt-review-ask">
          <textarea
            autoFocus
            rows={2}
            value={instruction}
            placeholder={labels.instructionPlaceholder}
            onChange={(e) => setInstruction(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
              if (e.key === "Escape") setAsking(false);
            }}
          />
          <span className="lalt-review-toolbar-actions">
            <button type="button" className="lalt-review-btn" onClick={() => setAsking(false)}>
              {labels.cancel}
            </button>
            <button type="button" className="lalt-review-btn lalt-review-btn-primary" disabled={!instruction.trim()} onClick={send}>
              {labels.send}
            </button>
          </span>
        </div>
      ) : (
        <div className="lalt-review-actions">
          <button type="button" className="lalt-review-btn" disabled={reworking} onClick={() => onDecide({ status: "rejected" })}>
            {labels.reject}
          </button>
          {canRework ? (
            <button type="button" className="lalt-review-btn" disabled={reworking} onClick={() => setAsking(true)}>
              {labels.rework}
            </button>
          ) : null}
          <button
            type="button"
            className="lalt-review-btn lalt-review-btn-primary"
            disabled={reworking}
            onClick={() => onDecide({ status: "accepted" })}
          >
            {labels.accept}
          </button>
        </div>
      )}
    </div>
  );
}

function SideBySide({ before, after, badge }: { before: string; after: string; badge: string | null }) {
  const parts = useMemo(() => wordParts(before, after), [before, after]);
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
  return (
    <div className="lalt-review-row lalt-review-pair">
      <div className={before ? "lalt-review-side-removed" : "lalt-review-side-empty"}>{before ? side("added", "removed") : null}</div>
      <div className={after ? "lalt-review-side-added" : "lalt-review-side-empty"}>
        {after ? side("removed", "added") : null}
        {badge ? <span className="lalt-review-badge">{badge}</span> : null}
      </div>
    </div>
  );
}
