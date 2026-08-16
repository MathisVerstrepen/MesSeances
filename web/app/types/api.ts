export type QueryLanguage = 'ALL' | 'VOSTFR' | 'VF'
export type ShowtimeLanguage = 'VOSTFR' | 'VF' | 'VO' | 'VF_SME'

export type Language = QueryLanguage

export interface Movie {
  slug: string
  title: string
  runtime_minutes: number
}

export interface CatalogMovie extends Movie {
  poster_url: string | null
}

export interface Showtime {
  id: string
  movie: Movie
  start_time: string
  end_time: string
  language: ShowtimeLanguage
  format: string
  room: string
  booking_url: string | null
}

export interface TimelineShowtime extends Showtime {
  start_offset_minutes: number
  duration_minutes: number
}

export interface TimelineTheater {
  id: string
  slug: string
  name: string
  city: string
  accepted_passes: string[]
  showtimes: TimelineShowtime[]
}

export interface TimelineResponse {
  date: string
  timezone: 'Europe/Paris'
  window_start_time: string
  window_end_time: string
  theaters: TimelineTheater[]
}

export interface SlotTheater {
  id: string
  name: string
  city: string
}

export interface SlotResult {
  showtime: Showtime
  theater: SlotTheater
  effective_end_time: string
  buffer_ads_minutes: number
  slack_before_minutes: number
  slack_after_minutes: number
}

export interface ApiErrorResponse {
  error: {
    code: string
    message: string
  }
}

export interface TimelineQuery {
  date: string
  theaters?: string
  language?: QueryLanguage
}

export interface SlotQuery {
  city?: string
  theaters?: string
  date: string
  start_after: string
  finish_before: string
  buffer_ads?: number
  language?: QueryLanguage
}

export interface Theater {
  id: string
  slug: string
  name: string
  address: string
  city: string
  postal_code: string
  available_dates: string[]
  accepted_passes: string[]
}

export interface TheaterQuery {
  city?: string
  chain?: string
}

export interface MoviesQuery {
  currently_screened?: boolean
  search?: string
  page?: number
  page_size?: number
}

export interface MoviesResponse {
  items: CatalogMovie[]
  page: number
  page_size: number
  total: number
}

export interface MovieShowtimesQuery {
  date: string
  city?: string
  theaters?: string
}

export interface MovieShowtimesTheater {
  id: string
  slug: string
  name: string
  city: string
  showtimes: Showtime[]
}

export interface MovieShowtimesResponse {
  movie: CatalogMovie
  date: string
  theaters: MovieShowtimesTheater[]
}
