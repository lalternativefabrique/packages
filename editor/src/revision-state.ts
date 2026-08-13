import { diffBlocks, type BlockChange, type ChangeKind } from "./diff";

export type { BlockChange, ChangeKind };

export type Decision = "pending" | "accepted" | "rejected";

export interface RevisionState {
  changes: BlockChange[];
  decisions: Record<string, Decision>;
  pending: number;
}

/**
 * openRevision starts a review of what the model returned.
 *
 * Nothing is applied yet: the document the user sees keeps reading as it did,
 * with the proposals shown beside it, until they decide one at a time.
 */
export function openRevision(before: string[], after: string[]): RevisionState {
  const changes = diffBlocks(before, after);
  const decisions: Record<string, Decision> = {};
  for (const change of changes) {
    decisions[change.id] = change.kind === "keep" ? "accepted" : "pending";
  }
  return { changes, decisions, pending: countPending(changes, decisions) };
}

export function accept(state: RevisionState, id: string): RevisionState {
  return decide(state, id, "accepted");
}

export function reject(state: RevisionState, id: string): RevisionState {
  return decide(state, id, "rejected");
}

export function acceptAll(state: RevisionState): RevisionState {
  return decideAll(state, "accepted");
}

export function rejectAll(state: RevisionState): RevisionState {
  return decideAll(state, "rejected");
}

/**
 * resolvedBlocks is the document as it currently stands, counting every
 * decision made so far and leaving undecided changes at their original text.
 *
 * It is always a valid document, so the editor can save mid-review rather than
 * forcing the user to finish before they are allowed to keep their work.
 */
export function resolvedBlocks(state: RevisionState): string[] {
  return resolved(state).map((entry) => entry.text);
}

/**
 * resolved is resolvedBlocks with the origin of each block.
 *
 * `sourceIndex` is where the block sat in the document under review, or -1 for
 * one the model added. A caller restoring block shapes needs it: accepting an
 * insertion shifts every later block, so positions in the result no longer
 * line up with positions in the source.
 */
export function resolved(state: RevisionState): Array<{ text: string; sourceIndex: number }> {
  const blocks: Array<{ text: string; sourceIndex: number }> = [];
  let source = 0;
  for (const change of state.changes) {
    const decided = state.decisions[change.id] === "accepted";
    switch (change.kind) {
      case "keep":
        blocks.push({ text: change.after, sourceIndex: source++ });
        break;
      case "insert":
        if (decided) blocks.push({ text: change.after, sourceIndex: -1 });
        break;
      case "delete":
        if (!decided) blocks.push({ text: change.before, sourceIndex: source });
        source++;
        break;
      case "replace":
        blocks.push({ text: decided ? change.after : change.before, sourceIndex: source++ });
        break;
    }
  }
  return blocks;
}

function decide(state: RevisionState, id: string, decision: Decision): RevisionState {
  if (!(id in state.decisions) || state.decisions[id] === decision) return state;

  const decisions = { ...state.decisions, [id]: decision };
  return { ...state, decisions, pending: countPending(state.changes, decisions) };
}

function decideAll(state: RevisionState, decision: Decision): RevisionState {
  const decisions: Record<string, Decision> = {};
  for (const change of state.changes) {
    decisions[change.id] = change.kind === "keep" ? "accepted" : decision;
  }
  return { ...state, decisions, pending: 0 };
}

function countPending(changes: BlockChange[], decisions: Record<string, Decision>): number {
  return changes.filter((c) => c.kind !== "keep" && decisions[c.id] === "pending").length;
}
