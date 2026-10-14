export type Slot = { start: string; end: string }

export type BookRequest = {
  start: string
  name: string
  email: string
  note?: string
  /** Honeypot: people never see the field, bots fill it. */
  website?: string
}

export type Booking = { start: string; end: string; cancelToken?: string }

/** What the widget needs from the product's server; the transport is the host's. */
export type BookingApi = {
  listSlots: (slug: string, from: string, to: string) => Promise<Slot[]>
  book: (slug: string, request: BookRequest) => Promise<Booking>
}

export class BookingError extends Error {
  readonly status: number
  constructor(status: number, message = `booking request failed with ${status}`) {
    super(message)
    this.status = status
  }
}

type CoreSlot = { start?: string; end?: string }
type CoreBooking = { start?: string; end?: string; cancel_token?: string }

/**
 * The messag core's public booking endpoints, mounted under `baseUrl`
 * (`/api/v1` behind the same-origin proxy, or the core's full origin).
 */
export function coreBookingApi(baseUrl: string, fetchImpl: typeof fetch = (...args) => fetch(...args)): BookingApi {
  const base = baseUrl.replace(/\/$/, '')
  const path = (slug: string, tail = '') => `${base}/public/booking/${encodeURIComponent(slug)}${tail}`
  return {
    async listSlots(slug, from, to) {
      const res = await fetchImpl(path(slug, `/slots?${new URLSearchParams({ from, to })}`))
      if (!res.ok) throw new BookingError(res.status)
      const slots = (await res.json()) as CoreSlot[]
      return slots.flatMap((s) => (s.start && s.end ? [{ start: s.start, end: s.end }] : []))
    },
    async book(slug, request) {
      const res = await fetchImpl(path(slug), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(request),
      })
      if (!res.ok) throw new BookingError(res.status)
      const booked = (await res.json()) as CoreBooking
      return { start: booked.start ?? request.start, end: booked.end ?? '', cancelToken: booked.cancel_token }
    },
  }
}
