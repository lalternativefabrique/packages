export type Span = { start: number; end: number }

export type Folded = { chars: string; source: number[] }

/** Spoken French, spaces and punctuation included, at playback rate 1. */
export const CHARS_PER_SECOND = 15

const PREFIX_CHARS = 24

export function sentences(text: string): Span[] {
  const out: Span[] = []
  const boundary = /[.!?…]+(?=\s)|\n+/g
  let start = 0
  for (const m of text.matchAll(boundary)) {
    const end = m.index + m[0].length
    pushTrimmed(out, text, start, end)
    start = end
  }
  pushTrimmed(out, text, start, text.length)
  return out
}

function pushTrimmed(out: Span[], text: string, start: number, end: number) {
  while (start < end && /\s/.test(text[start])) start++
  while (end > start && /\s/.test(text[end - 1])) end--
  if (end > start) out.push({ start, end })
}

/**
 * Where in the text the voice is. A reading's duration is only known once the
 * whole of it is decoded, so a first, streamed listen runs on a speaking rate
 * until then.
 */
export function spokenOffset(position: number, duration: number | null, length: number): number {
  const at = duration && duration > 0 ? (position / duration) * length : position * CHARS_PER_SECOND
  return Math.min(Math.max(at, 0), length)
}

export function sentenceAt(spans: Span[], offset: number): number {
  let lo = 0
  let hi = spans.length - 1
  let found = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (spans[mid].start <= offset) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  return found
}

/**
 * Letters and digits only, lowercased and without accents: what the spoken
 * text and the rendered page still share once Markdown, list markers and
 * typographic quotes have gone their separate ways.
 */
export function fold(text: string): Folded {
  let chars = ''
  const source: number[] = []
  for (let i = 0; i < text.length; i++) {
    const kept = text[i].normalize('NFD').replace(/[^\p{L}\p{N}]/gu, '').toLowerCase()
    for (const c of kept) {
      chars += c
      source.push(i)
    }
  }
  return { chars, source }
}

/** The folded needle's span in the folded haystack, searched from `from` on. */
export function locate(haystack: string, needle: string, from: number): Span | null {
  if (!needle) return null
  const whole = haystack.indexOf(needle, from)
  if (whole >= 0) return { start: whole, end: whole + needle.length }
  if (needle.length <= PREFIX_CHARS) return null
  const prefix = haystack.indexOf(needle.slice(0, PREFIX_CHARS), from)
  return prefix >= 0 ? { start: prefix, end: prefix + PREFIX_CHARS } : null
}
