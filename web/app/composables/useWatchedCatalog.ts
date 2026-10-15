import type {
  MoviesQuery,
  MoviesResponse,
  UpcomingMoviesQuery,
  UpcomingMoviesResponse,
} from '~/types/api'
import type { WatchedSection } from '~/types/watched'
import {
  collectPublicPages,
  localMoviePage,
  localUpcomingPage,
} from '~/utils/watchedDiscovery'

// Scope-local public collection. Private projection never enters a Nuxt payload/cache.
export function useWatchedCatalog(section: WatchedSection) {
  const discovery = useWatchedDiscovery(section)
  const api = useMesSeancesApi()
  let cache:
    | { kind: 'movies'; key: string; value: MoviesResponse }
    | { kind: 'upcoming'; key: string; value: UpcomingMoviesResponse }
    | undefined
  let controller: AbortController | undefined
  let generation = 0
  let revision = ''
  function cancel() {
    generation++
    controller?.abort()
  }
  onBeforeUnmount(cancel)
  watch(
    discovery.watched.scopeKey,
    () => {
      cancel()
      cache = undefined
    },
    { flush: 'sync' },
  )
  async function observeCatalog(next: string) {
    if (!import.meta.client) return
    const changed = revision !== '' && revision !== next
    revision = next
    if (changed) {
      cache = undefined
      if (discovery.personal.value) await discovery.watched.refreshCatalog()
    }
    if (discovery.blocked.value) throw new Error('Watched state unavailable')
  }
  async function movies(query: MoviesQuery): Promise<MoviesResponse> {
    cancel()
    const current = generation
    controller = new AbortController()
    const signal = controller.signal
    const key = JSON.stringify({ ...query, page: 1, page_size: 100 })
    if (
      !discovery.hide.value &&
      !(cache?.kind === 'movies' && cache.key === key)
    ) {
      const result = await api.movies(query, signal)
      await observeCatalog(result.catalog_revision)
      if (current !== generation) throw new Error('Catalog cancelled')
      return result
    }
    const collected =
      cache?.kind === 'movies' && cache.key === key
        ? cache.value
        : await collectPublicPages(
            (page, signal) =>
              api.movies({ ...query, page, page_size: 100 }, signal),
            signal,
          )
    await observeCatalog(collected.catalog_revision)
    if (current !== generation) throw new Error('Catalog cancelled')
    cache = { kind: 'movies', key, value: collected }
    return localMoviePage(
      collected,
      discovery.hide.value ? discovery.watched.slugs.value : new Set(),
      query.page ?? 1,
      query.page_size ?? 24,
    )
  }
  async function upcoming(
    query: UpcomingMoviesQuery,
  ): Promise<UpcomingMoviesResponse> {
    cancel()
    const current = generation
    controller = new AbortController()
    const signal = controller.signal
    const key = `upcoming:${JSON.stringify({ ...query, page: 1 })}`
    if (
      !discovery.hide.value &&
      !(cache?.kind === 'upcoming' && cache.key === key)
    ) {
      const result = await api.upcomingMovies(query, signal)
      await observeCatalog(result.catalog_revision)
      if (current !== generation) throw new Error('Catalog cancelled')
      return result
    }
    const collected =
      cache?.kind === 'upcoming' && cache.key === key
        ? cache.value
        : await collectPublicPages(
            (page, signal) => api.upcomingMovies({ ...query, page }, signal),
            signal,
          )
    await observeCatalog(collected.catalog_revision)
    if (current !== generation) throw new Error('Catalog cancelled')
    cache = { kind: 'upcoming', key, value: collected }
    return localUpcomingPage(
      collected,
      discovery.hide.value ? discovery.watched.slugs.value : new Set(),
      query.page ?? 1,
    )
  }
  return { ...discovery, movies, upcoming, observeCatalog, cancel }
}
