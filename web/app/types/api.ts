export type QueryLanguage = 'ALL' | 'VOSTFR' | 'VF'
export type ShowtimeLanguage = '' | 'VOSTFR' | 'VF' | 'VO' | 'VF_SME' | 'VFSTF'
export type ShowtimeFormat = '2D' | '3D' | 'IMAX' | 'DOLBY' | 'SCREENX' | 'LASER_ULTRA' | '4DX' | 'ICE'
export type QueryFormat = 'ALL' | ShowtimeFormat
export type Provider = 'ugc' | 'kinepolis' | 'pathe' | 'cgr' | 'megarama' | 'cineville' | 'mk2' | 'cinewest' | 'grandecran' | 'noecinemas'
export type MovieSort = 'title_asc' | 'title_desc' | 'release_date_desc' | 'runtime_asc' | 'runtime_desc' | 'showtimes_desc'
export type MovieDurationFilter = 'short' | 'medium' | 'long'

export type Language = QueryLanguage

export interface StatisticsQuery {
  date?: string
  date_to?: string
  city?: string[]
  theater?: string[]
  chain?: Provider
  language?: Exclude<ShowtimeLanguage, ''> | 'unknown'
  format?: ShowtimeFormat | 'unknown'
  genre?: string
  pass?: string
}

export interface StatisticsDateRange { from: string; through: string }
export interface StatisticsBucket { value: string; label: string; count: number }
export interface StatisticsMovieRank { slug: string; title: string; showtime_count: number; theater_count: number }
export interface StatisticsCityRank { slug: string; name: string; showtime_count: number; movie_count: number; theater_count: number }
export interface StatisticsTheaterRank { id: string; slug: string; name: string; city: string; city_slug: string; chain: Provider; showtime_count: number; movie_count: number }
export interface StatisticsHeatmapCell { weekday: number; hour: number; showtime_count: number }
export interface StatisticsOptions {
  cities: { slug: string; name: string }[]
  theaters: { id: string; slug: string; name: string; city: string; city_slug: string; chain: Provider; passes: string[] }[]
  chains: Provider[]
  languages: string[]
  formats: string[]
  genres: { value: string; label: string }[]
  passes: string[]
}
export interface StatisticsResponse {
  generated_at: string
  timezone: 'Europe/Paris'
  range: StatisticsDateRange
  coverage: { snapshot_window: StatisticsDateRange; intersection: StatisticsDateRange | null; completeness: 'unknown'; stale: boolean }
  options: StatisticsOptions
  totals: { showtimes: number; movies: number; theaters: number; cities: number }
  top_movies: { by_showtimes: StatisticsMovieRank[]; by_theaters: StatisticsMovieRank[] }
  heatmap: StatisticsHeatmapCell[]
  versions: StatisticsBucket[]
  formats: StatisticsBucket[]
  genres: StatisticsBucket[]
  runtimes: StatisticsBucket[]
  local: { cities: StatisticsCityRank[]; theaters: StatisticsTheaterRank[] }
  concentration: { top_movie_count: number; top_showtime_count: number; other_showtime_count: number }
}

export interface Movie {
  slug: string
  title: string
  runtime_minutes: number
  updated_at: string
}

export interface CatalogMovie extends Movie {
  poster_url: string | null
  tmdb_id: number | null
  imdb_id: string | null
  trailer_vf_youtube_key?: string | null
  trailer_vo_youtube_key?: string | null
  overview: string | null
  release_date: string | null
  french_release_date: string | null
  genres: string[]
  showtime_count?: number
}

export interface Showtime {
  provider: Provider
  id: string
  movie: Movie
  start_time: string
  end_time: string
  estimated_end_time: string | null
  estimated_end_ads_minutes: number | null
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

export const adminMovieFields = [
  'title',
  'runtime_minutes',
  'release_date',
  'genres',
  'overview',
  'poster_url',
  'backdrop_url',
  'trailer_vf_youtube_key',
  'trailer_vo_youtube_key'
] as const

export type AdminMovieField = typeof adminMovieFields[number]
export type AdminMovieOverrideStatus = 'all' | 'overridden' | 'automatic'
export type AdminMovieSort = 'title' | 'runtime_minutes' | 'release_date' | 'showtime_count' | 'updated_at' | 'id'
export type AdminMovieSortDirection = 'asc' | 'desc'

export interface AdminMovieMetadata {
  title: string
  runtime_minutes: number
  release_date: string | null
  genres: string[]
  overview: string | null
  poster_url: string | null
  backdrop_url: string | null
  trailer_vf_youtube_key: string | null
  trailer_vo_youtube_key: string | null
}

export interface AdminMovieItem {
  id: string
  updated_at: string
  showtime_count: number
  automatic: AdminMovieMetadata
  values: AdminMovieMetadata
  overridden_fields: AdminMovieField[]
}

export interface AdminMoviesResponse {
  items: AdminMovieItem[]
  total: number
  limit: number
  offset: number
}

export interface AdminMoviePoster {
  url: string
  width: number
  height: number
  language: string | null
}

export interface AdminMoviePostersResponse {
  posters: AdminMoviePoster[]
}

export interface AdminMoviesQuery {
  limit: number
  offset: number
  search?: string
  runtime_min?: number
  runtime_max?: number
  release_date_from?: string
  release_date_to?: string
  genre?: string
  override_status: AdminMovieOverrideStatus
  override_field?: AdminMovieField
  sort: AdminMovieSort
  direction: AdminMovieSortDirection
}

export type AdminMovieOverrideValues = Partial<AdminMovieMetadata>

export interface AdminMoviePatchRequest {
  expected_updated_at: string
  overrides?: AdminMovieOverrideValues
  restore?: AdminMovieField[]
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

export type AdminPendingMatchStatus = 'review_required' | 'unmatched' | 'rejected' | 'matched'
export type AdminPendingMatchesFilter = 'unresolved' | 'rejected' | 'matched'

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
  updated_at?: string
  current_match?: AdminTMDBCandidate
}

export interface AdminPendingMatchesResponse {
  items: AdminPendingMatch[]
  limit: number
  offset: number
}

export interface AdminPendingMatchesQuery {
  status: AdminPendingMatchesFilter
  limit: number
  offset: number
  search?: string
}

export interface AdminMatchDecisionResponse {
  status: 'matched' | 'rejected'
}

export interface AdminCorrectMatchRequest {
  tmdb_id: number
  expected_updated_at: string
}

export interface AdminTMDBRerunSummary {
  processed: number
  reused: number
  matched: number
  review_required: number
  unmatched: number
  failed: number
}

export interface AdminTMDBMetadataRefreshSummary {
  processed: number
  updated: number
  unchanged: number
  failed: number
}

export interface AdminTMDBMetadataRefreshRunningJob {
  state: 'running'
  started_at: string
  finished_at: null
  summary: null
}

export interface AdminTMDBMetadataRefreshSucceededJob {
  state: 'succeeded'
  started_at: string
  finished_at: string
  summary: AdminTMDBMetadataRefreshSummary
}

export interface AdminTMDBMetadataRefreshFailedJob {
  state: 'failed'
  started_at: string
  finished_at: string
  summary: null
  error_code: 'refresh_failed'
}

export type AdminTMDBMetadataRefreshJob =
  | AdminTMDBMetadataRefreshRunningJob
  | AdminTMDBMetadataRefreshSucceededJob
  | AdminTMDBMetadataRefreshFailedJob

export interface AdminTMDBMetadataRefreshResponse {
  job: AdminTMDBMetadataRefreshJob | null
}

export type UpcomingReviewReason = 'limited_only' | 'non_theatrical_before_or_same_day' | 'broadcaster_theatrical_note' | 'single_screening_note'
export type UpcomingReviewDecision = 'unreviewed' | 'approved' | 'excluded'
export type UpcomingReviewFilter = 'needs_review' | 'pending_assessment' | 'approved' | 'excluded' | 'all'

export interface FrenchReleaseRow {
  type: 1 | 2 | 3 | 4 | 5 | 6
  date: string
  note: string
}

export interface AdminUpcomingMovie {
  tmdb_id: number
  public_movie_id: string
  slug: string
  title: string
  poster_url: string | null
  french_release_date: string | null
  active: boolean
  in_window: boolean
  publicly_visible: boolean
  assessment_status: 'pending' | 'assessed'
  assessed_at: string | null
  french_releases: FrenchReleaseRow[]
  reason_codes: UpcomingReviewReason[]
  decision: UpcomingReviewDecision
  revision: number
}

export interface AdminUpcomingMoviesQuery {
  filter?: UpcomingReviewFilter
  search?: string
  limit?: number
  offset?: number
}

export interface AdminUpcomingMoviesResponse {
  items: AdminUpcomingMovie[]
  total: number
  limit: number
  offset: number
}

export interface AdminSetUpcomingDecisionRequest {
  decision: UpcomingReviewDecision
  expected_revision: number
}

export interface AdminUpcomingSyncResponse {
  job: {
    state: 'running' | 'succeeded' | 'failed'
    started_at: string
    finished_at: string | null
    error_code?: 'sync_failed'
  } | null
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

export interface AdminAddLocalMovieMembersRequest {
  members: AdminLocalMovieSource[]
}

export interface AdminAddLocalMovieMembersResponse {
  status: 'members_added'
  local_movie_id: string
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
export type AdminSyncScheduleTarget = Provider | 'tmdb_metadata_refresh' | 'tmdb_upcoming_movies'

export interface AdminSyncOccurrence {
  schedule_id: string
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
  id: string
  target: AdminSyncScheduleTarget
  revision: number
  enabled: boolean
  schedule: AdminSyncSchedule
  next_runs: string[]
  updated_at: string
}

export interface AdminSyncSchedulesResponse {
  timezone: 'Europe/Paris'
  available_targets: AdminSyncScheduleTarget[]
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
  latitude?: number | null
  longitude?: number | null
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

export interface UpcomingMoviesQuery {
  page?: number
}

export type UpcomingCatalogMovie = CatalogMovie & { french_release_date: string }

export interface UpcomingMoviesResponse {
  generated_at: string
  catalog_revision: string
  timezone: 'Europe/Paris'
  window: { from: string; through: string }
  items: UpcomingCatalogMovie[]
  page: number
  total: number
  total_weeks: number
  total_pages: number
}

export interface MovieShowtimesResponse {
  release_status: 'upcoming' | 'showing' | 'ended' | 'unavailable'
  movie: CatalogMovie
  backdrop_url: string | null
  date: string
  currently_screened: boolean
  available_dates: string[]
  theaters: MovieShowtimesTheater[]
}

export interface MovieShowtimesBundleResponse {
  scoped: MovieShowtimesResponse
  nationwide: MovieShowtimesResponse
}
