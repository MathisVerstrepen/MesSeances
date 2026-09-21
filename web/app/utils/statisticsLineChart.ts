import type { StatisticsDailyShowtimes } from '../types/api'

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

export function statisticsLineChart(rows: StatisticsDailyShowtimes[]) {
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
  const points = rows.map((row) => {
    const date = new Date(`${row.date}T00:00:00Z`)
    return {
      ...row,
      label: fullDate.format(date),
      shortLabel: shortDate.format(date),
      x:
        last === first ? 50 : ((date.getTime() - first) / (last - first)) * 100,
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
  return {
    ceiling,
    points,
    ticks,
    dateTicks,
    line: points.map((point) => `${point.x},${point.y}`).join(' '),
  }
}
