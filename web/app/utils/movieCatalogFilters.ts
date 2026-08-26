export const MOVIE_DURATION_FILTERS = ['short', 'medium', 'long'] as const
export const MOVIE_DATE_PRESETS = ['today', 'tomorrow', 'weekend'] as const

export type MovieDurationFilter = typeof MOVIE_DURATION_FILTERS[number]
export type MovieDatePreset = typeof MOVIE_DATE_PRESETS[number]
export type MovieDateMode = 'none' | MovieDatePreset | 'custom' | 'range'

export interface MovieCatalogFilters {
  genres: string[]
  duration?: MovieDurationFilter
  date?: MovieDatePreset | string
  dateTo?: string
}

export interface MovieCatalogFilterDraft {
  genres: string[]
  duration: MovieDurationFilter | ''
  dateMode: MovieDateMode
  customDate: string
  rangeStart: string
  rangeEnd: string
}

interface SerializedMovieCatalogFilters {
  genres: string | undefined
  duration: MovieDurationFilter | undefined
  date: string | undefined
  date_to: string | undefined
}

type QueryValue = string | null | undefined | (string | null)[]

const genreCollator = new Intl.Collator('fr-FR', { sensitivity: 'base' })

function scalarQueryValue(value: QueryValue): string | undefined {
  if (value === null || value === undefined || Array.isArray(value)) return undefined
  return value
}

function durationFilter(value: string | undefined): MovieDurationFilter | undefined {
  switch (value) {
    case 'short':
    case 'medium':
    case 'long':
      return value
    default:
      return undefined
  }
}

function datePreset(value: string | undefined): MovieDatePreset | undefined {
  switch (value) {
    case 'today':
    case 'tomorrow':
    case 'weekend':
      return value
    default:
      return undefined
  }
}

export function normalizeMovieGenres(genres: readonly string[]): string[] {
  const unique = new Map<string, string>()
  for (const genre of genres) {
    const normalized = genre.trim()
    if (!normalized) continue
    const key = normalized.toLocaleLowerCase('fr-FR')
    if (!unique.has(key)) unique.set(key, normalized)
  }
  return [...unique.values()].sort((left, right) => genreCollator.compare(left, right) || left.localeCompare(right))
}

function genresFromQuery(value: QueryValue): string[] {
  const raw = scalarQueryValue(value)
  if (!raw) return []
  const genres = raw.split(',')
  if (genres.some((genre) => !genre.trim())) return []
  return normalizeMovieGenres(genres)
}

export function isCalendarDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day, 12))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day
}

function isCurrentOrFutureDate(value: string, today: string): boolean {
  return isCalendarDate(value) && value >= today
}

export function parseMovieCatalogFilters(
  query: Readonly<Record<string, QueryValue>>,
  today: string
): MovieCatalogFilters {
  const filters: MovieCatalogFilters = { genres: genresFromQuery(query.genres) }
  const duration = durationFilter(scalarQueryValue(query.duration))
  if (duration) filters.duration = duration

  const date = scalarQueryValue(query.date)
  const preset = datePreset(date)
  if (preset) {
    filters.date = preset
    return filters
  }
  if (!date || !isCurrentOrFutureDate(date, today)) return filters

  filters.date = date
  const dateTo = scalarQueryValue(query.date_to)
  if (dateTo && isCurrentOrFutureDate(dateTo, today) && dateTo > date) filters.dateTo = dateTo
  return filters
}

export function serializeMovieCatalogFilters(filters: MovieCatalogFilters): SerializedMovieCatalogFilters {
  const genres = normalizeMovieGenres(filters.genres)
  return {
    genres: genres.length ? genres.join(',') : undefined,
    duration: filters.duration,
    date: filters.date,
    date_to: filters.dateTo
  }
}

export function movieCatalogFilterDraft(filters: MovieCatalogFilters, today: string): MovieCatalogFilterDraft {
  const base: MovieCatalogFilterDraft = {
    genres: normalizeMovieGenres(filters.genres),
    duration: filters.duration ?? '',
    dateMode: 'none',
    customDate: today,
    rangeStart: today,
    rangeEnd: addMovieCatalogDays(today, 1)
  }
  const preset = datePreset(filters.date)
  if (preset) {
    base.dateMode = preset
  } else if (filters.date && isCalendarDate(filters.date)) {
    base.dateMode = filters.dateTo ? 'range' : 'custom'
    base.customDate = filters.date
    base.rangeStart = filters.date
    base.rangeEnd = filters.dateTo ?? filters.date
  }
  return base
}

export function movieCatalogDraftError(draft: MovieCatalogFilterDraft, today: string): string {
  if (draft.dateMode === 'custom') {
    if (!isCalendarDate(draft.customDate)) return 'Saisissez une date valide au format dd-MM-yy.'
    if (draft.customDate < today) return 'Choisissez aujourd’hui ou une date ultérieure.'
  }
  if (draft.dateMode === 'range') {
    if (!isCalendarDate(draft.rangeStart) || !isCalendarDate(draft.rangeEnd)) return 'Saisissez deux dates valides au format dd-MM-yy.'
    if (draft.rangeStart < today || draft.rangeEnd < today) return 'Choisissez aujourd’hui ou des dates ultérieures.'
    if (draft.rangeEnd < draft.rangeStart) return 'La date de fin doit être égale ou postérieure à la date de début.'
  }
  return ''
}

export function movieCatalogFiltersFromDraft(draft: MovieCatalogFilterDraft, today: string): MovieCatalogFilters | null {
  if (movieCatalogDraftError(draft, today)) return null
  const filters: MovieCatalogFilters = {
    genres: normalizeMovieGenres(draft.genres),
    duration: draft.duration || undefined
  }
  const preset = datePreset(draft.dateMode)
  if (preset) {
    filters.date = preset
  } else if (draft.dateMode === 'custom') {
    filters.date = draft.customDate
  } else if (draft.dateMode === 'range') {
    filters.date = draft.rangeStart
    if (draft.rangeEnd > draft.rangeStart) filters.dateTo = draft.rangeEnd
  }
  return filters
}

export function hasMovieCatalogFilters(filters: MovieCatalogFilters): boolean {
  return Boolean(filters.genres.length || filters.duration || filters.date)
}

export function movieCatalogFiltersKey(filters: MovieCatalogFilters): string {
  const values = serializeMovieCatalogFilters(filters)
  return [values.genres, values.duration, values.date, values.date_to].map((value) => value ?? '').join('|')
}

export function dateFromCalendarDate(value: string): Date | null {
  if (!isCalendarDate(value)) return null
  const [year = 0, month = 0, day = 0] = value.split('-').map(Number)
  return new Date(year, month - 1, day, 12)
}

export function calendarDateFromDate(value: Date): string {
  if (Number.isNaN(value.getTime())) return ''
  return [value.getFullYear(), String(value.getMonth() + 1).padStart(2, '0'), String(value.getDate()).padStart(2, '0')].join('-')
}

export function formatShortCalendarDate(value: string): string {
  if (!isCalendarDate(value)) return value
  const [year = '', month = '', day = ''] = value.split('-')
  return `${day}-${month}-${year.slice(-2)}`
}

export function addMovieCatalogDays(value: string, days: number): string {
  if (!isCalendarDate(value)) return value
  const [year = 0, month = 0, day = 0] = value.split('-').map(Number)
  const next = new Date(Date.UTC(year, month - 1, day + days, 12))
  return [next.getUTCFullYear(), String(next.getUTCMonth() + 1).padStart(2, '0'), String(next.getUTCDate()).padStart(2, '0')].join('-')
}
