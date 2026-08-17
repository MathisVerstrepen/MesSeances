export type QueryLanguage = 'ALL' | 'VOSTFR' | 'VF'
export type ShowtimeLanguage = 'VOSTFR' | 'VF' | 'VO' | 'VF_SME'
export type Provider = 'ugc' | 'kinepolis'

export type Language = QueryLanguage

export interface Movie {
  provider: Provider
  slug: string
  title: string
  runtime_minutes: number
}

export interface CatalogMovie extends Movie {
  poster_url: string | null
  tmdb_id: number | null
  overview: string | null
  release_date: string | null
  genres: string[]
}

export interface Showtime {
  provider: Provider
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
  poster_url: string | null
  backdrop_url: string | null
}

export interface TimelineTheater {
  provider: Provider
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
  provider: Provider
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

export interface AdminSessionResponse {
  authenticated: boolean
}

export interface AdminTMDBCandidate {
  id: number
  title: string
  original_title?: string
  runtime_minutes?: number
  score?: number
  poster_url?: string
  detail_url: string
}

export type AdminPendingMatchStatus = 'review_required' | 'unmatched'

export interface AdminPendingMatch {
  source_movie_id: string
  source_title: string
  source_runtime_minutes: number
  source_poster_url?: string
  source_detail_url: string
  status: AdminPendingMatchStatus
  candidates: AdminTMDBCandidate[]
  evaluated_at: string
}

export interface AdminPendingMatchesResponse {
  items: AdminPendingMatch[]
  limit: number
  offset: number
}

export interface AdminMatchDecisionResponse {
  status: 'matched' | 'rejected'
}

export type AdminSyncTarget = 'all' | Provider
export type AdminSyncState = 'running' | 'succeeded' | 'failed'
export type AdminSyncProviderState = 'not_requested' | 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped'

export interface AdminSyncProviderStatus {
  state: AdminSyncProviderState
}

export interface AdminSyncJob {
  id: string
  target: AdminSyncTarget
  state: AdminSyncState
  started_at: string
  finished_at: string | null
  from: string
  through: string
  providers: Record<Provider, AdminSyncProviderStatus>
}

export interface AdminSyncResponse {
  job: AdminSyncJob | null
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
  provider: Provider
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
  provider: Provider
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
