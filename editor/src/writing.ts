import { splitRevision, type RevisionPart } from "./passage";

export const MIN_PASSAGE_CHARS = 12;

export function canAssist(passage: string): boolean {
  return passage.trim().length >= MIN_PASSAGE_CHARS;
}

/** Thrown by a host's callback when its backend has no model or search configured. */
export class WritingNotAvailableError extends Error {
  constructor() {
    super("writing assistance is not configured");
    this.name = "WritingNotAvailableError";
  }
}

export type Revise = (passage: string, instruction: string) => Promise<string>;

export const defaultRevisionPresets: readonly string[] = [
  "Rends-le plus concret",
  "Raccourcis",
  "Simplifie",
  "Rends-le plus direct",
];

export function normalizeInstruction(instruction: string): string | null {
  const trimmed = instruction.trim();
  return trimmed === "" ? null : trimmed;
}

export function proposalChanges(passage: string, proposal: string): RevisionPart[] {
  return splitRevision(passage, proposal);
}

export interface FoundSource {
  title: string;
  url: string;
  excerpt: string;
}

export type FindSources = (passage: string) => Promise<FoundSource[]>;

export function hostOf(url: string): string {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
}
