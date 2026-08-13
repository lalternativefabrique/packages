import { useCallback, useMemo, useState } from "react";

import { blocksFromDoc, docFromBlocks, shapesFromDoc, type RichDoc } from "./document";
import {
  accept,
  acceptAll,
  openRevision,
  reject,
  rejectAll,
  resolved,
  type RevisionState,
} from "./revision-state";

export interface UseRevisions {
  state: RevisionState | null;
  /** The document as it stands, counting the decisions made so far. */
  doc: RichDoc | null;
  pending: number;
  open: (revised: string[]) => void;
  accept: (id: string) => void;
  reject: (id: string) => void;
  acceptAll: () => void;
  rejectAll: () => void;
  /** Ends the review, keeping whatever `doc` currently reads as. */
  close: () => void;
}

/**
 * useRevisions holds one review of what a model proposed.
 *
 * The suggestions never enter the document: they live here, beside it, so the
 * editor's own content stays valid and saveable at every point of the review.
 */
export function useRevisions(source: RichDoc | null): UseRevisions {
  const [state, setState] = useState<RevisionState | null>(null);

  const original = useMemo(() => (source ? blocksFromDoc(source) : []), [source]);
  const shapes = useMemo(() => (source ? shapesFromDoc(source) : []), [source]);

  const open = useCallback(
    (revised: string[]) => setState(openRevision(original, revised)),
    [original],
  );

  const doc = useMemo(() => {
    if (!state) return null;
    const entries = resolved(state);
    return docFromBlocks(
      entries.map((entry) => entry.text),
      entries.map((entry) => shapes[entry.sourceIndex] ?? { type: "paragraph" as const }),
    );
  }, [state, shapes]);

  return {
    state,
    doc,
    pending: state?.pending ?? 0,
    open,
    accept: useCallback((id: string) => setState((s) => (s ? accept(s, id) : s)), []),
    reject: useCallback((id: string) => setState((s) => (s ? reject(s, id) : s)), []),
    acceptAll: useCallback(() => setState((s) => (s ? acceptAll(s) : s)), []),
    rejectAll: useCallback(() => setState((s) => (s ? rejectAll(s) : s)), []),
    close: useCallback(() => setState(null), []),
  };
}
