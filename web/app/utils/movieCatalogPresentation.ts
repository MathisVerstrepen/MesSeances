import type { CatalogMovie, MovieSort } from '~/types/api'

export const movieCatalogSortOptions = [
  { value: 'title_asc', label: 'Titre A–Z' },
  { value: 'title_desc', label: 'Titre Z–A' },
  { value: 'release_date_desc', label: 'Sorties récentes' },
  { value: 'runtime_asc', label: 'Durée croissante' },
  { value: 'runtime_desc', label: 'Durée décroissante' },
  { value: 'showtimes_desc', label: 'Plus de séances' }
] as const satisfies readonly { value: MovieSort, label: string }[]

export const movieCatalogSortValues = movieCatalogSortOptions.map((option) => option.value)

function normalizedTitle(value: string): string {
  return value.trim().normalize('NFD').replace(/\p{Diacritic}/gu, '').toLocaleLowerCase('fr-FR')
}

function compareTitles(left: CatalogMovie, right: CatalogMovie): number {
  return left.title.localeCompare(right.title, 'fr-FR', { sensitivity: 'base' }) || left.slug.localeCompare(right.slug)
}

export function filterAndSortCatalogMovies(
  movies: readonly CatalogMovie[],
  search: string,
  sort: MovieSort
): CatalogMovie[] {
  const query = normalizedTitle(search)
  const filtered = query
    ? movies.filter((movie) => normalizedTitle(movie.title).includes(query))
    : [...movies]

  return filtered.sort((left, right) => {
    if (sort === 'title_desc') return -compareTitles(left, right)
    if (sort === 'release_date_desc') {
      const comparison = (right.release_date ?? '').localeCompare(left.release_date ?? '')
      return comparison || compareTitles(left, right)
    }
    if (sort === 'runtime_asc') return left.runtime_minutes - right.runtime_minutes || compareTitles(left, right)
    if (sort === 'runtime_desc') return right.runtime_minutes - left.runtime_minutes || compareTitles(left, right)
    if (sort === 'showtimes_desc') return (right.showtime_count ?? 0) - (left.showtime_count ?? 0) || compareTitles(left, right)
    return compareTitles(left, right)
  })
}
