import { useState, type FormEvent } from 'react'
import type { BookingApi } from './api'
import { dayKey, fullMoment, shortDay, timeIn } from './dates'
import { frLabels, type Labels } from './labels'
import { useBooking, type BookingState } from './useBooking'

export type BookingWidgetProps = {
  api: BookingApi
  slug: string
  /** Shown in the confirmation, e.g. the service's title. */
  title?: string
  cancelHref?: (token: string) => string
  onBooked?: (booking: { start: string; end: string; cancelToken?: string }) => void
  windowDays?: number
  timeZone?: string
  locale?: string
  labels?: Labels
  className?: string
}

function Chevron({ left = false }: { left?: boolean }) {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
      <path d={left ? 'm15 6-6 6 6 6' : 'm9 6 6 6-6 6'} />
    </svg>
  )
}

/**
 * The slot picker and the visitor form, styled by `--booking-*` variables so
 * the host page's colours, radius and font come through (see styles.css).
 */
export function BookingWidget({
  api,
  slug,
  title,
  cancelHref,
  onBooked,
  windowDays,
  timeZone,
  locale = 'fr-FR',
  labels = frLabels,
  className = '',
}: BookingWidgetProps) {
  const state = useBooking({ api, slug, windowDays, timeZone })
  const zone = state.timeZone

  if (state.booking) {
    return (
      <div role="status" className={`lab-booking lab-confirmed ${className}`}>
        <p className="lab-strong">{labels.confirmed}</p>
        <p>
          {title ? `${title} — ` : ''}
          {fullMoment(state.booking.start, zone, locale)}
        </p>
        <p className="lab-muted">{labels.invitationSent}</p>
        {cancelHref && state.booking.cancelToken && (
          <a href={cancelHref(state.booking.cancelToken)} className="lab-link">
            {labels.cancel}
          </a>
        )}
      </div>
    )
  }

  if (state.slot) {
    return (
      <VisitorForm
        state={state}
        label={fullMoment(state.slot.start, zone, locale)}
        labels={labels}
        className={className}
        onBooked={onBooked}
      />
    )
  }

  return (
    <div className={`lab-booking ${className}`}>
      <div className="lab-row">
        <span className="lab-strong">{labels.chooseDay}</span>
        <div className="lab-arrows">
          <button
            type="button"
            aria-label={labels.previousDays}
            disabled={state.windowIndex === 0}
            onClick={state.previousWindow}
            className="lab-icon"
          >
            <Chevron left />
          </button>
          <button type="button" aria-label={labels.nextDays} onClick={state.nextWindow} className="lab-icon">
            <Chevron />
          </button>
        </div>
      </div>
      <div className="lab-days">
        {state.days.map((date) => {
          const key = dayKey(date, zone)
          const open = state.byDay.has(key)
          return (
            <button
              key={key}
              type="button"
              disabled={!open}
              aria-pressed={key === state.selectedDay}
              onClick={() => state.selectDay(key)}
              className="lab-day"
            >
              {shortDay(date, zone, locale)}
            </button>
          )
        })}
      </div>
      {state.slots.status === 'loading' && <p className="lab-muted">{labels.loading}</p>}
      {state.slots.status === 'ready' && state.byDay.size === 0 && <p className="lab-muted">{labels.noAvailability}</p>}
      {state.slots.status === 'error' && (
        <p role="alert" className="lab-error">
          {labels.loadFailed}
        </p>
      )}
      {state.candidates.length > 0 && (
        <div aria-label={labels.slots} className="lab-slots">
          {state.candidates.map((candidate) => (
            <button key={candidate.start} type="button" onClick={() => state.selectSlot(candidate)} className="lab-slot">
              {timeIn(candidate.start, zone, locale)}
            </button>
          ))}
        </div>
      )}
      <p className="lab-muted lab-small">{labels.timeZoneNote(zone)}</p>
    </div>
  )
}

function VisitorForm({
  state,
  label,
  labels,
  className,
  onBooked,
}: {
  state: BookingState
  label: string
  labels: Labels
  className: string
  onBooked?: BookingWidgetProps['onBooked']
}) {
  const [website, setWebsite] = useState('')

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    const text = (key: string) => String(data.get(key) ?? '')
    void state
      .submit({ name: text('name').trim(), email: text('email').trim(), note: text('note'), website })
      .then((booked) => booked && onBooked?.(booked))
  }

  return (
    <form onSubmit={submit} className={`lab-booking lab-form ${className}`}>
      <button type="button" onClick={() => state.selectSlot(null)} className="lab-back">
        <Chevron left />
        <span className="lab-capitalize">{label}</span>
      </button>
      <div className="lab-fields">
        <label className="lab-field">
          {labels.name}
          <input name="name" required maxLength={120} autoComplete="name" className="lab-input" />
        </label>
        <label className="lab-field">
          {labels.email}
          <input name="email" type="email" required autoComplete="email" className="lab-input" />
        </label>
      </div>
      <label className="lab-field">
        {labels.note}
        <textarea name="note" maxLength={2000} rows={3} className="lab-input" />
      </label>
      <div aria-hidden="true" className="lab-honeypot">
        <label>
          Site web
          <input name="website" tabIndex={-1} autoComplete="off" value={website} onChange={(e) => setWebsite(e.target.value)} />
        </label>
      </div>
      {state.error !== undefined && (
        <p role="alert" className="lab-error">
          {labels.failure(state.error ?? undefined)}
        </p>
      )}
      <button type="submit" disabled={state.submitting} className="lab-primary">
        {state.submitting ? labels.sending : labels.confirm}
      </button>
    </form>
  )
}
