export interface TextRange {
  from: number;
  to: number;
}

export interface RevisionPart {
  before: string;
  after: string;
}

// Letters, digits, and the marks that belong inside a French word.
const WORD = /[\p{L}\p{N}'’-]/u;

/**
 * expandToWords grows a range out to whole words.
 *
 * A selection dragged across a line lands mid-word more often than not, and a
 * model handed "ns de production" answers for a fragment — which then reads as
 * a truncated diff the author has to decode.
 */
export function expandToWords(text: string, from: number, to: number): TextRange {
  if (from >= to) return { from, to };

  let start = from;
  while (start > 0 && WORD.test(text[start - 1]) && WORD.test(text[start])) start--;

  let end = to;
  while (end < text.length && WORD.test(text[end]) && WORD.test(text[end - 1])) end++;

  return { from: start, to: end };
}

/**
 * splitRevision turns one model answer into the decisions it really contains.
 *
 * A three-paragraph rewrite accepted or rejected as a whole forces the author
 * to keep a weak paragraph for a strong one, so each paragraph becomes its own
 * decision — but only when both sides line up. When the model merged or split
 * paragraphs, position no longer maps them, and pairing by index would show a
 * diff that never existed.
 */
export function splitRevision(before: string, after: string): RevisionPart[] {
  if (before.trim() === after.trim()) return [];

  const olds = paragraphs(before);
  const news = paragraphs(after);

  if (olds.length !== news.length || olds.length === 0) {
    return [{ before: before.trim(), after: after.trim() }];
  }

  const parts: RevisionPart[] = [];
  for (let i = 0; i < olds.length; i++) {
    if (olds[i] !== news[i]) parts.push({ before: olds[i], after: news[i] });
  }
  return parts;
}

function paragraphs(s: string): string[] {
  return s
    .split(/\n\s*\n/)
    .map((p) => p.trim())
    .filter((p) => p !== "");
}
