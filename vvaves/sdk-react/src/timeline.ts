export type Piece = { start: number; duration: number }

export type ScheduledPiece = { index: number; when: number; offset: number }

export type Anchor = { offset: number; contextTime: number }

export const MIN_RATE = 0.5
export const MAX_RATE = 3

export function appendPiece(pieces: Piece[], duration: number): Piece {
  const last = pieces[pieces.length - 1]
  const piece = { start: last ? last.start + last.duration : 0, duration }
  pieces.push(piece)
  return piece
}

export function bufferedSeconds(pieces: Piece[]): number {
  const last = pieces[pieces.length - 1]
  return last ? last.start + last.duration : 0
}

export function clampSeek(target: number, buffered: number): number {
  if (!Number.isFinite(target)) return 0
  return Math.min(Math.max(target, 0), buffered)
}

export function clampRate(rate: number): number {
  if (!Number.isFinite(rate)) return 1
  return Math.min(Math.max(rate, MIN_RATE), MAX_RATE)
}

/**
 * Plans the sources that play the reading from offset onwards, started at
 * contextTime now. A source at playbackRate r lasts duration / r of context
 * time. nextStartAt is where a piece decoded later must start.
 */
export function planFrom(
  pieces: Piece[],
  offset: number,
  now: number,
  rate: number,
): { scheduled: ScheduledPiece[]; nextStartAt: number } {
  const scheduled: ScheduledPiece[] = []
  let when = now
  for (let index = 0; index < pieces.length; index++) {
    const piece = pieces[index]
    const end = piece.start + piece.duration
    if (end <= offset) continue
    const inPiece = Math.max(0, offset - piece.start)
    scheduled.push({ index, when, offset: inPiece })
    when += (piece.duration - inPiece) / rate
  }
  return { scheduled, nextStartAt: when }
}

/** Seconds into the reading: elapsed context time covers rate times as much audio. */
export function positionAt(anchor: Anchor, now: number, rate: number, buffered: number): number {
  const elapsed = Math.max(0, now - anchor.contextTime)
  return Math.min(anchor.offset + elapsed * rate, buffered)
}
