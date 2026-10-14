import type { Slot } from './api'

export const dayKey = (date: Date, timeZone: string): string =>
  new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(date)

export function slotsByDay(slots: Slot[], timeZone: string): Map<string, Slot[]> {
  const out = new Map<string, Slot[]>()
  for (const slot of slots) {
    const key = dayKey(new Date(slot.start), timeZone)
    out.set(key, [...(out.get(key) ?? []), slot])
  }
  return out
}

export function daysFrom(start: Date, count: number): Date[] {
  return Array.from({ length: count }, (_, i) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + i))
}

export function startOfToday(now = new Date()): Date {
  return new Date(now.getFullYear(), now.getMonth(), now.getDate())
}

export function timeIn(iso: string, timeZone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone, hour: '2-digit', minute: '2-digit' }).format(new Date(iso))
}

export function shortDay(date: Date, timeZone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, { timeZone, weekday: 'short', day: 'numeric', month: 'short' }).format(date)
}

export function fullMoment(iso: string, timeZone: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    timeZone,
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(iso))
}
