import './styles.css'

export { BookingWidget, type BookingWidgetProps } from './BookingWidget'
export { useBooking, type BookingState, type SlotsState, type UseBookingOptions } from './useBooking'
export { coreBookingApi, BookingError, type Booking, type BookingApi, type BookRequest, type Slot } from './api'
export { frLabels, type Labels } from './labels'
export { dayKey, daysFrom, fullMoment, shortDay, slotsByDay, startOfToday, timeIn } from './dates'
