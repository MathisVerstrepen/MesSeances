import type { Showtime } from '../types/api.ts'

export type ResolvedShowtimeEnd = {
  time: string
  estimated: boolean
  adsMinutes: number | null
}
type ShowtimeTiming = Pick<
  Showtime,
  'start_time' | 'end_time' | 'estimated_end_time' | 'estimated_end_ads_minutes'
>

export function hasCanonicalShowtimeEnd(
  startTime: string,
  endTime: string,
): boolean {
  const start = Date.parse(startTime)
  const end = Date.parse(endTime)
  return Number.isFinite(start) && Number.isFinite(end) && end > start
}

// Consume explicit API provenance only. Runtime and advertising arithmetic belong to the API.
export function resolveShowtimeEnd(
  showtime: ShowtimeTiming,
): ResolvedShowtimeEnd | null {
  if (hasCanonicalShowtimeEnd(showtime.start_time, showtime.end_time)) {
    return { time: showtime.end_time, estimated: false, adsMinutes: null }
  }
  const start = Date.parse(showtime.start_time)
  const canonical = Date.parse(showtime.end_time)
  const ads = showtime.estimated_end_ads_minutes
  if (
    !Number.isFinite(start) ||
    canonical !== start ||
    showtime.estimated_end_time === null ||
    ads === null ||
    !Number.isInteger(ads) ||
    ads < 0 ||
    ads > 120 ||
    !hasCanonicalShowtimeEnd(showtime.start_time, showtime.estimated_end_time)
  )
    return null
  return { time: showtime.estimated_end_time, estimated: true, adsMinutes: ads }
}
