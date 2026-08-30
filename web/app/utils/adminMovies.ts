import {
  adminMovieFields,
  type AdminMovieField,
  type AdminMovieItem,
  type AdminMovieMetadata,
  type AdminMovieOverrideStatus,
  type AdminMoviePatchRequest,
  type AdminMoviesQuery,
  type AdminMovieSort,
  type AdminMovieSortDirection
} from '../types/api.ts'

export const ADMIN_MOVIE_PAGE_SIZE = 50
export const ADMIN_MOVIE_ROUTE_KEYS = ['q', 'runtime_min', 'runtime_max', 'release_date_from', 'release_date_to', 'genre', 'override_status', 'override_field', 'sort', 'direction', 'page'] as const

export const adminMovieFieldLabels = {
  title: 'Titre',
  runtime_minutes: 'Durée',
  release_date: 'Date de sortie',
  genres: 'Genres',
  overview: 'Synopsis',
  poster_url: 'Affiche',
  backdrop_url: 'Arrière-plan',
  trailer_vf_youtube_key: 'Bande-annonce VF',
  trailer_vo_youtube_key: 'Bande-annonce VO'
} satisfies Record<AdminMovieField, string>

export type AdminMovieDraftValue = string | number | string[] | null
export type AdminMovieDraftOperation = { kind: 'override', value: AdminMovieDraftValue } | { kind: 'restore' }

export interface AdminMovieDraft {
  operations: Partial<Record<AdminMovieField, AdminMovieDraftOperation>>
  expected_updated_at?: string
}

export interface AdminMovieValidationErrors {
  title?: string
  runtime_minutes?: string
  release_date?: string
  genres?: string
  overview?: string
  poster_url?: string
  backdrop_url?: string
  trailer_vf_youtube_key?: string
  trailer_vo_youtube_key?: string
}

type RouteInputValue = string | string[] | null | undefined

export interface AdminMovieRouteQueryInput {
  q?: RouteInputValue
  runtime_min?: RouteInputValue
  runtime_max?: RouteInputValue
  release_date_from?: RouteInputValue
  release_date_to?: RouteInputValue
  genre?: RouteInputValue
  override_status?: RouteInputValue
  override_field?: RouteInputValue
  sort?: RouteInputValue
  direction?: RouteInputValue
  page?: RouteInputValue
}

export interface AdminMovieOwnedRouteQuery {
  q?: string
  runtime_min?: string
  runtime_max?: string
  release_date_from?: string
  release_date_to?: string
  genre?: string
  override_status?: string
  override_field?: string
  sort?: string
  direction?: string
  page?: string
}

export interface AdminMovieRouteState {
  q: string
  runtime_min?: number
  runtime_max?: number
  release_date_from?: string
  release_date_to?: string
  genre: string
  override_status: AdminMovieOverrideStatus
  override_field?: AdminMovieField
  sort: AdminMovieSort
  direction: AdminMovieSortDirection
  page: number
}

export interface AdminMovieGridSort {
  colId: string
  sort: 'asc' | 'desc'
}

export interface AdminMovieGridFilter {
  type?: string
  filter?: string | number | null
  filterTo?: string | number | null
  dateFrom?: string | null
  dateTo?: string | null
}

export interface AdminMovieGridRequest {
  startRow?: number
  endRow?: number
  sortModel?: AdminMovieGridSort[]
  filterModel?: AdminMovieGridFilterModel
}

export interface AdminMovieGridFilterModel {
  runtime_minutes?: AdminMovieGridFilter
  release_date?: AdminMovieGridFilter
  genres?: AdminMovieGridFilter
}

const adminMovieSorts = ['title', 'runtime_minutes', 'release_date', 'updated_at', 'id'] as const
const adminMovieDirections = ['asc', 'desc'] as const
const adminMovieOverrideStatuses = ['all', 'overridden', 'automatic'] as const
const maxRuntime = 2_147_483_647

export function emptyAdminMovieDraft(): AdminMovieDraft {
  return { operations: {} }
}

export function adminMovieDraftFingerprint(draft: AdminMovieDraft | undefined): string {
  if (!draft) return '[]'
  return JSON.stringify(adminMovieFields.flatMap((field) => {
    const operation = draft.operations[field]
    return operation ? [[field, operation.kind, operation.kind === 'override' ? operation.value : null]] : []
  }))
}

export function isAdminMovieDraftDirty(draft: AdminMovieDraft | undefined): boolean {
  return adminMovieDraftFingerprint(draft) !== '[]'
}

export function adminMovieFieldValue(item: AdminMovieItem, draft: AdminMovieDraft | undefined, field: AdminMovieField): AdminMovieDraftValue {
  const operation = draft?.operations[field]
  if (operation?.kind === 'override') return cloneValue(operation.value)
  if (operation?.kind === 'restore') return cloneValue(item.automatic[field])
  return cloneValue(item.values[field])
}

export function isAdminMovieFieldOverridden(item: AdminMovieItem, draft: AdminMovieDraft | undefined, field: AdminMovieField): boolean {
  const operation = draft?.operations[field]
  if (operation) return operation.kind === 'override'
  return item.overridden_fields.includes(field)
}

export function stageAdminMovieOverride(item: AdminMovieItem, draft: AdminMovieDraft | undefined, field: AdminMovieField, value: AdminMovieDraftValue): AdminMovieDraft {
  const next = cloneDraft(draft)
  next.expected_updated_at ??= item.updated_at
  const normalized = normalizeAdminMovieDraftValue(field, value)
  if (item.overridden_fields.includes(field) && valuesEqual(normalized, item.values[field])) {
    delete next.operations[field]
  } else {
    next.operations[field] = { kind: 'override', value: normalized }
  }
  return next
}

export function stageAdminMovieRestore(item: AdminMovieItem, draft: AdminMovieDraft | undefined, field: AdminMovieField): AdminMovieDraft {
  const next = cloneDraft(draft)
  next.expected_updated_at ??= item.updated_at
  if (item.overridden_fields.includes(field)) next.operations[field] = { kind: 'restore' }
  else delete next.operations[field]
  return next
}

export function validateAdminMovieDraft(item: AdminMovieItem, draft: AdminMovieDraft | undefined): AdminMovieValidationErrors {
  const errors: AdminMovieValidationErrors = {}
  const value = (field: AdminMovieField) => adminMovieFieldValue(item, draft, field)
  const title = value('title')
  if (!isStringValue(title) || title.trim() === '') errors.title = 'Indiquez un titre.'
  else if (runeLength(title.trim()) > 1024) errors.title = 'Le titre ne peut pas dépasser 1 024 caractères.'

  const runtime = value('runtime_minutes')
  if (!isNumberValue(runtime) || !Number.isInteger(runtime) || runtime < 0 || runtime > maxRuntime) errors.runtime_minutes = 'Utilisez une durée entière positive ou nulle.'

  const releaseDate = value('release_date')
  if (releaseDate !== null && (!isStringValue(releaseDate) || !isCalendarDate(releaseDate))) errors.release_date = 'Utilisez une date valide au format AAAA-MM-JJ.'

  const genres = value('genres')
  if (!Array.isArray(genres)) errors.genres = 'Les genres sont invalides.'
  else if (genres.length > 32) errors.genres = 'Limitez la liste à 32 genres.'
  else if (genres.some((genre) => genre.trim() === '' || runeLength(genre.trim()) > 256)) errors.genres = 'Chaque genre doit contenir de 1 à 256 caractères.'

  validateOptionalString(errors, 'overview', value('overview'), 10000)
  validateOptionalURL(errors, 'poster_url', value('poster_url'))
  validateOptionalURL(errors, 'backdrop_url', value('backdrop_url'))
  validateTrailerKey(errors, 'trailer_vf_youtube_key', value('trailer_vf_youtube_key'))
  validateTrailerKey(errors, 'trailer_vo_youtube_key', value('trailer_vo_youtube_key'))

  const vf = value('trailer_vf_youtube_key')
  const vo = value('trailer_vo_youtube_key')
  if (isStringValue(vf) && vf !== '' && vf === vo) {
    errors.trailer_vo_youtube_key = 'Les bandes-annonces VF et VO doivent être différentes.'
  }
  return errors
}

export function buildAdminMoviePatch(item: AdminMovieItem, draft: AdminMovieDraft | undefined): AdminMoviePatchRequest | null {
  if (!isAdminMovieDraftDirty(draft) || Object.keys(validateAdminMovieDraft(item, draft)).length > 0) return null
  const overrides: Partial<AdminMovieMetadata> = {}
  const restore: AdminMovieField[] = []
  for (const field of adminMovieFields) {
    const operation = draft?.operations[field]
    if (!operation) continue
    if (operation.kind === 'restore') restore.push(field)
    else Object.assign(overrides, { [field]: normalizeAdminMovieDraftValue(field, operation.value) })
  }
  const request: AdminMoviePatchRequest = { expected_updated_at: draft?.expected_updated_at ?? item.updated_at }
  if (Object.keys(overrides).length) request.overrides = overrides
  if (restore.length) request.restore = restore
  return request
}

export function parseAdminMovieRouteQuery(query: Readonly<AdminMovieRouteQueryInput>): AdminMovieRouteState {
  let runtimeMin = nonnegativeInteger(queryValue(query.runtime_min))
  let runtimeMax = nonnegativeInteger(queryValue(query.runtime_max))
  if (runtimeMin !== undefined && runtimeMax !== undefined && runtimeMin > runtimeMax) runtimeMax = undefined
  let releaseDateFrom = calendarDate(queryValue(query.release_date_from))
  let releaseDateTo = calendarDate(queryValue(query.release_date_to))
  if (releaseDateFrom && releaseDateTo && releaseDateFrom > releaseDateTo) releaseDateTo = undefined
  const overrideStatus = enumValue(queryValue(query.override_status), adminMovieOverrideStatuses) ?? 'all'
  const overrideField = overrideStatus === 'automatic' ? undefined : enumValue(queryValue(query.override_field), adminMovieFields)
  const sort = enumValue(queryValue(query.sort), adminMovieSorts) ?? 'title'
  const direction = enumValue(queryValue(query.direction), adminMovieDirections) ?? 'asc'
  const page = positiveSafeInteger(queryValue(query.page)) ?? 1
  const state: AdminMovieRouteState = {
    q: boundedTrimmed(queryValue(query.q), 1024),
    genre: boundedTrimmed(queryValue(query.genre), 256),
    override_status: overrideStatus,
    sort,
    direction,
    page
  }
  if (runtimeMin !== undefined) state.runtime_min = runtimeMin
  if (runtimeMax !== undefined) state.runtime_max = runtimeMax
  if (releaseDateFrom) state.release_date_from = releaseDateFrom
  if (releaseDateTo) state.release_date_to = releaseDateTo
  if (overrideField) state.override_field = overrideField
  return state
}

export function adminMovieRouteQuery(state: AdminMovieRouteState): AdminMovieOwnedRouteQuery {
  return {
    q: state.q || undefined,
    runtime_min: state.runtime_min === undefined ? undefined : String(state.runtime_min),
    runtime_max: state.runtime_max === undefined ? undefined : String(state.runtime_max),
    release_date_from: state.release_date_from,
    release_date_to: state.release_date_to,
    genre: state.genre || undefined,
    override_status: state.override_status === 'all' ? undefined : state.override_status,
    override_field: state.override_field,
    sort: state.sort === 'title' ? undefined : state.sort,
    direction: state.direction === 'asc' ? undefined : state.direction,
    page: state.page === 1 ? undefined : String(state.page)
  }
}

export function adminMovieRouteStateFromGrid(state: AdminMovieRouteState, sortModel: readonly AdminMovieGridSort[], filterModel: Readonly<AdminMovieGridFilterModel>): AdminMovieRouteState {
  const sortEntry = sortModel.find((entry) => isAdminMovieSort(entry.colId))
  const runtime = filterModel.runtime_minutes
  const releaseDate = filterModel.release_date
  const genre = filterModel.genres
  const runtimeMin = runtime?.type === 'inRange' ? numberFilterValue(runtime.filter) : undefined
  const runtimeMax = runtime?.type === 'inRange' ? numberFilterValue(runtime.filterTo) : undefined
  const releaseDateFrom = releaseDate?.type === 'inRange' ? calendarDate(dateFilterValue(releaseDate.dateFrom)) : undefined
  const releaseDateTo = releaseDate?.type === 'inRange' ? calendarDate(dateFilterValue(releaseDate.dateTo)) : undefined
  return {
    ...state,
    runtime_min: runtimeMin,
    runtime_max: runtimeMax,
    release_date_from: releaseDateFrom,
    release_date_to: releaseDateTo,
    genre: genre?.type === 'contains' ? boundedTrimmed(String(genre.filter ?? ''), 256) : '',
    sort: sortEntry && isAdminMovieSort(sortEntry.colId) ? sortEntry.colId : 'title',
    direction: sortEntry?.sort ?? 'asc',
    page: 1
  }
}

export function adminMovieGridFilterModel(state: AdminMovieRouteState): AdminMovieGridFilterModel {
  const model: AdminMovieGridFilterModel = {}
  if (state.runtime_min !== undefined || state.runtime_max !== undefined) {
    model.runtime_minutes = { type: 'inRange', filter: state.runtime_min ?? 0, filterTo: state.runtime_max ?? maxRuntime }
  }
  if (state.release_date_from || state.release_date_to) {
    model.release_date = { type: 'inRange', dateFrom: state.release_date_from ?? '0001-01-01', dateTo: state.release_date_to ?? '9999-12-31' }
  }
  if (state.genre) model.genres = { type: 'contains', filter: state.genre }
  return model
}

export function adminMovieQueryFromGrid(state: AdminMovieRouteState, request: AdminMovieGridRequest): AdminMoviesQuery {
  const translated = adminMovieRouteStateFromGrid(state, request.sortModel ?? [], request.filterModel ?? {})
  const start = Math.max(0, request.startRow ?? (state.page - 1) * ADMIN_MOVIE_PAGE_SIZE)
  const end = Math.max(start + 1, request.endRow ?? start + ADMIN_MOVIE_PAGE_SIZE)
  const query: AdminMoviesQuery = {
    limit: Math.min(100, end - start),
    offset: start,
    override_status: translated.override_status,
    sort: translated.sort,
    direction: translated.direction
  }
  if (translated.q) query.search = translated.q
  if (translated.runtime_min !== undefined) query.runtime_min = translated.runtime_min
  if (translated.runtime_max !== undefined) query.runtime_max = translated.runtime_max
  if (translated.release_date_from) query.release_date_from = translated.release_date_from
  if (translated.release_date_to) query.release_date_to = translated.release_date_to
  if (translated.genre) query.genre = translated.genre
  if (translated.override_field) query.override_field = translated.override_field
  return query
}

function cloneDraft(draft: AdminMovieDraft | undefined): AdminMovieDraft {
  const operations: AdminMovieDraft['operations'] = {}
  for (const field of adminMovieFields) {
    const operation = draft?.operations[field]
    if (operation?.kind === 'restore') operations[field] = { kind: 'restore' }
    else if (operation) operations[field] = { kind: 'override', value: cloneValue(operation.value) }
  }
  return { operations, expected_updated_at: draft?.expected_updated_at }
}

function cloneValue(value: AdminMovieDraftValue): AdminMovieDraftValue {
  return Array.isArray(value) ? [...value] : value
}

function normalizeAdminMovieDraftValue(field: AdminMovieField, value: AdminMovieDraftValue): AdminMovieDraftValue {
  if (field === 'title' && isStringValue(value)) return value.trim()
  if (field === 'genres' && Array.isArray(value)) return value.map((genre) => genre.trim())
  return cloneValue(value)
}

function valuesEqual(left: AdminMovieDraftValue, right: AdminMovieDraftValue): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

function validateOptionalString(errors: AdminMovieValidationErrors, field: AdminMovieField, value: AdminMovieDraftValue, maxLength: number) {
  if (value !== null && (!isStringValue(value) || runeLength(value) > maxLength)) errors[field] = `Ce champ ne peut pas dépasser ${maxLength.toLocaleString('fr-FR')} caractères.`
}

function validateOptionalURL(errors: AdminMovieValidationErrors, field: 'poster_url' | 'backdrop_url', value: AdminMovieDraftValue) {
  if (value === null) return
  if (!isStringValue(value) || runeLength(value) > 4096 || /\s/u.test(value)) {
    errors[field] = 'Utilisez une URL HTTPS valide.'
    return
  }
  try {
    const parsed = new URL(value)
    if (parsed.protocol !== 'https:' || !parsed.hostname || parsed.username || parsed.password) errors[field] = 'Utilisez une URL HTTPS valide.'
  } catch {
    errors[field] = 'Utilisez une URL HTTPS valide.'
  }
}

function validateTrailerKey(errors: AdminMovieValidationErrors, field: 'trailer_vf_youtube_key' | 'trailer_vo_youtube_key', value: AdminMovieDraftValue) {
  if (value !== null && (!isStringValue(value) || !/^[A-Za-z0-9_-]{11}$/.test(value))) errors[field] = 'Utilisez une clé YouTube de 11 caractères.'
}

function queryValue(value: RouteInputValue): string | undefined {
  return isStringValue(value) ? value : undefined
}

function boundedTrimmed(value: string | undefined, maxLength: number): string {
  const trimmed = value?.trim() ?? ''
  return runeLength(trimmed) <= maxLength ? trimmed : ''
}

function nonnegativeInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= maxRuntime ? parsed : undefined
}

function positiveSafeInteger(value: string | undefined): number | undefined {
  if (!value || !/^\d+$/.test(value)) return undefined
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined
}

function enumValue<const T extends string>(value: string | undefined, values: readonly T[]): T | undefined {
  return values.find((candidate) => candidate === value)
}

function calendarDate(value: string | undefined): string | undefined {
  return value && isCalendarDate(value) ? value : undefined
}

function isCalendarDate(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false
  const [year = Number.NaN, month = Number.NaN, day = Number.NaN] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year, month - 1, day, 12))
  return parsed.getUTCFullYear() === year && parsed.getUTCMonth() === month - 1 && parsed.getUTCDate() === day
}

function numberFilterValue(value: string | number | null | undefined): number | undefined {
  const parsed = isNumberValue(value) ? value : Number(value)
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= maxRuntime ? parsed : undefined
}

function dateFilterValue(value: string | null | undefined): string | undefined {
  return value?.slice(0, 10)
}

function runeLength(value: string): number {
  return [...value].length
}

function isStringValue(value: RouteInputValue | AdminMovieDraftValue): value is string {
  return Object.prototype.toString.call(value) === '[object String]'
}

function isNumberValue(value: string | number | string[] | null | undefined): value is number {
  return Object.prototype.toString.call(value) === '[object Number]'
}

function isAdminMovieSort(value: string): value is AdminMovieSort {
  return adminMovieSorts.some((candidate) => candidate === value)
}
