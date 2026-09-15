import type { LocationQuery } from 'vue-router'
import type { HistoryOptionsResponse, StatisticsDateRange, StatisticsQuery } from '../types/api.ts'
import { createStatisticsRequest, parseStatisticsQuery, statisticsDraft, statisticsDraftQuery, statisticsQuerySignature, statisticsRouteQuery, type StatisticsDraft, type StatisticsSelectOption } from './statistics.ts'

export const statisticsPeriods = [
  { value: 'next7', label: '7 prochains jours' },
  { value: 'last30', label: '30 derniers jours' },
  { value: 'all', label: 'Depuis le début de la collecte' },
  { value: 'custom', label: 'Période personnalisée' }
] as const
export type StatisticsPeriod = typeof statisticsPeriods[number]['value']
export type StatisticsPageDraft = StatisticsDraft & { period: StatisticsPeriod }
export function statisticsPeriod(route: LocationQuery) {
  if (route.period === undefined) return { period: 'next7' as const, error: '' }
  const choice = statisticsPeriods.find(choice => choice.value === route.period)
  return choice ? { period: choice.value, error: '' } : { period: 'next7' as const, error: 'Période invalide. Choisissez une période ou réinitialisez les filtres.' }
}
export function statisticsParisToday(now = new Date()) {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Paris', year: 'numeric', month: '2-digit', day: '2-digit' }).format(now)
}
export function statisticsPeriodRange(period: StatisticsPeriod, today: string): StatisticsDateRange | null {
  // UTC arithmetic counts calendar days, not elapsed Paris hours across DST.
  const shift = (days: number) => {
    const date = new Date(`${today}T12:00:00Z`)
    date.setUTCDate(date.getUTCDate() + days)
    return date.toISOString().slice(0, 10)
  }
  if (period === 'next7') return { from: today, through: shift(6) }
  if (period === 'last30') return { from: shift(-29), through: today }
  return null
}
export function historyDateError(date?: string, through?: string) {
  if (date === undefined && through === undefined) return ''
  const valid = (value?: string) => {
    if (!value || !/^(?!0000)\d{4}-\d{2}-\d{2}$/.test(value)) return false
    const parsed = new Date(`${value}T12:00:00Z`)
    return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value
  }
  if (!valid(date) || (through !== undefined && !valid(through))) return 'Choisissez des dates valides au format jour, mois, année.'
  if (through && date && through < date) return 'La date de fin doit suivre la date de début.'
  return ''
}
export function parseHistoryStatisticsQuery(route: LocationQuery) {
  return parseStatisticsQuery(route, undefined, historyDateError)
}
export function parseStatisticsPageQuery(route: LocationQuery, today = statisticsParisToday()) {
  const { period, error } = statisticsPeriod(route)
  if (error) return { query: {}, error }
  const values = { ...route }
  if (period === 'custom') {
    if (values.date === undefined || values.date_to === undefined) return { query: {}, error: 'Choisissez une date de début et une date de fin.' }
  } else {
    delete values.date
    delete values.date_to
    const range = statisticsPeriodRange(period, today)
    if (range) { values.date = range.from; values.date_to = range.through }
  }
  return parseHistoryStatisticsQuery(values)
}
export function statisticsPageSignature(route: LocationQuery) {
  return JSON.stringify([route.period, statisticsQuerySignature(route)])
}
export function statisticsPageDraft(route: LocationQuery, today = statisticsParisToday()): StatisticsPageDraft {
  const { period } = statisticsPeriod(route)
  const values = { ...route }
  if (period !== 'custom') { delete values.date; delete values.date_to }
  const draft = statisticsDraft(values, statisticsPeriodRange(period, today), parseHistoryStatisticsQuery)
  // Unlike the API's single-day shorthand, custom UI requires both explicit endpoints.
  if (period === 'custom' && route.date_to === undefined) draft.date_to = ''
  return { ...draft, period }
}
export function statisticsCustomDraft(draft: StatisticsPageDraft, displayed: StatisticsDateRange | null | undefined, today = statisticsParisToday()): StatisticsPageDraft {
  const range = displayed ?? statisticsPeriodRange('next7', today)!
  return { ...draft, period: 'custom', date: range.from, date_to: range.through }
}
export function statisticsPageDraftQuery(draft: StatisticsPageDraft, today = statisticsParisToday()) {
  return statisticsDraftQuery({ ...draft, explicitDates: draft.period === 'custom' }, today, values => parseStatisticsPageQuery({ ...values, period: draft.period }, today))
}
export function statisticsPageRoute(route: LocationQuery, period: StatisticsPeriod = 'next7', query: StatisticsQuery = {}) {
  const next = statisticsRouteQuery(route, query)
  delete next.mode
  delete next.period
  if (period !== 'next7') next.period = period
  if (period !== 'custom') { delete next.date; delete next.date_to }
  return next
}

export function historySelectionOptions(items: readonly StatisticsSelectOption[], known: readonly StatisticsSelectOption[], selected: readonly string[], resolved: ReadonlySet<string>) {
  const labels = new Map([...items, ...known].map(option => [option.value, option]))
  const visible = new Set(items.map(option => option.value))
  return [...items.map(option => labels.get(option.value) ?? option), ...selected.filter(value => !visible.has(value)).map(value => labels.get(value) ?? { value, label: resolved.has(value) ? `${value} (indisponible)` : value })]
}

// Invalidate immediately on each edit, not after debounce: old responses cannot win during the wait.
export function createHistoryOptionsRequest(callbacks: { start: () => void; success: (result: HistoryOptionsResponse) => void; error: (cause: unknown) => void; finish: () => void }) {
  const request = createStatisticsRequest(callbacks)
  let timer: ReturnType<typeof setTimeout> | undefined
  return {
    schedule(fetcher: (signal: AbortSignal) => Promise<HistoryOptionsResponse>, immediate = false) {
      clearTimeout(timer)
      request.cancel()
      callbacks.start()
      if (immediate) return request.run(fetcher)
      timer = setTimeout(() => { void request.run(fetcher) }, 250)
    },
    cancel() { clearTimeout(timer); request.cancel() }
  }
}
