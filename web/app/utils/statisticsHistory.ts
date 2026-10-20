import type { LocationQuery } from 'vue-router'
import type {
  HistoryOptionsResponse,
  HistoryStatisticsQuery,
} from '../types/api.ts'
import {
  createStatisticsRequest,
  parseStatisticsQuery,
  statisticsDraft,
  statisticsDraftQuery,
  statisticsQuerySignature,
  statisticsRouteQuery,
  type StatisticsDraft,
  type StatisticsSelectOption,
} from './statistics.ts'

export const statisticsPeriods = [
  { value: 'next7', label: '7 prochains jours' },
  { value: 'next30', label: '30 prochains jours' },
  { value: 'week', label: 'Semaine courante' },
  { value: 'all', label: 'Depuis le début de la collecte' },
] as const
export type StatisticsPeriod = (typeof statisticsPeriods)[number]['value']
export type StatisticsPageDraft = Omit<
  StatisticsDraft,
  'date' | 'date_to' | 'explicitDates'
> & { period: StatisticsPeriod }
export function statisticsPeriod(route: LocationQuery) {
  if (route.period === undefined) return { period: 'next7' as const, error: '' }
  const choice = statisticsPeriods.find(
    (choice) => choice.value === route.period,
  )
  return choice
    ? { period: choice.value, error: '' }
    : {
        period: 'next7' as const,
        error:
          'Période invalide. Choisissez une période ou réinitialisez les filtres.',
      }
}
export function statisticsParisToday(now = new Date()) {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Europe/Paris',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(now)
}
interface ParsedStatisticsPageQuery {
  query: HistoryStatisticsQuery
  error: string
}
export function parseStatisticsPageQuery(
  route: LocationQuery,
): ParsedStatisticsPageQuery {
  const { period, error } = statisticsPeriod(route)
  if (error) return { query: { period }, error }
  const values = { ...route }
  delete values.date
  delete values.date_to
  const parsed = parseStatisticsQuery(values)
  return { query: { ...parsed.query, period }, error: parsed.error }
}
export function statisticsPageSignature(route: LocationQuery) {
  return JSON.stringify([route.period, statisticsQuerySignature(route)])
}
export function statisticsPageDraft(route: LocationQuery): StatisticsPageDraft {
  const { period } = statisticsPeriod(route)
  const values = { ...route }
  delete values.date
  delete values.date_to
  const {
    date: _date,
    date_to: _through,
    explicitDates: _explicit,
    ...draft
  } = statisticsDraft(values)
  return { ...draft, period }
}
export function statisticsPageDraftQuery(draft: StatisticsPageDraft) {
  const parsed = statisticsDraftQuery({
    ...draft,
    date: '',
    date_to: '',
    explicitDates: false,
  })
  return {
    query: { ...parsed.query, period: draft.period },
    error: parsed.error,
  }
}
export function statisticsPageRoute(
  route: LocationQuery,
  period: StatisticsPeriod = 'next7',
  query: Partial<HistoryStatisticsQuery> = {},
) {
  const next = statisticsRouteQuery(route, query)
  delete next.mode
  delete next.period
  if (period !== 'next7') next.period = period
  delete next.date
  delete next.date_to
  return next
}

export function historySelectionOptions(
  items: readonly StatisticsSelectOption[],
  known: readonly StatisticsSelectOption[],
  selected: readonly string[],
  resolved: ReadonlySet<string>,
) {
  const labels = new Map(
    [...items, ...known].map((option) => [option.value, option]),
  )
  const visible = new Set(items.map((option) => option.value))
  return [
    ...items.map((option) => labels.get(option.value) ?? option),
    ...selected
      .filter((value) => !visible.has(value))
      .map(
        (value) =>
          labels.get(value) ?? {
            value,
            label: resolved.has(value) ? `${value} (indisponible)` : value,
          },
      ),
  ]
}

// Invalidate immediately on each edit, not after debounce: old responses cannot win during the wait.
export function createHistoryOptionsRequest(callbacks: {
  start: () => void
  success: (result: HistoryOptionsResponse) => void
  error: (cause: unknown) => void
  finish: () => void
}) {
  const request = createStatisticsRequest(callbacks)
  let timer: ReturnType<typeof setTimeout> | undefined
  return {
    schedule(
      fetcher: (signal: AbortSignal) => Promise<HistoryOptionsResponse>,
      immediate = false,
    ) {
      clearTimeout(timer)
      request.cancel()
      callbacks.start()
      if (immediate) return request.run(fetcher)
      timer = setTimeout(() => {
        void request.run(fetcher)
      }, 250)
    },
    cancel() {
      clearTimeout(timer)
      request.cancel()
    },
  }
}
