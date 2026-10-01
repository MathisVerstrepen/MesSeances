import type { TheaterActivityItem } from '../types/api.ts'
import { cinemaMovieTarget } from './cinemaMovieTarget.ts'
import { isCalendarDate } from './date.ts'

const observationFormatter = new Intl.DateTimeFormat('fr-FR', {
  timeZone: 'Europe/Paris',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
})
const fullDateFormatter = new Intl.DateTimeFormat('fr-FR', {
  timeZone: 'Europe/Paris',
  weekday: 'long',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

export function activityObservationDay(timestamp: string): string {
  const date = new Date(timestamp)
  if (!Number.isFinite(date.getTime())) return ''
  const parts = observationFormatter.formatToParts(date)
  const value = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? ''
  return `${value('year')}-${value('month')}-${value('day')}`
}

export function activityFullDate(date: string): string {
  if (!isCalendarDate(date)) return date
  const [year = 0, month = 0, day = 0] = date.split('-').map(Number)
  return fullDateFormatter.format(new Date(Date.UTC(year, month - 1, day, 12)))
}

export function activityTypeLabel(type: TheaterActivityItem['type']): string {
  return type === 'return_to_program'
    ? 'Retour à l’affiche'
    : 'Ajout à la programmation'
}

export function appendActivityItems(
  current: readonly TheaterActivityItem[],
  next: readonly TheaterActivityItem[],
): TheaterActivityItem[] {
  const seen = new Set<string>()
  return [...current, ...next].filter((item) => {
    if (seen.has(item.event_id)) return false
    seen.add(item.event_id)
    return true
  })
}

export function groupActivityItems(items: readonly TheaterActivityItem[]) {
  const groups: { day: string; items: TheaterActivityItem[] }[] = []
  for (const item of items) {
    const day = activityObservationDay(item.detected_at)
    const last = groups.at(-1)
    if (last?.day === day) last.items.push(item)
    else groups.push({ day, items: [item] })
  }
  return groups
}

export function activityShowtimesTarget(
  item: TheaterActivityItem,
  theaterId: string,
): string | null {
  if (!item.has_upcoming_showtimes || !item.next_showtime_date) return null
  if (!isCalendarDate(item.next_showtime_date)) return null
  return `${cinemaMovieTarget(item.movie.slug, theaterId)}&date=${item.next_showtime_date}#schedule-heading`
}
