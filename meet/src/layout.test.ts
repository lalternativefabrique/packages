import { test } from 'node:test'
import assert from 'node:assert/strict'
import { arrange, canShareScreen, defaultLayout, parseStage, type Tile } from './layout.ts'

const camA: Tile = { id: 'camA', participant: 'a', kind: 'camera' }
const camB: Tile = { id: 'camB', participant: 'b', kind: 'camera', speaking: true }
const scrA1: Tile = { id: 'scrA1', participant: 'a', kind: 'screen' }
const scrA2: Tile = { id: 'scrA2', participant: 'a', kind: 'screen' }

test('parseStage reads the pinned track and ignores garbage', () => {
  assert.deepEqual(parseStage('{"pinnedTrack":"T1","pinnedBy":"a"}'), { pinnedTrack: 'T1', pinnedBy: 'a' })
  assert.deepEqual(parseStage(''), {})
  assert.deepEqual(parseStage('not json'), {})
  assert.deepEqual(parseStage('{"pinnedTrack":42}'), { pinnedTrack: undefined, pinnedBy: undefined })
})

test('canShareScreen enforces per-participant and per-room limits', () => {
  assert.equal(canShareScreen(0, 0), true)
  assert.equal(canShareScreen(1, 1), true)
  assert.equal(canShareScreen(2, 2), false)
  assert.equal(canShareScreen(0, 4), false)
})

test('grid puts screens first, then cameras', () => {
  assert.deepEqual(arrange([camA, scrA1, camB], 'grid', {}).main.map((t) => t.id), ['scrA1', 'camA', 'camB'])
})

test('screens layout shows every screen side by side with cameras in the strip', () => {
  const r = arrange([camA, scrA1, scrA2, camB], 'screens', {})
  assert.deepEqual(r.main.map((t) => t.id), ['scrA1', 'scrA2'])
  assert.deepEqual(r.strip.map((t) => t.id), ['camA', 'camB'])
})

test('stage follows the local focus, then the pin, then a screen, then the speaker', () => {
  const tiles = [camA, camB, scrA1, scrA2]
  assert.equal(arrange(tiles, 'stage', { pinnedTrack: 'scrA2' }, 'camA').main[0]?.id, 'camA')
  assert.equal(arrange(tiles, 'stage', { pinnedTrack: 'scrA2' }).main[0]?.id, 'scrA2')
  assert.equal(arrange(tiles, 'stage', { pinnedTrack: 'gone' }).main[0]?.id, 'scrA1')
  assert.equal(arrange([camA, camB], 'stage', {}).main[0]?.id, 'camB')
  assert.equal(arrange(tiles, 'stage', {}).strip.length, 3)
})

test('stage with nobody is empty', () => {
  assert.deepEqual(arrange([], 'stage', {}), { main: [], strip: [] })
})

test('defaultLayout picks screens, stage or grid', () => {
  assert.equal(defaultLayout([camA, scrA1, scrA2]), 'screens')
  assert.equal(defaultLayout([camA, scrA1]), 'stage')
  assert.equal(defaultLayout([camA, camB]), 'grid')
})
