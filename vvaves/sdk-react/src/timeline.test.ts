import { test } from 'node:test'
import assert from 'node:assert/strict'
import {
  appendPiece,
  bufferedSeconds,
  clampRate,
  clampSeek,
  planFrom,
  positionAt,
  type Piece,
} from './timeline.ts'

function pieces(...durations: number[]): Piece[] {
  const out: Piece[] = []
  for (const d of durations) appendPiece(out, d)
  return out
}

test('pieces carry their cumulative start offsets', () => {
  const p = pieces(2, 3, 1.5)
  assert.deepEqual(p.map((x) => x.start), [0, 2, 5])
  assert.equal(bufferedSeconds(p), 6.5)
  assert.equal(bufferedSeconds([]), 0)
})

test('playing from the start schedules every piece back to back', () => {
  const { scheduled, nextStartAt } = planFrom(pieces(2, 3), 0, 10, 1)
  assert.deepEqual(scheduled, [
    { index: 0, when: 10, offset: 0 },
    { index: 1, when: 12, offset: 0 },
  ])
  assert.equal(nextStartAt, 15)
})

test('seeking starts inside the piece holding the target', () => {
  const { scheduled, nextStartAt } = planFrom(pieces(2, 3, 1), 3, 0, 1)
  assert.deepEqual(scheduled, [
    { index: 1, when: 0, offset: 1 },
    { index: 2, when: 2, offset: 0 },
  ])
  assert.equal(nextStartAt, 3)
})

test('a target on a boundary starts the next piece at its beginning', () => {
  const { scheduled } = planFrom(pieces(2, 3), 2, 0, 1)
  assert.deepEqual(scheduled, [{ index: 1, when: 0, offset: 0 }])
})

test('a faster rate shortens each piece in context time', () => {
  const { scheduled, nextStartAt } = planFrom(pieces(2, 4), 1, 0, 2)
  assert.deepEqual(scheduled, [
    { index: 0, when: 0, offset: 1 },
    { index: 1, when: 0.5, offset: 0 },
  ])
  assert.equal(nextStartAt, 2.5)
})

test('a target past what is decoded schedules nothing and waits at now', () => {
  const { scheduled, nextStartAt } = planFrom(pieces(2), 2, 7, 1)
  assert.deepEqual(scheduled, [])
  assert.equal(nextStartAt, 7)
})

test('position advances by elapsed context time times rate, capped at buffered', () => {
  const anchor = { offset: 4, contextTime: 10 }
  assert.equal(positionAt(anchor, 11, 1, 100), 5)
  assert.equal(positionAt(anchor, 11, 2, 100), 6)
  assert.equal(positionAt(anchor, 12, 0.5, 100), 5)
  assert.equal(positionAt(anchor, 50, 1, 8), 8)
  assert.equal(positionAt(anchor, 9, 1, 100), 4)
})

test('seek targets are clamped to what is decoded', () => {
  assert.equal(clampSeek(-3, 10), 0)
  assert.equal(clampSeek(4, 10), 4)
  assert.equal(clampSeek(25, 10), 10)
  assert.equal(clampSeek(Number.NaN, 10), 0)
})

test('rates are clamped to 0.5..3', () => {
  assert.equal(clampRate(0.1), 0.5)
  assert.equal(clampRate(1.25), 1.25)
  assert.equal(clampRate(9), 3)
  assert.equal(clampRate(Number.NaN), 1)
})
