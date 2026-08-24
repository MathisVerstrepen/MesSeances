import type { Provider, ShowtimeFormat, ShowtimeLanguage } from './api'

export type ResultGrouping = 'movie' | 'chronological'
export type ResultLayout = 'lines' | 'boxes'
export type ShowtimeResultScope = 'multi-theater' | 'single-theater'

export interface ShowtimeResultViewModel {
  key: string
  showtimeId: string
  provider: Provider
  movieKey: string
  movieSlug: string
  movieTitle: string
  movieRuntimeMinutes: number
  theaterName: string
  advertisedStartTime: string
  effectiveStartTime: string
  endTime: string
  language: ShowtimeLanguage
  format: ShowtimeFormat
  room: string
  bookingUrl: string | null
  posterUrl: string | null
  backdropUrl: string | null
}

export interface ShowtimeMovieResultGroup {
  key: string
  results: ShowtimeResultViewModel[]
}
