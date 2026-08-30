const PARIS_TIMEZONE = 'Europe/Paris'

export function todayInParis(): string {
  const parts = new Intl.DateTimeFormat('fr-FR', {
    timeZone: PARIS_TIMEZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).formatToParts(new Date())

  const value = (type: Intl.DateTimeFormatPartTypes) => parts.find((part) => part.type === type)?.value ?? ''
  return `${value('year')}-${value('month')}-${value('day')}`
}

export function formatParisTime(isoDate: string): string {
  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: PARIS_TIMEZONE,
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).format(new Date(isoDate)).replace(':', 'h')
}

export function formatLongDate(date: string): string {
  const [year, month, day] = date.split('-').map(Number)
  if (!year || !month || !day) return date

  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: PARIS_TIMEZONE,
    weekday: 'long',
    day: 'numeric',
    month: 'long'
  }).format(new Date(Date.UTC(year, month - 1, day, 12)))
}

export function addCalendarDays(date: string, days: number): string {
  const [year, month, day] = date.split('-').map(Number)
  if (!year || !month || !day) return ''

  const nextDate = new Date(Date.UTC(year, month - 1, day + days, 12))
  return [nextDate.getUTCFullYear(), String(nextDate.getUTCMonth() + 1).padStart(2, '0'), String(nextDate.getUTCDate()).padStart(2, '0')].join('-')
}

export function isCalendarDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day, 12))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day
}

export function dateFromCalendarDate(value: string): Date | null {
  if (!isCalendarDate(value)) return null
  const [year = 0, month = 0, day = 0] = value.split('-').map(Number)
  return new Date(year, month - 1, day, 12)
}

export function calendarDateFromDate(value: Date): string {
  if (Number.isNaN(value.getTime())) return ''
  return [value.getFullYear(), String(value.getMonth() + 1).padStart(2, '0'), String(value.getDate()).padStart(2, '0')].join('-')
}

export function formatShortCalendarDate(value: string): string {
  if (!isCalendarDate(value)) return value
  const [year = '', month = '', day = ''] = value.split('-')
  return `${day}-${month}-${year.slice(-2)}`
}

export function formatDateLabel(date: string, referenceDate = todayInParis()): string {
  if (date === referenceDate) return 'Aujourd’hui'
  if (date === addCalendarDays(referenceDate, 1)) return 'Demain'

  const [year, month, day] = date.split('-').map(Number)
  if (!year || !month || !day) return date

  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: PARIS_TIMEZONE,
    weekday: 'short',
    day: 'numeric'
  }).format(new Date(Date.UTC(year, month - 1, day, 12)))
}

export function createServiceTimeOptions(): Array<{ value: string; label: string }> {
  const options: Array<{ value: string; label: string }> = []
  for (let minutes = 8 * 60; minutes < 24 * 60; minutes += 15) {
    const hour = Math.floor(minutes / 60)
    const minute = minutes % 60
    const value = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
    options.push({ value, label: value })
  }
  for (let minutes = 0; minutes <= 2 * 60; minutes += 15) {
    const hour = Math.floor(minutes / 60)
    const minute = minutes % 60
    const value = `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
    options.push({ value, label: `${value} (lendemain)` })
  }
  return options
}
