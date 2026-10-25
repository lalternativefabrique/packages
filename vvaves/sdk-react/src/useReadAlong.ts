import { useEffect, useMemo, useRef, type RefObject } from 'react'
import { fold, locate, sentenceAt, sentences, spokenOffset, type Folded, type Span } from './readAlong'
import type { VoicePlayback } from './useVoicePlayback'

export type ReadAlongOptions = {
  /** The CSS highlight the page styles with `::highlight(<name>)`. */
  name?: string
  /** Scroll the sentence being read back into view when it leaves the viewport. */
  follow?: boolean
}

export type ReadAlong = {
  /** The sentence being read, as a span of the spoken text. */
  sentence: Span | null
}

type Page = { nodes: Text[]; starts: number[]; folded: Folded }

/**
 * Highlights, in the page the listener is reading, the sentence the voice is
 * on. The spoken text is never the rendered one (Markdown stripped, section
 * names read aloud), so sentences are matched on their letters, forward from
 * the last match, and one the page does not show leaves the previous
 * highlight in place.
 *
 * Draws through the CSS Custom Highlight API, which leaves the DOM untouched:
 * whatever renders the page keeps owning it. Without that API nothing is
 * drawn and `sentence` still reports where the voice is.
 */
export function useReadAlong(
  playback: VoicePlayback,
  container: RefObject<HTMLElement | null>,
  { name = 'vvaves-reading', follow = true }: ReadAlongOptions = {},
): ReadAlong {
  const { state, position, duration, text } = playback
  const spans = useMemo(() => (text ? sentences(text) : []), [text])
  const live = !!text && (state === 'playing' || state === 'paused')
  const index = live ? sentenceAt(spans, spokenOffset(position, duration, text.length)) : -1
  const cursor = useRef({ index: -1, page: 0 })

  useEffect(() => {
    const registry = highlights()
    const root = container.current
    if (index < 0 || !text || !root) {
      cursor.current = { index: -1, page: 0 }
      registry?.delete(name)
      return
    }
    if (index === cursor.current.index) return
    if (index < cursor.current.index) cursor.current.page = 0
    cursor.current.index = index

    const span = spans[index]
    const page = read(root)
    const found = locate(page.folded.chars, fold(text.slice(span.start, span.end)).chars, cursor.current.page)
    if (!found) return
    cursor.current.page = found.end
    if (!registry) return
    const range = toRange(page, found)
    registry.set(name, new Highlight(range))
    if (follow && state === 'playing') reveal(range)
  }, [index, text, spans, container, name, follow, state])

  useEffect(() => () => void highlights()?.delete(name), [name])

  return { sentence: index >= 0 ? spans[index] : null }
}

function highlights(): HighlightRegistry | null {
  if (typeof CSS === 'undefined' || !('highlights' in CSS) || typeof Highlight === 'undefined') return null
  return CSS.highlights
}

function read(root: HTMLElement): Page {
  const nodes: Text[] = []
  const starts: number[] = []
  let full = ''
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT)
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    nodes.push(n as Text)
    starts.push(full.length)
    full += n.nodeValue ?? ''
  }
  return { nodes, starts, folded: fold(full) }
}

function toRange(page: Page, found: Span): Range {
  const range = document.createRange()
  const [startNode, startOffset] = pointAt(page, page.folded.source[found.start])
  const [endNode, endOffset] = pointAt(page, page.folded.source[found.end - 1])
  range.setStart(startNode, startOffset)
  range.setEnd(endNode, endOffset + 1)
  return range
}

function pointAt(page: Page, at: number): [Text, number] {
  let lo = 0
  let hi = page.starts.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (page.starts[mid] <= at) lo = mid
    else hi = mid - 1
  }
  return [page.nodes[lo], at - page.starts[lo]]
}

function reveal(range: Range) {
  const rect = range.getBoundingClientRect()
  if (rect.top >= 0 && rect.bottom <= window.innerHeight) return
  range.startContainer.parentElement?.scrollIntoView({ block: 'center', behavior: 'smooth' })
}
