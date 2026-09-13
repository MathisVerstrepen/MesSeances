import type { LocationQuery } from 'vue-router'
import type { UpcomingCatalogMovie, UpcomingMoviesQuery } from '../types/api.ts'
import { isCalendarDate } from './date.ts'
import { normalizeMovieGenres } from './movieCatalogFilters.ts'
import { positiveSafeInteger, singularQueryValue } from './routeQuery.ts'

export interface UpcomingFilters {
  month: string
  genres: string[]
  page: number
}

export function parseUpcomingFilters(query: LocationQuery): UpcomingFilters {
  const month = singularQueryValue(query.month) ?? ''
  const rawGenres = singularQueryValue(query.genres)?.split(',') ?? []
  return {
    month: /^\d{4}-(?:0[1-9]|1[0-2])$/.test(month) && isCalendarDate(`${month}-01`) ? month : '',
    genres: rawGenres.some(genre => !genre.trim()) ? [] : normalizeMovieGenres(rawGenres),
    page: positiveSafeInteger(singularQueryValue(query.page)) ?? 1
  }
}

export function upcomingRouteQuery(filters: UpcomingFilters): LocationQuery {
  const query: LocationQuery = {}
  if (filters.month) query.month = filters.month
  if (filters.genres.length) query.genres = normalizeMovieGenres(filters.genres).join(',')
  if (filters.page > 1) query.page = String(filters.page)
  return query
}

export function upcomingApiQuery(filters: UpcomingFilters): UpcomingMoviesQuery {
  return { month: filters.month || undefined, genres: filters.genres.length ? filters.genres.join(',') : undefined, page: filters.page, page_size: 24 }
}

export function groupUpcomingMovies(items: UpcomingCatalogMovie[]): Array<{ month: string; movies: UpcomingCatalogMovie[] }> {
  const groups = new Map<string, UpcomingCatalogMovie[]>()
  for (const movie of items) {
    if (!isCalendarDate(movie.french_release_date)) throw new Error('Invalid French release date')
    const month = movie.french_release_date.slice(0, 7)
    const movies = groups.get(month) ?? []
    movies.push(movie)
    groups.set(month, movies)
  }
  return [...groups].sort(([left], [right]) => left.localeCompare(right)).map(([month, movies]) => ({ month, movies }))
}

export function formatFrenchReleaseDate(value: string): string {
  if (!isCalendarDate(value)) throw new Error('Invalid French release date')
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'UTC', day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(`${value}T12:00:00Z`))
}

export function formatReleaseMonth(value: string): string {
  if (!isCalendarDate(`${value}-01`)) throw new Error('Invalid release month')
  return new Intl.DateTimeFormat('fr-FR', { timeZone: 'UTC', month: 'long', year: 'numeric' }).format(new Date(`${value}-01T12:00:00Z`))
}
