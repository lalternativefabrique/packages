# @lalternative/booking

The slot picker and visitor form shared by L'Alternative apps: a window of
days, the slots of the chosen day, name and e-mail, confirmation with a
cancel link. The product's server owns the data; the widget only needs two
calls.

```tsx
import '@lalternative/booking/styles.css'
import { BookingWidget, coreBookingApi } from '@lalternative/booking'

const api = coreBookingApi('/api/v1')

<BookingWidget api={api} slug="consultation" title="Consultation" cancelHref={(token) => `/r/annuler/${token}`} />
```

`coreBookingApi` speaks to the messag core's public booking endpoints. Any
other server fits by implementing `BookingApi` (`listSlots`, `book`).

## Looking like the host page

The widget carries no colours of its own: everything falls back to
`currentColor` and the page's font. Set the variables on an ancestor to match
a theme:

```css
.my-page {
  --booking-fg: #1a1a1a;
  --booking-muted: #6b7280;
  --booking-border: #e5e7eb;
  --booking-surface: #fff;
  --booking-accent: #2563eb;
  --booking-accent-fg: #fff;
  --booking-radius: 0.75rem;
  --booking-font: inherit;
}
```

Class names are prefixed `lab-` for targeted overrides.

## Own markup

`useBooking({ api, slug })` returns the whole flow as state and handlers
(`days`, `byDay`, `selectDay`, `candidates`, `selectSlot`, `submit`,
`booking`) for a product that wants its own markup.

Labels are French by default; pass `labels` (see `Labels`) for another
language and `locale` for date formatting.
