import { useEffect, useMemo, useState } from 'react'
import type { Booking, BookingApi, BookRequest, Slot } from './api'
import { dayKey, daysFrom, slotsByDay, startOfToday } from './dates'

export type SlotsState = { status: 'loading' | 'ready' | 'error'; slots: Slot[] }

export type UseBookingOptions = {
  api: BookingApi
  slug: string
  /** Days shown per window; the arrows move by that many. */
  windowDays?: number
  /** Where the days are laid out; defaults to the visitor's zone. */
  timeZone?: string
}

export type BookingState = {
  timeZone: string
  days: Date[]
  byDay: Map<string, Slot[]>
  slots: SlotsState
  windowIndex: number
  previousWindow: () => void
  nextWindow: () => void
  selectedDay: string | null
  selectDay: (key: string) => void
  candidates: Slot[]
  slot: Slot | null
  selectSlot: (slot: Slot | null) => void
  submitting: boolean
  error: number | null | undefined
  submit: (visitor: Omit<BookRequest, 'start'>) => Promise<Booking | null>
  booking: Booking | null
  reset: () => void
}

const visitorZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone

/** The whole flow, without markup: window of days → day → slot → visitor → confirmation. */
export function useBooking({ api, slug, windowDays = 14, timeZone }: UseBookingOptions): BookingState {
  const zone = useMemo(() => timeZone ?? visitorZone(), [timeZone])
  const [windowIndex, setWindowIndex] = useState(0)
  const [slots, setSlots] = useState<SlotsState>({ status: 'loading', slots: [] })
  const [day, setDay] = useState<string | null>(null)
  const [slot, setSlot] = useState<Slot | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<number | null | undefined>(undefined)
  const [booking, setBooking] = useState<Booking | null>(null)

  const from = useMemo(() => {
    const start = startOfToday()
    start.setDate(start.getDate() + windowIndex * windowDays)
    return start
  }, [windowIndex, windowDays])
  const days = useMemo(() => daysFrom(from, windowDays), [from, windowDays])

  useEffect(() => {
    let live = true
    setSlots({ status: 'loading', slots: [] })
    const to = daysFrom(from, windowDays + 1).at(-1)!
    api
      .listSlots(slug, from.toISOString(), to.toISOString())
      .then((items) => live && setSlots({ status: 'ready', slots: items }))
      .catch(() => live && setSlots({ status: 'error', slots: [] }))
    return () => {
      live = false
    }
  }, [api, slug, from, windowDays])

  const byDay = useMemo(() => slotsByDay(slots.slots, zone), [slots.slots, zone])
  const firstOpen = days.map((d) => dayKey(d, zone)).find((key) => byDay.has(key)) ?? null
  const selectedDay = day && byDay.has(day) ? day : firstOpen
  const candidates = selectedDay ? (byDay.get(selectedDay) ?? []) : []

  const submit = async (visitor: Omit<BookRequest, 'start'>) => {
    if (!slot) return null
    setSubmitting(true)
    setError(undefined)
    try {
      const booked = await api.book(slug, { ...visitor, start: slot.start })
      setBooking(booked)
      return booked
    } catch (err) {
      setError((err as { status?: number })?.status ?? null)
      return null
    } finally {
      setSubmitting(false)
    }
  }

  const reset = () => {
    setSlot(null)
    setBooking(null)
    setError(undefined)
  }

  return {
    timeZone: zone,
    days,
    byDay,
    slots,
    windowIndex,
    previousWindow: () => setWindowIndex((i) => Math.max(0, i - 1)),
    nextWindow: () => setWindowIndex((i) => i + 1),
    selectedDay,
    selectDay: setDay,
    candidates,
    slot,
    selectSlot: setSlot,
    submitting,
    error,
    submit,
    booking,
    reset,
  }
}
