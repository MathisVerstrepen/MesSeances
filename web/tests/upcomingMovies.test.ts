import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { MovieShowtimesResponse, UpcomingCatalogMovie } from '../app/types/api.ts'
import { getFrenchApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { loadInitialFilmSchedule } from '../app/utils/filmInitialSchedule.ts'
import { buildFilmJsonLd } from '../app/utils/filmJsonLd.ts'
import { formatFrenchReleaseDate, formatReleaseMonth, groupUpcomingMovies, parseUpcomingFilters, upcomingApiQuery, upcomingRouteQuery } from '../app/utils/upcomingMovies.ts'
import { upcomingSitemapEntry } from '../server/utils/sitemap.ts'

function movie(date: string, slug = 'film-1'): UpcomingCatalogMovie {
  return {
    slug, title: 'Film à venir', runtime_minutes: 0, updated_at: '2026-09-13T12:00:00Z',
    poster_url: null, tmdb_id: 42, imdb_id: null, overview: null, genres: ['Drame'],
    release_date: '2000-01-01', french_release_date: date
  }
}

test('normalizes route keys, rejects duplicate scalar filters, and keeps OR genres and safe pages', () => {
  const filters = parseUpcomingFilters({ month: '2027-02', genres: 'Drame, Animation,drame', page: '02', theaters: 'ugc-1' })
  assert.deepEqual(filters, { month: '2027-02', genres: ['Animation', 'Drame'], page: 2 })
  assert.deepEqual(upcomingRouteQuery(filters), { month: '2027-02', genres: 'Animation,Drame', page: '2' })
  assert.deepEqual(upcomingApiQuery(filters), { month: '2027-02', genres: 'Animation,Drame', page: 2, page_size: 24 })
  for (const month of ['2026-13', '2026-1', '2026-02-01', ['2026-01', '2026-02']]) {
    assert.equal(parseUpcomingFilters({ month }).month, '')
  }
  assert.deepEqual(parseUpcomingFilters({ genres: ['Drame', 'Action'], page: '9007199254740992' }), { month: '', genres: [], page: 1 })
  assert.deepEqual(upcomingRouteQuery({ month: '', genres: [], page: 1 }), {})
})

test('groups each page by verified French month, keeps within-month backend ordering', () => {
  const films = [movie('2027-01-06', 'film-2'), movie('2026-12-16', 'film-3'), movie('2026-12-23', 'film-1')]
  assert.deepEqual(groupUpcomingMovies(films).map(group => [group.month, group.movies.map(item => item.slug)]), [
    ['2026-12', ['film-3', 'film-1']], ['2027-01', ['film-2']]
  ])
  assert.deepEqual(groupUpcomingMovies([]), [])
  assert.throws(() => groupUpcomingMovies([movie('2027-02-29')]), /Invalid French release date/)
  assert.throws(() => groupUpcomingMovies([movie('')]), /Invalid French release date/)
})

test('date-only labels retain year, leap day and DST calendar dates without clock-based drift', () => {
  assert.equal(formatFrenchReleaseDate('2028-02-29'), '29 février 2028')
  assert.equal(formatFrenchReleaseDate('2026-10-25'), '25 octobre 2026')
  assert.equal(formatFrenchReleaseDate('2027-03-28'), '28 mars 2027')
  assert.equal(formatReleaseMonth('2027-09'), 'septembre 2027')
  assert.throws(() => formatReleaseMonth('2026-13'))
})

interface UpcomingFetchOptions {
  query: Record<string, string | number>
  retry: false
}

test('API composable sends only frozen upcoming query and disables retry', async () => {
  const calls: Array<{ url: string; options: UpcomingFetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: UpcomingFetchOptions) => { calls.push({ url, options }); return Promise.resolve({}) }
  })
  await useMesSeancesApi().upcomingMovies({ month: '2026-10', page: 1, page_size: 24 })
  assert.deepEqual(calls, [{ url: 'http://localhost:8080/api/v1/movies/upcoming', options: { query: { month: '2026-10', page: 1, page_size: 24 }, retry: false } }])
  assert.match(getFrenchApiError({ data: { error: { code: 'upcoming_unavailable', message: 'unavailable' } } }), /pas encore disponibles/)
})

test('catalog-only SSR preserves upcoming and withdrawn states with no invented screenings', async () => {
  for (const status of ['upcoming', 'unavailable'] as const) {
    const response: MovieShowtimesResponse = {
      movie: { ...movie('2026-10-07'), french_release_date: status === 'upcoming' ? '2026-10-07' : null },
      release_status: status, currently_screened: false, backdrop_url: null, date: '2026-09-13', available_dates: [], theaters: []
    }
    const result = await loadInitialFilmSchedule({
      requestedDate: '2026-09-13', today: '2026-09-13',
      fetchScoped: async () => response, fetchNationwide: async () => response
    })
    assert.equal(result.scoped.release_status, status)
    assert.equal(result.selectedDate, '2026-09-13')
    const json = JSON.stringify(buildFilmJsonLd(response, { movieUrl: 'https://messeances.fr/film/film-1', siteUrl: 'https://messeances.fr' }))
    assert.doesNotMatch(json, /ScreeningEvent/)
  }
})

test('sitemap uses real upcoming publication timestamp only at canonical page', async () => {
  assert.deepEqual(upcomingSitemapEntry('2026-09-13T12:00:00Z'), { path: '/films/prochainement', lastmod: '2026-09-13T12:00:00Z' })
  assert.throws(() => upcomingSitemapEntry(''))
  const handler = await readFile(new URL('../server/routes/sitemaps/films.xml.ts', import.meta.url), 'utf8')
  assert.match(handler, /publication\.error\.code === 'upcoming_unavailable'/)
  assert.match(handler, /upcomingSitemapEntry\(publication\.generated_at\)/)
})

test('SSR page and detail use exact states, shared cards and no catalog-only preference blocker', async () => {
  const page = await readFile(new URL('../app/pages/films/prochainement.vue', import.meta.url), 'utf8')
  const detail = await readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8')
  const tabs = await readFile(new URL('../app/components/FilmCatalogTabs.vue', import.meta.url), 'utf8')
  assert.match(page, /await useAsyncData/)
  assert.match(page, /currentRequest !== requestId/)
  assert.match(page, /router\.push/)
  assert.match(page, /Aucune sortie annoncée/)
  assert.match(page, /Aucun film ne correspond aux filtres/)
  assert.doesNotMatch(page, /useCinemaPreferences|todayInParis|\.release_date/)
  assert.match(tabs, /aria-current/)
  assert.doesNotMatch(tabs, /role="tab/)
  assert.match(detail, /release_status === 'upcoming'/)
  assert.match(detail, /release_status === 'ended'/)
  assert.doesNotMatch(detail, /isEndedFilm\.value = !/)
  assert.match(detail, /Séances à venir/)
  assert.match(detail, /Aucune séance disponible/)
  assert.match(detail, /if \(schedule\.value && !schedule\.value\.currently_screened\)/)
  assert.match(detail, /v-if="schedule\.movie\.runtime_minutes > 0"/)
})
