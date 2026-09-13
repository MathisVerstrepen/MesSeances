import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { MovieShowtimesResponse, UpcomingCatalogMovie } from '../app/types/api.ts'
import { getFrenchApiError, useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { loadInitialFilmSchedule } from '../app/utils/filmInitialSchedule.ts'
import { buildFilmJsonLd } from '../app/utils/filmJsonLd.ts'
import { formatFrenchReleaseDate, formatReleaseMonth, formatReleaseWeek, groupUpcomingMovies, parseUpcomingFilters, releaseWeekStart, upcomingApiQuery, upcomingRouteQuery } from '../app/utils/upcomingMovies.ts'
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

test('assigns every weekday to its prior-or-same Wednesday, with Tuesday closing the week', () => {
  for (const date of ['2026-09-16', '2026-09-17', '2026-09-18', '2026-09-19', '2026-09-20', '2026-09-21', '2026-09-22']) {
    assert.equal(releaseWeekStart(date), '2026-09-16', date)
  }
  assert.equal(releaseWeekStart('2026-09-15'), '2026-09-09')
  assert.equal(releaseWeekStart('2026-09-23'), '2026-09-23')
})

test('groups chronologically without inventing empty weeks or changing within-week backend order or actual dates', () => {
  const films = [movie('2027-01-13', 'film-2'), movie('2027-01-05', 'film-3'), movie('2026-12-30', 'film-1'), movie('2026-12-30', 'film-4')]
  const original = structuredClone(films)
  films.forEach(Object.freeze)
  Object.freeze(films)
  const groups = groupUpcomingMovies(films)
  assert.deepEqual(groups.map(group => [group.weekStart, group.movies.map(item => item.slug)]), [
    ['2026-12-30', ['film-3', 'film-1', 'film-4']], ['2027-01-13', ['film-2']]
  ])
  assert.equal(groups[0]?.movies[0], films[1])
  assert.deepEqual(films, original)
  assert.deepEqual(groupUpcomingMovies([]), [])
})

test('weeks cross month, year, leap day and DST boundaries without timezone drift', () => {
  const cases = [
    ['2026-09-30', '2026-09-30'], ['2026-10-01', '2026-09-30'], ['2026-10-06', '2026-09-30'],
    ['2027-01-01', '2026-12-30'], ['2027-01-05', '2026-12-30'], ['2027-01-06', '2027-01-06'],
    ['2028-02-29', '2028-02-23'], ['2028-03-01', '2028-03-01'],
    ['2026-10-25', '2026-10-21'], ['2026-10-27', '2026-10-21'], ['2026-10-28', '2026-10-28'],
    ['2027-03-28', '2027-03-24'], ['2027-03-30', '2027-03-24'], ['2027-03-31', '2027-03-31']
  ] as const
  const previousTimezone = process.env.TZ
  try {
    for (const timezone of ['UTC', 'Europe/Paris', 'America/Los_Angeles', 'Pacific/Kiritimati']) {
      process.env.TZ = timezone
      for (const [date, expected] of cases) assert.equal(releaseWeekStart(date), expected, `${timezone}: ${date}`)
      assert.equal(formatReleaseWeek('2026-09-16'), '16 septembre 2026')
      assert.equal(formatReleaseWeek('2027-01-01'), '30 décembre 2026')
      assert.equal(formatFrenchReleaseDate('2026-10-01'), '1 octobre 2026')
    }
  } finally {
    if (previousTimezone === undefined) delete process.env.TZ
    else process.env.TZ = previousTimezone
  }
})

test('groups only supplied page and month-filter items even when the same week spans both', () => {
  const september = movie('2026-09-30', 'film-1')
  const october = movie('2026-10-01', 'film-2')
  assert.deepEqual(groupUpcomingMovies([september, october]), [{ weekStart: '2026-09-30', movies: [september, october] }])
  assert.deepEqual(groupUpcomingMovies([september]), [{ weekStart: '2026-09-30', movies: [september] }])
  assert.deepEqual(groupUpcomingMovies([october]), [{ weekStart: '2026-09-30', movies: [october] }])
})

test('rejects missing or invalid verified dates rather than falling back to general release dates', () => {
  for (const date of ['', '2027-02-29', '2026-04-31', '2026-13-01', '2026-09-00', '2026-9-16', '2026-09-16T00:00:00Z', null, undefined]) {
    // SAFETY: Deliberately pass malformed API fields to verify runtime rejection, including absent dates.
    const invalid = date as string
    assert.throws(() => releaseWeekStart(invalid), /Invalid French release date/)
    assert.throws(() => groupUpcomingMovies([movie(invalid)]), /Invalid French release date/)
    assert.throws(() => formatReleaseWeek(invalid), /Invalid French release date/)
  }
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
  assert.match(page, /:key="group\.weekStart"/)
  assert.match(page, /:aria-labelledby="`week-\$\{group\.weekStart\}`"/)
  assert.match(page, /:id="`week-\$\{group\.weekStart\}`"/)
  assert.match(page, /formatReleaseWeek\(group\.weekStart\)/)
  assert.match(page, /Sortie le <time :datetime="movie\.french_release_date">\{\{ formatFrenchReleaseDate\(movie\.french_release_date\) \}\}/)
  assert.match(page, /semaine par semaine/)
  assert.doesNotMatch(page, /group\.month|mois par mois/)
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
