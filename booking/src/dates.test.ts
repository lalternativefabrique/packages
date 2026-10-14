import assert from 'node:assert/strict'
import { test } from 'node:test'
import { daysFrom, slotsByDay, timeIn } from './dates.ts'

test('slots group by the day they fall on in the given zone', () => {
  const byDay = slotsByDay(
    [
      { start: '2026-10-14T22:30:00Z', end: '2026-10-14T23:00:00Z' },
      { start: '2026-10-15T07:00:00Z', end: '2026-10-15T07:30:00Z' },
    ],
    'Europe/Paris',
  )
  assert.deepEqual([...byDay.keys()], ['2026-10-15'])
  assert.equal(byDay.get('2026-10-15')?.length, 2)
})

test('days run from the start without skipping month ends', () => {
  const days = daysFrom(new Date(2026, 9, 30), 3)
  assert.deepEqual(
    days.map((d) => d.getDate()),
    [30, 31, 1],
  )
})

test('times print in the visitor zone', () => {
  assert.equal(timeIn('2026-10-15T07:00:00Z', 'Europe/Paris', 'fr-FR'), '09:00')
})
