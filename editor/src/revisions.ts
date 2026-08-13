export {
  accept,
  acceptAll,
  openRevision,
  reject,
  rejectAll,
  resolved,
  resolvedBlocks,
} from "./revision-state";
export type { Decision, RevisionState } from "./revision-state";
export { useRevisions } from "./useRevisions";
export type { UseRevisions } from "./useRevisions";
export type { BlockChange, ChangeKind } from "./diff";
