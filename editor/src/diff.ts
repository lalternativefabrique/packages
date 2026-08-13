export type ChangeKind = "keep" | "insert" | "delete" | "replace";

export interface BlockChange {
  id: string;
  kind: ChangeKind;
  before: string;
  after: string;
}

// Below this, two blocks are different things rather than one rewritten twice,
// and pairing them would hide a deletion behind an unrelated insertion.
const PAIR_THRESHOLD = 0.34;

/**
 * diffBlocks compares two documents one block at a time.
 *
 * The block is the unit because it is the unit a reviewer accepts or rejects:
 * a finer diff turns one editorial decision into a dozen clicks, and a coarser
 * one makes rejecting a single sentence cost the whole section.
 *
 * Accepting every change rebuilds `after` exactly; rejecting every change
 * rebuilds `before` exactly. The UI relies on both.
 */
export function diffBlocks(before: string[], after: string[]): BlockChange[] {
  const common = longestCommonSubsequence(before, after);

  const changes: BlockChange[] = [];
  let i = 0;
  let j = 0;
  let seq = 0;
  const id = () => `c${seq++}`;

  const flush = (removed: string[], added: string[]) => {
    // Blocks that vanish and appear at the same place between two unchanged
    // anchors are one rewrite, and the reviewer wants one decision rather than
    // two that can be answered inconsistently. When the counts differ, the
    // extras are genuine insertions or deletions, so only pair the ones that
    // read as rewrites of each other.
    const balanced = removed.length === added.length;
    const paired = Math.min(removed.length, added.length);
    let p = 0;
    for (; p < paired; p++) {
      if (!balanced && similarity(removed[p], added[p]) < PAIR_THRESHOLD) break;
      changes.push({ id: id(), kind: "replace", before: removed[p], after: added[p] });
    }
    for (let r = p; r < removed.length; r++) {
      changes.push({ id: id(), kind: "delete", before: removed[r], after: "" });
    }
    for (let a = p; a < added.length; a++) {
      changes.push({ id: id(), kind: "insert", before: "", after: added[a] });
    }
  };

  for (const anchor of common) {
    const removed: string[] = [];
    const added: string[] = [];
    while (i < before.length && before[i] !== anchor) removed.push(before[i++]);
    while (j < after.length && after[j] !== anchor) added.push(after[j++]);
    flush(removed, added);
    changes.push({ id: id(), kind: "keep", before: anchor, after: anchor });
    i++;
    j++;
  }
  flush(before.slice(i), after.slice(j));

  return changes;
}

function longestCommonSubsequence(a: string[], b: string[]): string[] {
  const table: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      table[i][j] =
        a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }

  const common: string[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      common.push(a[i]);
      i++;
      j++;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      i++;
    } else {
      j++;
    }
  }
  return common;
}

// Word overlap rather than character distance: an LLM rewrite keeps most of the
// vocabulary and reorders it, which edit distance scores as a near-total change.
function similarity(a: string, b: string): number {
  const left = words(a);
  const right = words(b);
  if (left.size === 0 || right.size === 0) return 0;

  let shared = 0;
  for (const word of left) if (right.has(word)) shared++;
  return shared / Math.max(left.size, right.size);
}

function words(s: string): Set<string> {
  return new Set(s.toLowerCase().match(/\p{L}+/gu) ?? []);
}
