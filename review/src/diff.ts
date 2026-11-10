export type Segment =
  | { kind: "same"; text: string }
  | { kind: "change"; id: string; before: string; after: string };

type Part = { kind: "same" | "removed" | "added"; text: string };

const MAX_CELLS = 4_000_000;

function tokens(text: string): string[] {
  return text.match(/\s+|[^\s]+/g) ?? [];
}

function diffTokens(a: string[], b: string[]): Part[] {
  if (a.length * b.length > MAX_CELLS) {
    return [
      { kind: "removed", text: a.join("") },
      { kind: "added", text: b.join("") },
    ];
  }
  const width = b.length + 1;
  const lcs = new Uint32Array((a.length + 1) * width);
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      lcs[i * width + j] =
        a[i] === b[j]
          ? lcs[(i + 1) * width + j + 1] + 1
          : Math.max(lcs[(i + 1) * width + j], lcs[i * width + j + 1]);
    }
  }
  const parts: Part[] = [];
  const push = (kind: Part["kind"], text: string) => {
    const last = parts[parts.length - 1];
    if (last?.kind === kind) last.text += text;
    else parts.push({ kind, text });
  };
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      push("same", a[i]);
      i++;
      j++;
    } else if (lcs[(i + 1) * width + j] >= lcs[i * width + j + 1]) {
      push("removed", a[i++]);
    } else {
      push("added", b[j++]);
    }
  }
  while (i < a.length) push("removed", a[i++]);
  while (j < b.length) push("added", b[j++]);
  return parts;
}

/** A shared run of this many words or fewer between two changes reads as one edit, not two. */
const GLUE_WORDS = 1;

function words(text: string): number {
  return text.match(/[^\s]+/g)?.length ?? 0;
}

/**
 * Splits a rewrite into passages the author decides one by one. Unchanged text
 * stays as is; each change carries what it replaces and what it proposes. A
 * change never splits inside a word, and two changes separated by a word or
 * less of shared text are one passage.
 */
export function segments(before: string, after: string, idPrefix = "c"): Segment[] {
  const parts = diffTokens(tokens(before), tokens(after));
  const out: Segment[] = [];
  let open: { before: string; after: string } | null = null;
  let n = 0;
  const close = () => {
    if (open) out.push({ kind: "change", id: `${idPrefix}${n++}`, ...open });
    open = null;
  };
  parts.forEach((p, idx) => {
    if (p.kind === "same") {
      const between = open !== null && idx < parts.length - 1;
      if (between && words(p.text) <= GLUE_WORDS) {
        open!.before += p.text;
        open!.after += p.text;
        return;
      }
      close();
      const last = out[out.length - 1];
      if (last?.kind === "same") last.text += p.text;
      else out.push({ kind: "same", text: p.text });
      return;
    }
    open ??= { before: "", after: "" };
    if (p.kind === "removed") open.before += p.text;
    else open.after += p.text;
  });
  close();
  return out;
}

export type Decision =
  | { status: "pending" }
  | { status: "accepted" }
  | { status: "rejected" }
  | { status: "reworking"; instruction: string }
  | { status: "failed"; error: string };

/**
 * The text the author ends with. An accepted change takes its proposal, which
 * a rework may have replaced; anything not accepted keeps the original.
 */
export function assemble(
  segs: Segment[],
  decisions: Record<string, Decision | undefined>,
  proposals: Record<string, string | undefined> = {},
): string {
  return segs
    .map((s) => {
      if (s.kind === "same") return s.text;
      if (decisions[s.id]?.status !== "accepted") return s.before;
      return proposals[s.id] ?? s.after;
    })
    .join("");
}

/** The text around a change, so a rework can match what surrounds it. */
export function surroundings(
  segs: Segment[],
  id: string,
  reach = 600,
): { before: string; after: string } {
  const at = segs.findIndex((s) => s.kind === "change" && s.id === id);
  const text = (s: Segment) => (s.kind === "same" ? s.text : s.before);
  const before = segs.slice(0, Math.max(at, 0)).map(text).join("");
  const after = segs.slice(at + 1).map(text).join("");
  return { before: before.slice(-reach), after: after.slice(0, reach) };
}
