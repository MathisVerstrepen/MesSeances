import type { SlotResult, TheaterShowtimesResponse } from '../types/api'
import type { ResultGrouping, ResultLayout, ShowtimeMovieResultGroup, ShowtimeResultViewModel } from '../types/showtimeResults'

export const resultGroupingOptions: [{ value: ResultGrouping; label: string }, { value: ResultGrouping; label: string }] = [
  { value: 'movie', label: 'Par film' },
  { value: 'chronological', label: 'Chronologique' }
]

export const resultLayoutOptions: [{ value: ResultLayout; label: string }, { value: ResultLayout; label: string }] = [
  { value: 'lines', label: 'Lignes' },
  { value: 'boxes', label: 'Boîtes' }
]

export function toSlotShowtimeResults(results: readonly SlotResult[]): ShowtimeResultViewModel[] {
  return results.map((result) => ({
    key: `${result.showtime.provider}:${result.showtime.id}`,
    showtimeId: result.showtime.id,
    provider: result.showtime.provider,
    movieKey: `${result.showtime.provider}:${result.showtime.movie.slug}`,
    movieSlug: result.showtime.movie.slug,
    movieTitle: result.showtime.movie.title,
    movieRuntimeMinutes: result.showtime.movie.runtime_minutes,
    theaterName: result.theater.name,
    advertisedStartTime: result.showtime.start_time,
    effectiveStartTime: result.effective_start_time,
    endTime: result.showtime.end_time,
    language: result.showtime.language,
    format: result.showtime.format,
    room: result.showtime.room,
    bookingUrl: result.showtime.booking_url,
    posterUrl: result.poster_url,
    backdropUrl: result.backdrop_url
  }))
}

export function toTheaterShowtimeResults(response: TheaterShowtimesResponse): ShowtimeResultViewModel[] {
  return response.showtimes.map((showtime) => ({
    key: `${showtime.provider}:${showtime.id}`,
    showtimeId: showtime.id,
    provider: showtime.provider,
    movieKey: `${showtime.provider}:${showtime.movie.slug}`,
    movieSlug: showtime.movie.slug,
    movieTitle: showtime.movie.title,
    movieRuntimeMinutes: showtime.movie.runtime_minutes,
    theaterName: response.theater.name,
    advertisedStartTime: showtime.start_time,
    effectiveStartTime: showtime.start_time,
    endTime: showtime.end_time,
    language: showtime.language,
    format: showtime.format,
    room: showtime.room,
    bookingUrl: showtime.booking_url,
    posterUrl: showtime.poster_url,
    backdropUrl: showtime.backdrop_url
  }))
}

export function sortShowtimeResults(results: readonly ShowtimeResultViewModel[]): ShowtimeResultViewModel[] {
  return [...results].sort((first, second) => {
    const timeDifference = Date.parse(first.advertisedStartTime) - Date.parse(second.advertisedStartTime)
    return timeDifference || first.showtimeId.localeCompare(second.showtimeId)
  })
}

export function groupShowtimeResults(results: readonly ShowtimeResultViewModel[]): ShowtimeMovieResultGroup[] {
  const groups = new Map<string, ShowtimeResultViewModel[]>()
  for (const result of results) {
    const group = groups.get(result.movieKey)
    if (group) group.push(result)
    else groups.set(result.movieKey, [result])
  }
  return [...groups.entries()].map(([key, groupedResults]) => ({ key, results: groupedResults }))
}
