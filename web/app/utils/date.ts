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
