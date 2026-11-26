export type ShowtimeTimingState = 'upcoming' | 'warning' | 'past'

/** A showtime stays "started" (warning) this long before it counts as past. */
export const SHOWTIME_WARNING_DURATION_MS = 20 * 60 * 1000

export function showtimeTimingState(
  startTime: string,
  now: number | null,
): ShowtimeTimingState {
  if (now === null) return 'upcoming'

  const start = Date.parse(startTime)
  if (!Number.isFinite(start)) return 'upcoming'
  if (now > start + SHOWTIME_WARNING_DURATION_MS) return 'past'
  if (now >= start) return 'warning'
  return 'upcoming'
}

/** Accessible-name suffix appended to a showtime label, e.g. ", séance passée". */
export function showtimeTimingSuffix(state: ShowtimeTimingState): string {
  if (state === 'warning') return ', séance commencée'
  if (state === 'past') return ', séance passée'
  return ''
}

/** Earliest future instant at which one of the showtimes changes timing state. */
export function nextShowtimeTimingTransition(
  startTimes: Iterable<string>,
  now: number,
): number | null {
  let next = Number.POSITIVE_INFINITY
  for (const startTime of startTimes) {
    const start = Date.parse(startTime)
    if (!Number.isFinite(start)) continue
    for (const transition of [
      start,
      start + SHOWTIME_WARNING_DURATION_MS + 1,
    ]) {
      if (transition > now && transition < next) next = transition
    }
  }
  return Number.isFinite(next) ? next : null
}
