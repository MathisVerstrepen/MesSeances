export type Language = 'ALL' | 'VOSTFR' | 'VF'

export interface Movie {
  slug: string
  title: string
  runtime_minutes: number
}

export interface Showtime {
  id: string
  movie: Movie
  start_time: string
  end_time: string
  language: Exclude<Language, 'ALL'>
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
  language?: Language
}

export interface SlotQuery {
  city: string
  date: string
  start_after: string
  finish_before: string
  buffer_ads?: number
  language?: Language
}
