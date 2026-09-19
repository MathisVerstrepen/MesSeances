import type { LocationQuery } from 'vue-router'
import type { Provider, StatisticsBucket, StatisticsCityRank, StatisticsDateRange, StatisticsHeatmapCell, StatisticsQuery, StatisticsTheaterRank } from '../types/api.ts'
import { calendarDate, enumQueryValue, mergeOwnedQuery } from './routeQuery.ts'
import { formatLabel, formatOptions } from './formats.ts'
import { languageLabel, showtimeLanguageValues } from './showtimeFilters.ts'

export const statisticsQueryKeys = ['date', 'date_to', 'city', 'theater', 'chain', 'language', 'format', 'genre', 'pass', 'film'] as const
export type StatisticsFilterKey = Exclude<typeof statisticsQueryKeys[number], 'date' | 'date_to' | 'film'>
export type StatisticsMultiFilterKey = 'city' | 'theater'
export type StatisticsScalarFilterKey = Exclude<StatisticsFilterKey, StatisticsMultiFilterKey>
export type StatisticsDraft = Record<Exclude<typeof statisticsQueryKeys[number], StatisticsMultiFilterKey>, string> & Record<StatisticsMultiFilterKey, string[]> & { explicitDates: boolean }
export const statisticsMaxSelections = 50
export const statisticsChainLabels = {
  ugc: 'UGC', kinepolis: 'Kinepolis', pathe: 'Pathé', cgr: 'CGR', megarama: 'Megarama', cineville: 'Cinéville', mk2: 'MK2', cinewest: 'CinéWest', grandecran: 'Grand Écran', noecinemas: 'Noé Cinémas'
} as const satisfies Record<Provider, string>
const chains = Object.keys(statisticsChainLabels)
const languages = [...showtimeLanguageValues, 'unknown'] as const
const formats = [...formatOptions.filter(option => option.value !== 'ALL').map(option => option.value), 'unknown']
const invalidFilters = 'Filtres invalides. Vérifiez les champs ou réinitialisez la sélection.'

export function statisticsDateError(date: string | undefined, through: string | undefined, today?: string): string {
  if (date === undefined && through === undefined) return ''
  if (!calendarDate(date) || (through !== undefined && !calendarDate(through))) return 'Choisissez des dates valides au format jour, mois, année.'
  if (!date) return invalidFilters
  if (today && date < today) return 'La date de début doit être aujourd’hui ou plus tard.'
  const end = through ?? date
  if (end < date) return 'La date de fin doit suivre la date de début.'
  // UTC is used only to count calendar dates, never elapsed Paris hours across DST.
  if ((Date.parse(`${end}T12:00:00Z`) - Date.parse(`${date}T12:00:00Z`)) / 86400000 >= 31) return 'Choisissez une période de 31 jours maximum.'
  return ''
}

export interface ParsedStatisticsQuery { query: StatisticsQuery; error: string }
export function parseStatisticsQuery(route: LocationQuery, today?: string, validateDates = statisticsDateError): ParsedStatisticsQuery {
  const values: Record<string, string> = {}
  const selections: Partial<Record<StatisticsMultiFilterKey, string[]>> = {}
  const encoded = new URLSearchParams()
  for (const key of statisticsQueryKeys) {
    const raw = route[key]
    if (raw === undefined) continue
    const multiple = key === 'city' || key === 'theater'
    const entries = Array.isArray(raw) ? raw : [raw]
    if ((!multiple && Array.isArray(raw)) || (multiple && entries.length > statisticsMaxSelections)) return { query: {}, error: invalidFilters }
    const normalized: string[] = []
    for (const entry of entries) {
      if (entry === null) return { query: {}, error: invalidFilters }
      // Reject NUL and lone UTF-16 surrogates before UTF-8 encoding can replace them.
      if (key === 'film' && (entry.includes('\0') || /[\uD800-\uDFFF]/u.test(entry))) return { query: {}, error: invalidFilters }
      const isDate = key === 'date' || key === 'date_to'
      if (!isDate && new TextEncoder().encode(entry).length > 200) return { query: {}, error: invalidFilters }
      const value = isDate ? entry : entry.trim()
      if (!value) return { query: {}, error: invalidFilters }
      encoded.append(key, entry)
      normalized.push(value)
    }
    if (multiple) {
      if (normalized.length) selections[key] = [...new Set(normalized)]
    } else values[key] = normalized[0]!
  }
  if (new TextEncoder().encode(encoded.toString()).length > 4096) return { query: {}, error: invalidFilters }
  if ((values.chain && !chains.includes(values.chain)) || (values.language && !languages.some(value => value === values.language)) || (values.format && !formats.includes(values.format))) return { query: {}, error: invalidFilters }
  const dateError = validateDates(values.date, values.date_to, today)
  if (dateError) return { query: {}, error: dateError }
  const query: StatisticsQuery = { ...selections }
  for (const key of ['date', 'date_to', 'genre', 'pass', 'film'] as const) {
    if (values[key] !== undefined) query[key] = values[key]
  }
  if (values.chain) query.chain = enumQueryValue(values.chain, ['ugc', 'kinepolis', 'pathe', 'cgr', 'megarama', 'cineville', 'mk2', 'cinewest', 'grandecran', 'noecinemas'])
  if (values.language) query.language = enumQueryValue(values.language, languages)
  if (values.format) query.format = enumQueryValue(values.format, ['2D', '3D', 'IMAX', 'DOLBY', 'SCREENX', 'LASER_ULTRA', '4DX', 'ICE', 'INFINITY_VISION', 'unknown'])
  return { query, error: '' }
}

export function statisticsDraft(route: LocationQuery, range?: StatisticsDateRange | null, parse = parseStatisticsQuery): StatisticsDraft {
  const draft: StatisticsDraft = { date: '', date_to: '', city: [], theater: [], chain: '', language: '', format: '', genre: '', pass: '', film: '', explicitDates: route.date !== undefined || route.date_to !== undefined }
  const parsed = parse(route)
  const source = parsed.error ? route : statisticsRouteQuery({}, parsed.query)
  for (const key of statisticsQueryKeys) {
    const value = source[key]
    if (key === 'city' || key === 'theater') draft[key] = value === undefined ? [] : (Array.isArray(value) ? value : [value]).map(entry => entry ?? '')
    else draft[key] = Array.isArray(value) ? value[0] ?? '' : value ?? ''
  }
  if (!draft.explicitDates && range) {
    draft.date = range.from
    draft.date_to = range.through
  } else if (draft.date && route.date_to === undefined) draft.date_to = draft.date
  return draft
}

export function statisticsDraftQuery(draft: StatisticsDraft, today?: string, parse = parseStatisticsQuery) {
  const values: LocationQuery = {}
  for (const key of statisticsQueryKeys) {
    if (key === 'date' || key === 'date_to') {
      if (draft.explicitDates) values[key] = draft[key]
    } else if (key === 'city' || key === 'theater') {
      if (draft[key].length) values[key] = [...draft[key]]
    } else if (draft[key]) values[key] = draft[key]
  }
  return parse(values, today)
}

export function statisticsRouteQuery(route: LocationQuery, query: StatisticsQuery = {}): LocationQuery {
  const values = { ...query, city: query.city?.length ? [...query.city] : undefined, theater: query.theater?.length ? [...query.theater] : undefined }
  return mergeOwnedQuery(route, statisticsQueryKeys, values)
}

export function statisticsQuerySignature(route: LocationQuery): string {
  return JSON.stringify(statisticsQueryKeys.map(key => [key, route[key]]))
}

export function statisticsShare(count: number, total: number): string {
  if (!Number.isFinite(count) || !Number.isFinite(total) || total <= 0) return 'Non calculable'
  return new Intl.NumberFormat('fr-FR', { style: 'percent', minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(count / total)
}

export function statisticsCount(count: number): string {
  return new Intl.NumberFormat('fr-FR').format(count)
}

export const statisticsHeatmapColors = ['#ffffff', '#f1f7ed', '#e0eed7', '#c9e1bc', '#afd09e', '#8dbc7a', '#6b9e59', '#477b3a', '#2d5b28', '#193d1a'] as const

export function statisticsHeatmapLevel(count: number, maximum: number): number {
  if (!Number.isFinite(count) || !Number.isFinite(maximum) || count <= 0 || maximum <= 0) return 0
  // Square-root scaling keeps typical counts distinct when a few hours dominate.
  return Math.min(9, Math.ceil(Math.sqrt(Math.min(count / maximum, 1)) * 9))
}

export function statisticsHeatmapStyle(count: number, maximum: number) {
  const level = statisticsHeatmapLevel(count, maximum)
  return { backgroundColor: statisticsHeatmapColors[level], color: level >= 7 ? '#ffffff' : '#20251f' }
}

export function statisticsBucketLabel(value: string, label: string, kind?: 'language' | 'format'): string {
  if (value === 'unknown') return 'Non renseigné'
  if (kind === 'language') return languageLabel(value)
  if (kind === 'format') return formatLabel(value)
  return label
}

export interface StatisticsBar extends StatisticsBucket { href?: string; detail?: string }
export function statisticsBars(rows: readonly StatisticsBar[], total?: number) {
  const max = rows.reduce((largest, row) => Math.max(largest, row.count), 0)
  return rows.map(row => ({ ...row, width: max > 0 ? row.count / max * 100 : 0, share: total === undefined ? undefined : statisticsShare(row.count, total) }))
}

export interface StatisticsSelectOption { value: string; label: string }
export function statisticsOptionsWithSelection(options: readonly StatisticsSelectOption[], selected: readonly string[]) {
  const available = new Set(options.map(option => option.value))
  const missing = [...new Set(selected)].filter(value => !available.has(value))
  return missing.length ? [...options, ...missing.map(value => ({ value, label: `${value} (indisponible)` }))] : options
}

export function statisticsSearchOptions(options: readonly StatisticsSelectOption[], selected: readonly string[], search: string) {
  const selectedValues = new Set(selected)
  const term = search.trim().toLocaleLowerCase('fr-FR')
  return options.filter(option => selectedValues.has(option.value) || option.label.toLocaleLowerCase('fr-FR').includes(term))
}

export function toggleStatisticsSelection(selected: readonly string[], value: string, maximum: number): string[] {
  if (selected.includes(value)) return selected.filter(entry => entry !== value)
  return selected.length < maximum ? [...selected, value] : [...selected]
}

export function statisticsSelectionSummary(options: readonly StatisticsSelectOption[], selected: readonly string[], label: string, allLabel: string): string {
  if (!selected.length) return allLabel
  if (selected.length === 1) return options.find(option => option.value === selected[0])?.label ?? `${selected[0]} (indisponible)`
  return `${selected.length} ${label.toLocaleLowerCase('fr-FR')}s ${label === 'Ville' ? 'sélectionnées' : 'sélectionnés'}`
}

export const statisticsHours = Array.from({ length: 24 }, (_, index) => (index + 8) % 24)
export const statisticsWeekdays = ['Lundi', 'Mardi', 'Mercredi', 'Jeudi', 'Vendredi', 'Samedi', 'Dimanche']
export function statisticsHeatmapRows(cells: readonly StatisticsHeatmapCell[]) {
  const counts = new Map(cells.map(cell => [`${cell.weekday}:${cell.hour}`, cell.showtime_count]))
  return statisticsWeekdays.map((label, index) => ({ label, cells: statisticsHours.map(hour => ({ hour, count: counts.get(`${index + 1}:${hour}`) ?? 0 })) }))
}

export type StatisticsLocalRow = StatisticsCityRank | StatisticsTheaterRank
const cityNameParticles = new Set(['à', 'au', 'aux', 'd', 'de', 'des', 'du', 'en', 'et', 'l', 'la', 'le', 'les', 'lès', 'près', 'sous', 'sur'])
// Display only: provider spelling must not change city identities or grouping.
export function statisticsCityName(value: string) {
  return value.trim().toLocaleLowerCase('fr-FR').replace(/\p{L}[\p{L}\p{M}]*/gu, (word: string, offset: number) =>
    offset > 0 && cityNameParticles.has(word) ? word : word.charAt(0).toLocaleUpperCase('fr-FR') + word.slice(1))
}
export type StatisticsLocalColumn = 'name' | 'movie_count' | 'showtime_count' | 'theater_count' | 'city'
export interface StatisticsLocalSort { column: StatisticsLocalColumn; direction: 'ascending' | 'descending' }
const normalized = (value: string) => value.trim().toLocaleLowerCase('fr-FR')
const compareText = (left: string, right: string) => left < right ? -1 : left > right ? 1 : 0
const localID = (row: StatisticsLocalRow) => 'id' in row ? row.id : row.slug
function localValue(row: StatisticsLocalRow, column: StatisticsLocalColumn): string | number {
  if (column === 'city') return 'city' in row ? row.city : ''
  if (column === 'theater_count') return 'theater_count' in row ? row.theater_count : 0
  return row[column]
}
export function nextStatisticsSort(current: StatisticsLocalSort | null, column: StatisticsLocalColumn): StatisticsLocalSort {
  current ??= { column: 'showtime_count', direction: 'descending' }
  return { column, direction: current?.column === column ? current.direction === 'ascending' ? 'descending' : 'ascending' : column === 'name' || column === 'city' ? 'ascending' : 'descending' }
}
export function statisticsLocalPage(rows: readonly StatisticsLocalRow[], sort: StatisticsLocalSort | null, page: number) {
  const sorted = [...rows].sort((left, right) => {
    const tie = compareText(normalized(left.name), normalized(right.name)) || compareText(localID(left), localID(right))
    if (!sort) return right.showtime_count - left.showtime_count || right.movie_count - left.movie_count || tie
    const a = localValue(left, sort.column)
    const b = localValue(right, sort.column)
    const comparison = sort.column === 'name' || sort.column === 'city' ? compareText(normalized(String(a)), normalized(String(b))) : Number(a) - Number(b)
    return comparison * (sort.direction === 'ascending' ? 1 : -1) || tie
  })
  const pages = Math.max(1, Math.ceil(sorted.length / 20))
  const currentPage = Math.min(pages, Math.max(1, Math.trunc(page) || 1))
  return { rows: sorted.slice((currentPage - 1) * 20, currentPage * 20), page: currentPage, pages, total: rows.length }
}

export function createStatisticsRequest<T>(callbacks: { start: () => void; success: (result: T) => void; error: (cause: unknown) => void; finish: () => void }) {
  let revision = 0
  let controller: AbortController | undefined
  return {
    async run(fetcher: (signal: AbortSignal) => Promise<T>) {
      const current = ++revision
      controller?.abort()
      controller = new AbortController()
      callbacks.start()
      try {
        const result = await fetcher(controller.signal)
        if (revision === current) callbacks.success(result)
      } catch (cause) {
        if (revision === current) callbacks.error(cause)
      } finally {
        if (revision === current) callbacks.finish()
      }
    },
    cancel() { revision++; controller?.abort() }
  }
}
