import { segments } from "./diff";

export interface CodeNote {
  /** Line in the new version the note is about. */
  line: number;
  text: string;
  author?: string;
}

export type CodeRow =
  | { kind: "same"; key: string; old: number; new: number; text: string }
  | { kind: "fold"; key: string; rows: Extract<CodeRow, { kind: "same" }>[] }
  | {
      kind: "change";
      key: string;
      id: string;
      before: string;
      after: string;
      oldFrom: number;
      newFrom: number;
      oldCount: number;
      newCount: number;
    }
  | { kind: "note"; key: string; note: CodeNote };

type Same = Extract<CodeRow, { kind: "same" }>;

function splitLines(text: string): string[] {
  return text.match(/[^\n]*\n|[^\n]+/g) ?? [];
}

/**
 * Lays a file's change out the way a code review reads it: changed blocks with
 * their line numbers, `context` unchanged lines around each, longer unchanged
 * runs folded, and each note placed after the line it is about.
 */
export function codeRows(before: string, after: string, context = 3, notes: CodeNote[] = []): CodeRow[] {
  const segs = segments(before, after, "c", "line");
  const rows: CodeRow[] = [];
  let oldN = 1;
  let newN = 1;
  segs.forEach((s, i) => {
    if (s.kind === "change") {
      const oldCount = splitLines(s.before).length;
      const newCount = splitLines(s.after).length;
      rows.push({ kind: "change", key: s.id, id: s.id, before: s.before, after: s.after, oldFrom: oldN, newFrom: newN, oldCount, newCount });
      oldN += oldCount;
      newN += newCount;
      return;
    }
    const same: Same[] = splitLines(s.text).map((text) => ({
      kind: "same",
      key: `s${oldN}-${newN}`,
      old: oldN++,
      new: newN++,
      text: text.replace(/\n$/, ""),
    }));
    const head = i === 0 ? 0 : context;
    const tail = i === segs.length - 1 ? 0 : context;
    if (same.length <= head + tail + 1) {
      rows.push(...same);
      return;
    }
    rows.push(...same.slice(0, head));
    const hidden = same.slice(head, same.length - tail);
    rows.push({ kind: "fold", key: `f${hidden[0].key}`, rows: hidden });
    rows.push(...same.slice(same.length - tail));
  });
  return placeNotes(rows, notes);
}

function lastNewLine(r: CodeRow): number {
  switch (r.kind) {
    case "same":
      return r.new;
    case "fold":
      return r.rows[r.rows.length - 1].new;
    case "change":
      return r.newFrom + r.newCount - 1;
    default:
      return -1;
  }
}

function placeNotes(rows: CodeRow[], notes: CodeNote[]): CodeRow[] {
  if (notes.length === 0) return rows;
  const out: CodeRow[] = [];
  const left = [...notes].sort((a, b) => a.line - b.line);
  rows.forEach((r) => {
    out.push(r);
    const upTo = lastNewLine(r);
    while (left.length > 0 && left[0].line <= upTo) {
      const note = left.shift()!;
      out.push({ kind: "note", key: `n${note.line}-${out.length}`, note });
    }
  });
  for (const note of left) out.push({ kind: "note", key: `n${note.line}-${out.length}`, note });
  return out;
}
