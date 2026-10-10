import type { StatisticsDailyShowtimes } from '../types/api'
import { isCalendarDate } from './date.ts'
import { releaseWeekStart } from './upcomingMovies.ts'

const day = 86_400_000
export interface StatisticsChartOptions {
  today?: string
  film?: boolean
  frenchReleaseDate?: string | null
}
export interface StatisticsWeekMarker {
  date: string
  x: number
  label: string
  description: string
}

const shortDate = new Intl.DateTimeFormat('fr-FR', {
  timeZone: 'UTC',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
})
const fullDate = new Intl.DateTimeFormat('fr-FR', {
  timeZone: 'UTC',
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

export function statisticsLineChart(
  rows: StatisticsDailyShowtimes[],
  options: StatisticsChartOptions = {},
) {
  const maximum = rows.reduce(
    (value, row) => Math.max(value, row.showtime_count),
    0,
  )
  const step = Math.max(1, Math.ceil(maximum / 4))
  const intervals = Math.max(1, Math.ceil(maximum / step))
  const ceiling = intervals * step
  // Service dates are calendar days, not instants in the viewer's timezone.
  const first = Date.parse(`${rows[0]?.date ?? '1970-01-01'}T00:00:00Z`)
  const last = Date.parse(`${rows.at(-1)?.date ?? '1970-01-01'}T00:00:00Z`)
  const position = (time: number) =>
    last === first ? 50 : ((time - first) / (last - first)) * 100
  const points = rows.map((row) => {
    const date = new Date(`${row.date}T00:00:00Z`)
    return {
      ...row,
      label: fullDate.format(date),
      shortLabel: shortDate.format(date),
      x: position(date.getTime()),
      y: (1 - row.showtime_count / ceiling) * 100,
    }
  })
  const ticks = Array.from({ length: intervals + 1 }, (_, index) => ({
    count: ceiling - index * step,
    y: (index / intervals) * 100,
  }))
  const dateTicks = points.filter(
    (_, index) =>
      index === 0 ||
      index === points.length - 1 ||
      (points.length > 2 && index === Math.floor((points.length - 1) / 2)),
  )
  const todayTime =
    options.today && isCalendarDate(options.today)
      ? Date.parse(`${options.today}T00:00:00Z`)
      : NaN
  const today =
    rows.length && todayTime >= first && todayTime <= last
      ? {
          date: options.today!,
          x: position(todayTime),
          label: fullDate.format(todayTime),
        }
      : null
  const weeks: StatisticsWeekMarker[] = []
  if (options.film && rows.length) {
    const release =
      options.frenchReleaseDate && isCalendarDate(options.frenchReleaseDate)
        ? Date.parse(`${releaseWeekStart(options.frenchReleaseDate)}T00:00:00Z`)
        : null
    const firstWednesday =
      first + ((3 - new Date(first).getUTCDay() + 7) % 7) * day
    for (let time = firstWednesday; time <= last; time += 7 * day) {
      const number = release === null ? null : (time - release) / (7 * day) + 1
      const label =
        number !== null && number > 0 ? `Semaine ${number}` : 'Mercredi'
      weeks.push({
        date: new Date(time).toISOString().slice(0, 10),
        x: position(time),
        label,
        description: `${label} : ${fullDate.format(time)}`,
      })
    }
  }
  return {
    ceiling,
    points,
    ticks,
    dateTicks,
    today,
    weeks,
    line: points.map((point) => `${point.x},${point.y}`).join(' '),
  }
}

// Keep every separator; only its visual label is optional in dense histories.
export function statisticsWeekLabels(
  markers: StatisticsWeekMarker[],
  width: number,
) {
  let previousRight = -Infinity
  return markers.flatMap((marker) => {
    const labelWidth = marker.label.length * 7 + 8
    if (labelWidth > width) return []
    const center = Math.max(
      labelWidth / 2,
      Math.min(width - labelWidth / 2, (marker.x * width) / 100),
    )
    const left = center - labelWidth / 2
    if (left < previousRight + 12) return []
    previousRight = center + labelWidth / 2
    return [{ ...marker, labelX: (center / width) * 100 }]
  })
}

export function statisticsNearestPoint(points: { x: number }[], x: number) {
  return points.reduce(
    (nearest, point, index) =>
      Math.abs(point.x - x) < Math.abs(points[nearest]!.x - x)
        ? index
        : nearest,
    0,
  )
}
