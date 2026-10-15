import type {
  MoviesResponse,
  TimelineResponse,
  UpcomingMoviesResponse,
} from '../types/api.ts'
import type { WatchedPreferences, WatchedSection } from '../types/watched.ts'
import { groupUpcomingMovies } from './upcomingMovies.ts'

export function showWatched(
  preferences: WatchedPreferences,
  section: WatchedSection,
  temporary?: boolean,
): boolean {
  if (temporary !== undefined) return temporary
  const choice = preferences[`${section}_visibility`]
  return (
    (choice === 'inherit' ? preferences.default_visibility : choice) === 'show'
  )
}
export function withoutWatched<T>(
  items: readonly T[],
  slugs: ReadonlySet<string>,
  slug: (item: T) => string,
): T[] {
  return items.filter((item) => !slugs.has(slug(item)))
}
export function projectTimeline(
  value: TimelineResponse,
  slugs: ReadonlySet<string>,
): TimelineResponse {
  return {
    ...value,
    theaters: value.theaters
      .map((theater) => ({
        ...theater,
        showtimes: withoutWatched(
          theater.showtimes,
          slugs,
          (item) => item.movie.slug,
        ),
      }))
      .filter((theater) => theater.showtimes.length > 0),
  }
}
export function localMoviePage(
  value: MoviesResponse,
  slugs: ReadonlySet<string>,
  page: number,
  size: number,
): MoviesResponse {
  const items = withoutWatched(value.items, slugs, (item) => item.slug)
  const resolved = Math.min(
    Math.max(1, page),
    Math.max(1, Math.ceil(items.length / size)),
  )
  return {
    ...value,
    items: items.slice((resolved - 1) * size, resolved * size),
    total: items.length,
    page: resolved,
    page_size: size,
  }
}
export function localUpcomingPage(
  value: UpcomingMoviesResponse,
  slugs: ReadonlySet<string>,
  page: number,
): UpcomingMoviesResponse {
  const items = withoutWatched(value.items, slugs, (item) => item.slug)
  const groups = groupUpcomingMovies(items, value.view)
  const pages = Math.ceil(groups.length / 4)
  const resolved = Math.min(Math.max(1, page), Math.max(1, pages))
  return {
    ...value,
    items: groups
      .slice((resolved - 1) * 4, resolved * 4)
      .flatMap((group) => group.movies),
    total: items.length,
    total_weeks: groups.length,
    total_pages: pages,
    page: resolved,
  }
}

type PublicPage = MoviesResponse | UpcomingMoviesResponse
function boundary(value: PublicPage) {
  return 'total_weeks' in value
    ? JSON.stringify([
        value.catalog_revision,
        value.total,
        value.total_pages,
        value.total_weeks,
        value.view,
        value.year,
        value.month,
        value.window,
        value.timezone,
        value.available_years,
        value.available_months,
      ])
    : JSON.stringify([
        value.catalog_revision,
        value.total,
        value.screening_window,
        value.available_genres,
      ])
}
// Complete, sequential, finite walks. Public totals enumerate, never describe a private projection.
export async function collectPublicPages<T extends PublicPage>(
  fetchPage: (page: number, signal: AbortSignal) => Promise<T>,
  signal: AbortSignal,
): Promise<T> {
  const check = () => {
    if (signal.aborted) throw new Error('Catalog walk cancelled')
  }
  check()
  const first = await fetchPage(1, signal)
  check()
  if (
    first.page !== 1 ||
    (!('total_weeks' in first) &&
      (first.page_size < 1 || first.page_size > 100))
  )
    throw new Error('Invalid catalog page')
  if (
    !Number.isSafeInteger(first.total) ||
    first.total < 0 ||
    (first.total > 0 && !first.items.length)
  )
    throw new Error('Incomplete catalog walk')
  const pages =
    'total_weeks' in first
      ? first.total_pages
      : Math.ceil(first.total / first.page_size)
  if (!Number.isSafeInteger(pages) || pages < 0)
    throw new Error('Invalid catalog total')
  const expected = boundary(first)
  const items = [...first.items]
  for (let page = 2; page <= pages; page++) {
    check()
    const next = await fetchPage(page, signal)
    check()
    if (
      next.page !== page ||
      boundary(next) !== expected ||
      (!('total_weeks' in first) &&
        (!('page_size' in next) || next.page_size !== first.page_size))
    )
      throw new Error('Catalog changed during pagination')
    if (!next.items.length) throw new Error('Incomplete catalog walk')
    items.push(...next.items)
  }
  check()
  if (items.length < first.total) throw new Error('Incomplete catalog walk')
  const seen = new Set<string>()
  return {
    ...first,
    items: items.filter((item) => {
      if (seen.has(item.slug)) return false
      seen.add(item.slug)
      return true
    }),
  }
}
