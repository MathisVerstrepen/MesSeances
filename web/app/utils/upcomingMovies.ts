import type { LocationQuery } from 'vue-router'
import type { UpcomingCatalogMovie, UpcomingMoviesQuery } from '../types/api.ts'
import { isCalendarDate } from './date.ts'
import { positiveSafeInteger, singularQueryValue } from './routeQuery.ts'

export interface UpcomingRouteState {
  page: number
}

export function parseUpcomingRoute(query: LocationQuery): UpcomingRouteState {
  return {
    page: positiveSafeInteger(singularQueryValue(query.page)) ?? 1
  }
}

export function upcomingRouteQuery(state: UpcomingRouteState): LocationQuery {
  const query: LocationQuery = {}
  if (state.page > 1) query.page = String(state.page)
  return query
}

export function upcomingApiQuery(state: UpcomingRouteState): UpcomingMoviesQuery {
  return { page: state.page }
}

export function releaseWeekStart(value: string): string {
  if (!isCalendarDate(value)) throw new Error('Invalid French release date')
  const date = new Date(`${value}T12:00:00Z`)
  // UTC calendar arithmetic keeps Wednesday-through-Tuesday weeks independent of DST and host timezone.
  date.setUTCDate(date.getUTCDate() - (date.getUTCDay() + 4) % 7)
  return date.toISOString().slice(0, 10)
}

export function groupUpcomingMovies(items: UpcomingCatalogMovie[]): Array<{ weekStart: string; movies: UpcomingCatalogMovie[] }> {
  const groups = new Map<string, UpcomingCatalogMovie[]>()
  for (const movie of items) {
    const weekStart = releaseWeekStart(movie.french_release_date)
    const movies = groups.get(weekStart) ?? []
    movies.push(movie)
    groups.set(weekStart, movies)
  }
  return [...groups].sort(([left], [right]) => left.localeCompare(right)).map(([weekStart, movies]) => ({ weekStart, movies }))
}

export function formatReleaseWeek(value: string): string {
  return formatFrenchReleaseDate(releaseWeekStart(value))
}

export function formatFrenchReleaseDate(value: string): string {
  if (!isCalendarDate(value)) throw new Error('Invalid French release date')
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'UTC', day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(`${value}T12:00:00Z`))
}
