import type { CatalogMovie, MovieSort } from '~/types/api'
import { compileMovieTitleSearch } from './movieTitleSearch.ts'

export const movieCatalogSortOptions = [
  { value: 'title_asc', label: 'Titre A–Z' },
  { value: 'title_desc', label: 'Titre Z–A' },
  { value: 'release_date_desc', label: 'Sorties récentes' },
  { value: 'runtime_asc', label: 'Durée croissante' },
  { value: 'runtime_desc', label: 'Durée décroissante' },
  { value: 'showtimes_desc', label: 'Plus de séances' },
] as const satisfies readonly { value: MovieSort; label: string }[]

export const movieCatalogSortValues = movieCatalogSortOptions.map(
  (option) => option.value,
)

export function movieOriginalTitleSubtitle(
  movie: Pick<CatalogMovie, 'title' | 'original_title'>,
): string {
  const originalTitle = movie.original_title?.trim() ?? ''
  const comparableTitle = (title: string) =>
    title.trim().replace(/\s+/gu, ' ').toLocaleLowerCase('fr-FR')

  return comparableTitle(originalTitle) === comparableTitle(movie.title)
    ? ''
    : originalTitle
}

function compareTitles(left: CatalogMovie, right: CatalogMovie): number {
  return (
    left.title.localeCompare(right.title, 'fr-FR', { sensitivity: 'base' }) ||
    left.slug.localeCompare(right.slug)
  )
}

export function filterAndSortCatalogMovies(
  movies: readonly CatalogMovie[],
  search: string,
  sort: MovieSort,
): CatalogMovie[] {
  const query = compileMovieTitleSearch(search)
  const filtered = query.blank()
    ? [...movies]
    : movies.filter((movie) => query.matches(movie.title, movie.original_title))

  return filtered.sort((left, right) => {
    if (sort === 'title_desc') return -compareTitles(left, right)
    if (sort === 'release_date_desc') {
      const comparison = (right.release_date ?? '').localeCompare(
        left.release_date ?? '',
      )
      return comparison || compareTitles(left, right)
    }
    if (sort === 'runtime_asc')
      return (
        left.runtime_minutes - right.runtime_minutes ||
        compareTitles(left, right)
      )
    if (sort === 'runtime_desc')
      return (
        right.runtime_minutes - left.runtime_minutes ||
        compareTitles(left, right)
      )
    if (sort === 'showtimes_desc')
      return (
        (right.showtime_count ?? 0) - (left.showtime_count ?? 0) ||
        compareTitles(left, right)
      )
    return compareTitles(left, right)
  })
}
