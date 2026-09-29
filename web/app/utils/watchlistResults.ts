import type { ShowtimeResultViewModel } from '../types/showtimeResults'
import { sortShowtimeResults } from './showtimeResults.ts'

export function partitionWatchlistResults(
  results: readonly ShowtimeResultViewModel[],
  slugs: ReadonlySet<string>,
  only: boolean,
) {
  const saved: ShowtimeResultViewModel[] = []
  const others: ShowtimeResultViewModel[] = []
  for (const result of sortShowtimeResults(results)) {
    // Canonical movie grouping also joins screenings from different providers.
    const canonical = { ...result, movieKey: result.movieSlug }
    if (slugs.has(result.movieSlug)) saved.push(canonical)
    else if (!only) others.push(canonical)
  }
  return [
    { key: 'watchlist', title: 'Ma watchlist', results: saved },
    { key: 'others', title: 'Autres films', results: others },
  ].filter((section) => section.results.length > 0)
}
