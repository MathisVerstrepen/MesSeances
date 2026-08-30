import type { CatalogMovie } from '../types/api.ts'
import { calendarDate } from './routeQuery.ts'

function nonblank(value: string | null | undefined): boolean {
  return value !== null && value !== undefined && value.trim().length > 0
}

export function hasSubstantialEvergreenMovieMetadata(movie: CatalogMovie): boolean {
  const hasExternalIdentity = nonblank(movie.poster_url)
    || (Number.isSafeInteger(movie.tmdb_id) && (movie.tmdb_id ?? 0) > 0)
    || nonblank(movie.imdb_id)

  return nonblank(movie.overview)
    && calendarDate(movie.release_date?.trim()) !== undefined
    && Array.isArray(movie.genres)
    && movie.genres.some(nonblank)
    && hasExternalIdentity
}

export function isIndexableMovie(movie: CatalogMovie, currentlyScreened: boolean): boolean {
  return currentlyScreened || hasSubstantialEvergreenMovieMetadata(movie)
}
