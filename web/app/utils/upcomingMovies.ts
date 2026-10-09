import type { LocationQuery } from 'vue-router'
import type {
  UpcomingCatalogMovie,
  UpcomingMoviesQuery,
  UpcomingMoviesResponse,
} from '../types/api.ts'
import { isCalendarDate } from './date.ts'
import { positiveSafeInteger, singularQueryValue } from './routeQuery.ts'

export interface UpcomingRouteState {
  view: 'upcoming' | 'history'
  year: number | null
  month: number | null
  page: number
}

export function parseUpcomingRoute(query: LocationQuery): UpcomingRouteState {
  const view =
    singularQueryValue(query.vue) === 'historique' ? 'history' : 'upcoming'
  const year = positiveSafeInteger(singularQueryValue(query.annee))
  const month = positiveSafeInteger(singularQueryValue(query.mois))
  return {
    view,
    year: view === 'history' && year && year <= 9999 ? year : null,
    month: view === 'history' && month && month <= 12 ? month : null,
    page: positiveSafeInteger(singularQueryValue(query.page)) ?? 1,
  }
}

export function upcomingRouteQuery(state: UpcomingRouteState): LocationQuery {
  const query: LocationQuery = {}
  if (state.view === 'history') {
    query.vue = 'historique'
    if (state.year !== null) query.annee = String(state.year)
    if (state.month !== null) query.mois = String(state.month)
  }
  if (state.page > 1) query.page = String(state.page)
  return query
}

export function upcomingApiQuery(
  state: UpcomingRouteState,
): UpcomingMoviesQuery {
  const query: UpcomingMoviesQuery = { page: state.page }
  if (state.view === 'history') {
    query.view = 'history'
    if (state.year !== null) query.year = state.year
    if (state.month !== null) query.month = state.month
  }
  return query
}

export function resolvedUpcomingRoute(
  response: UpcomingMoviesResponse,
): UpcomingRouteState {
  return {
    view: response.view,
    year: response.year,
    month: response.month,
    page: response.page,
  }
}

export function formatReleaseMonth(month: number): string {
  return new Intl.DateTimeFormat('fr-FR', {
    month: 'long',
    timeZone: 'UTC',
  }).format(new Date(Date.UTC(2000, month - 1, 1)))
}

export function releaseWeekStart(value: string): string {
  if (!isCalendarDate(value)) throw new Error('Invalid French release date')
  const date = new Date(`${value}T12:00:00Z`)
  // UTC calendar arithmetic keeps Wednesday-through-Tuesday weeks independent of DST and host timezone.
  date.setUTCDate(date.getUTCDate() - ((date.getUTCDay() + 4) % 7))
  return date.toISOString().slice(0, 10)
}

export function groupUpcomingMovies(
  items: UpcomingCatalogMovie[],
  view: UpcomingRouteState['view'] = 'upcoming',
): Array<{ weekStart: string; movies: UpcomingCatalogMovie[] }> {
  const groups = new Map<string, UpcomingCatalogMovie[]>()
  for (const movie of items) {
    const weekStart = releaseWeekStart(movie.french_release_date)
    const movies = groups.get(weekStart) ?? []
    movies.push(movie)
    groups.set(weekStart, movies)
  }
  return [...groups]
    .sort(([left], [right]) =>
      view === 'history'
        ? right.localeCompare(left)
        : left.localeCompare(right),
    )
    .map(([weekStart, movies]) => ({ weekStart, movies }))
}

export function formatReleaseWeek(value: string): string {
  return formatFrenchReleaseDate(releaseWeekStart(value))
}

export function formatFrenchReleaseDate(value: string): string {
  if (!isCalendarDate(value)) throw new Error('Invalid French release date')
  return new Intl.DateTimeFormat('fr-FR', {
    timeZone: 'UTC',
    day: 'numeric',
    month: 'long',
    year: 'numeric',
  }).format(new Date(`${value}T12:00:00Z`))
}
