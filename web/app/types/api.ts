export type QueryLanguage = 'ALL' | 'VOSTFR' | 'VF'
export type ShowtimeLanguage = 'VOSTFR' | 'VF' | 'VO' | 'VF_SME'
export type ShowtimeFormat = '2D' | '3D' | 'IMAX' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX' | 'ICE'
export type QueryFormat = 'ALL' | ShowtimeFormat
export type Provider = 'ugc' | 'kinepolis' | 'pathe' | 'cgr'
export type MovieSort = 'title_asc' | 'title_desc' | 'release_date_desc' | 'runtime_asc' | 'runtime_desc' | 'showtimes_desc'
export type MovieDurationFilter = 'short' | 'medium' | 'long'

export type Language = QueryLanguage

export interface Movie {
  slug: string
  title: string
  runtime_minutes: number
  updated_at: string
}

export interface CatalogMovie extends Movie {
  poster_url: string | null
  tmdb_id: number | null
  overview: string | null
  release_date: string | null
  genres: string[]
  showtime_count?: number
}

export interface Showtime {
  provider: Provider
  id: string
  movie: Movie
  start_time: string
  end_time: string
  language: ShowtimeLanguage
  format: ShowtimeFormat
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
  poster_url: string | null
  backdrop_url: string | null
  effective_start_time: string
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

export interface ShortLinkResponse {
  code: string
  target: string
}

export interface AdminSessionResponse {
  authenticated: boolean
}

export type AdminTheaterLocationStatus = 'ambiguous' | 'not_found'

export interface AdminTheaterLocationSuggestion {
  label: string
  score: number
  latitude: number | null
  longitude: number | null
  postal_code: string | null
  city: string | null
  type: string | null
}

export interface AdminTheaterLocation {
  provider: Provider
  provider_theater_id: string
  theater_id: string
  name: string
  address: string
  postal_code: string
  city: string
  status: AdminTheaterLocationStatus
  updated_at: string
  suggestion: AdminTheaterLocationSuggestion | null
  can_accept_suggestion: boolean
}

export interface AdminTheaterLocationsResponse {
  items: AdminTheaterLocation[]
  limit: number
  offset: number
}

export interface AdminAcceptTheaterLocationSuggestionRequest {
  expected_updated_at: string
}

export interface AdminSetManualTheaterLocationRequest {
  expected_updated_at: string
  latitude: number
  longitude: number
}

export interface AdminTheaterLocationResolutionResponse {
  status: 'manual'
}

export type AdminTheaterGeocodingState = 'running' | 'succeeded' | 'failed'
export type AdminTheaterGeocodingFailureCode = 'run_failed' | 'canceled' | 'internal_failure'

export interface AdminTheaterGeocodingSummary {
  selected: number
  skipped: number
  matched: number
  ambiguous: number
  not_found: number
  failed: number
  written: number
}

export interface AdminTheaterGeocodingJob {
  id: string
  state: AdminTheaterGeocodingState
  started_at: string
  finished_at: string | null
  summary: AdminTheaterGeocodingSummary | null
  error_code: AdminTheaterGeocodingFailureCode | null
}

export interface AdminTheaterGeocodingResponse {
  job: AdminTheaterGeocodingJob | null
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

export type AdminPendingMatchStatus = 'review_required' | 'unmatched' | 'rejected'
export type AdminPendingMatchesFilter = 'unresolved' | 'rejected'

export interface AdminPendingMatch {
  source_provider: Provider
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

export interface AdminTMDBRerunSummary {
  processed: number
  reused: number
  matched: number
  review_required: number
  unmatched: number
  failed: number
}

export interface AdminLocalMovieSource {
  source_provider: Provider
  source_movie_id: string
}

export interface AdminLocalMovieMember extends AdminLocalMovieSource {
  available: boolean
  source_title: string | null
  source_runtime_minutes: number | null
  source_poster_url: string | null
}

export interface AdminLocalMovieGroup {
  local_movie_id: string
  primary: AdminLocalMovieSource
  metadata_source: AdminLocalMovieSource | null
  members: AdminLocalMovieMember[]
}

export interface AdminLocalMovieGroupsResponse {
  items: AdminLocalMovieGroup[]
  limit: number
  offset: number
}

export interface AdminCreateLocalMovieGroupRequest {
  members: AdminLocalMovieSource[]
  primary: AdminLocalMovieSource
}

export interface AdminUnmergeLocalMovieResponse {
  status: 'unmerged'
  local_movie_id: string
}

export type AdminSyncTarget = 'all' | Provider
export type AdminSyncState = 'running' | 'succeeded' | 'failed'
export type AdminSyncTrigger = 'manual' | 'scheduled'
export type AdminSyncProviderState = 'not_requested' | 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped'
export type AdminSyncFailureCode = 'none' | 'client_creation_failed' | 'provider_sync_failed' | 'dataset_rejected' | 'replacement_failed' | 'canceled' | 'internal_failure'
export type AdminSyncEnrichmentState = 'skipped' | 'complete' | 'degraded'
export type AdminSyncScheduleKind = 'daily' | 'weekly' | 'cron'
export type AdminSyncWeekday = 'mon' | 'tue' | 'wed' | 'thu' | 'fri' | 'sat' | 'sun'

export interface AdminSyncOccurrence {
  schedule_revision: number
  scheduled_for: string
  attempt: number
}

export interface AdminDailySyncSchedule {
  kind: 'daily'
  time: string
}

export interface AdminWeeklySyncSchedule {
  kind: 'weekly'
  time: string
  weekdays: AdminSyncWeekday[]
}

export interface AdminCronSyncSchedule {
  kind: 'cron'
  expression: string
}

export type AdminSyncSchedule = AdminDailySyncSchedule | AdminWeeklySyncSchedule | AdminCronSyncSchedule

export interface AdminSyncScheduleItem {
  provider: Provider
  revision: number
  enabled: boolean
  schedule: AdminSyncSchedule
  next_runs: string[]
  updated_at: string
}

export interface AdminSyncSchedulesResponse {
  timezone: 'Europe/Paris'
  schedules: AdminSyncScheduleItem[]
}

export interface AdminSaveSyncScheduleRequest {
  enabled: boolean
  schedule: AdminSyncSchedule
}

export interface AdminSyncMetrics {
  version: number
  cinemas: number
  movies: number
  new_movies: number
  dates?: number
  requests?: number
  showtimes: number
  new_showtimes: number
  skipped?: number
  generated_at: string
}

export interface AdminSyncEnrichmentCounts {
  reused: number
  matched: number
  review_required: number
  unmatched: number
  failed: number
}

export interface AdminSyncEnrichmentOutcome {
  status: AdminSyncEnrichmentState
  counts?: AdminSyncEnrichmentCounts
}

export interface AdminSyncProviderOutcome {
  sync: AdminSyncMetrics
  enrichment: AdminSyncEnrichmentOutcome
}

export interface AdminSyncProviderStatus {
  state: AdminSyncProviderState
  error_code?: AdminSyncFailureCode
  log?: string[]
  outcome?: AdminSyncProviderOutcome
}

export interface AdminSyncJob {
  id: string
  target: AdminSyncTarget
  state: AdminSyncState
  trigger: AdminSyncTrigger
  occurrence?: AdminSyncOccurrence
  started_at: string
  finished_at: string | null
  from: string
  through: string
  providers: Record<Provider, AdminSyncProviderStatus>
}

export interface AdminSyncResponse {
  job: AdminSyncJob | null
  runs: AdminSyncJob[]
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
  include_ads?: boolean
  language?: QueryLanguage
  format?: QueryFormat
}

export interface Theater {
  provider: Provider
  id: string
  slug: string
  name: string
  address: string
  city: string
  city_slug: string
  postal_code: string
  available_dates: string[]
  accepted_passes: string[]
}

export interface City {
  name: string
  slug: string
}

export interface CityTheater {
  provider: Provider
  id: string
  slug: string
  name: string
}

export interface CityInventoryItem extends City {
  theaters: CityTheater[]
}

export interface CitiesResponse {
  generated_at: string
  items: CityInventoryItem[]
}

export interface CityDetailResponse {
  generated_at: string
  city: City
  theaters: Theater[]
  movies: CatalogMovie[]
}

export interface TheaterShowtimesResponse {
  generated_at: string
  timezone: 'Europe/Paris'
  theater: Theater
  date: string | null
  showtimes: TimelineShowtime[]
}

export interface TheaterQuery {
  city?: string
  chain?: string
}

export interface MoviesQuery {
  currently_screened?: boolean
  include_ended?: boolean
  theaters?: string
  search?: string
  genres?: string
  duration?: MovieDurationFilter
  date?: string
  date_to?: string
  sort?: MovieSort
  page?: number
  page_size?: number
}

export interface MoviesResponse {
  items: CatalogMovie[]
  available_genres: string[]
  page: number
  page_size: number
  total: number
  generated_at: string
  catalog_revision: string
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
  city_slug: string
  showtimes: Showtime[]
}

export interface MovieShowtimesResponse {
  movie: CatalogMovie
  backdrop_url: string | null
  date: string
  currently_screened: boolean
  available_dates: string[]
  theaters: MovieShowtimesTheater[]
}
