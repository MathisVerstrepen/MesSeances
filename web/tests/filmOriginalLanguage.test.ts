import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, nextTick, reactive, ref, type Ref } from 'vue'
import type { LocationQuery } from 'vue-router'
import type {
  MovieShowtimesResponse,
  ShowtimeLanguage,
} from '../app/types/api.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as filters from '../app/utils/showtimeFilters.ts'
import { isShowtimeFormat } from '../app/utils/formats.ts'
import { resolveShowtimeEnd } from '../app/utils/showtimeEnd.ts'
import { withSharedTheaterSelection } from '../app/utils/sharedTheaterSelection.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'

// Execute the actual film route/filter setup before its initial Nuxt SSR request.
const source = await readFile(
  new URL('../app/pages/film/[slug].vue', import.meta.url),
  'utf8',
)
const script = source
  .match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
  .split('\nhydrateRoute()\n')[0]!
const parsed = ts.createSourceFile(
  'film.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const withoutImports = parsed.statements
  .filter((statement) => !ts.isImportDeclaration(statement))
  .map((statement) => statement.getFullText(parsed))
  .join('\n')
const compiled = ts.transpileModule(withoutImports, {
  compilerOptions: { target: ts.ScriptTarget.ESNext },
}).outputText

interface PageState {
  activeLanguage: Ref<filters.FilmLanguageFilter>
  activeFilterSummary: Ref<string>
  languageOptions: Ref<
    readonly filters.ShowtimeFilterOption<filters.FilmLanguageFilter>[]
  >
  visibleShowtimeCount: Ref<number>
  selectedDate: Ref<string>
  hydrateRoute: () => LocationQuery
  filmQuery: () => LocationQuery
  applyRoute: () => Promise<void>
  normalizeDynamicFilters: () => Promise<void>
  resetFilters: () => void
}

function response(
  languages: ShowtimeLanguage[],
  original_language: string | null = 'en',
  dates = ['2027-06-27', '2027-06-28'],
): MovieShowtimesResponse {
  const movie = {
    slug: 'film-1',
    title: 'Film',
    original_language,
    runtime_minutes: 100,
    updated_at: '2027-06-27T00:00:00Z',
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    overview: null,
    release_date: null,
    french_release_date: null,
    genres: [],
  }
  return {
    movie,
    release_status: 'showing',
    currently_screened: true,
    date: dates[0] ?? '2027-06-27',
    available_dates: dates,
    backdrop_url: null,
    theaters: [
      {
        provider: 'ugc',
        id: 'ugc-25',
        slug: 'ugc-25',
        name: 'UGC',
        city: 'Lille',
        city_slug: 'lille',
        showtimes: languages.map((language, index) => ({
          provider: 'ugc',
          id: `ugc-showing-${index}`,
          movie,
          start_time: '2027-06-27T18:00:00+02:00',
          end_time: '2027-06-27T20:00:00+02:00',
          estimated_end_time: null,
          estimated_end_ads_minutes: null,
          language,
          format: '2D',
          room: '',
          booking_url: null,
        })),
      },
    ],
  }
}

function harness(
  query: LocationQuery,
  initialResponse: MovieShowtimesResponse,
) {
  const route = reactive({ params: { slug: 'film-1' }, query })
  const activeTheaterIds = ref(['ugc-25'])
  let currentResponse = initialResponse
  const bindings = {
    ...routeQuery,
    ...filters,
    computed,
    reactive,
    ref,
    nextTick,
    isShowtimeFormat,
    resolveShowtimeEnd,
    todayInParis: () => '2027-06-27',
    useRoute: () => route,
    useRouter: () => ({
      replace: async ({ query: nextQuery }: { query: LocationQuery }) => {
        route.query = nextQuery
      },
    }),
    useMesSeancesApi: () => ({ movieShowtimes: async () => currentResponse }),
    usePageCinemaSelection: () => ({
      activeTheaterIds,
      isInitialized: ref(true),
    }),
  }
  // SAFETY: The wrapper returns only these bindings from the actual page setup.
  const page = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { activeLanguage, activeFilterSummary, languageOptions, visibleShowtimeCount, selectedDate, hydrateRoute, filmQuery, applyRoute, normalizeDynamicFilters, resetFilters }`,
  )(...Object.values(bindings)) as PageState
  return {
    page,
    route,
    activeTheaterIds,
    setResponse: (value: MovieShowtimesResponse) => {
      currentResponse = value
    },
  }
}

for (const language of ['ORIGINAL', 'VOF'] as const) {
  test(`film accepts and shares ${language}, retaining it through empty matches, date and cinema changes`, async () => {
    const { page, route, activeTheaterIds, setResponse } = harness(
      { language, shared_theaters: 'ugc-25' },
      response(['VF']),
    )
    await page.applyRoute()
    assert.equal(page.activeLanguage.value, language)
    assert.equal(page.visibleShowtimeCount.value, 0)
    assert.equal(
      page.activeFilterSummary.value,
      language === 'ORIGINAL' ? 'Version originale (VOF & VOSTFR)' : 'VOF',
    )
    assert.deepEqual(
      page.languageOptions.value.map((option) => option.value),
      ['ALL', 'ORIGINAL', 'VOF', 'VF'],
    )
    await page.normalizeDynamicFilters()
    assert.equal(route.query.language, language)

    route.query = { ...route.query, date: '2027-06-28' }
    await page.applyRoute()
    assert.equal(page.selectedDate.value, '2027-06-28')
    assert.equal(route.query.language, language)

    setResponse(response([], null, []))
    activeTheaterIds.value = ['ugc-26']
    await page.applyRoute()
    assert.equal(page.visibleShowtimeCount.value, 0)
    assert.equal(page.activeLanguage.value, language)
    assert.equal(route.query.language, language)
    assert.deepEqual(
      page.languageOptions.value.map((option) => option.value),
      ['ALL', 'ORIGINAL', 'VOF'],
    )
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(page.filmQuery())) {
      const scalar = routeQuery.singularQueryValue(value)
      if (scalar !== undefined) query.set(key, scalar)
    }
    const shared = withSharedTheaterSelection(
      `/film/film-1?${query}`,
      activeTheaterIds.value,
    )!
    assert.equal(isValidShortLinkTarget(shared), true)
    const sharedQuery = new URL(shared, 'https://messeances.fr').searchParams
    assert.deepEqual(sharedQuery.getAll('language'), [language])
    assert.equal(sharedQuery.get('shared_theaters'), 'ugc-26')
    const reloaded = harness(
      Object.fromEntries(sharedQuery),
      response([], null, []),
    )
    await reloaded.page.applyRoute()
    assert.equal(reloaded.page.activeLanguage.value, language)
    assert.equal(reloaded.page.filmQuery().language, language)
    page.resetFilters()
    page.hydrateRoute()
    assert.equal(page.activeLanguage.value, 'ALL')
    assert.equal(route.query.language, undefined)
  })
}

test('film uses French-original metadata for matches and contextual VF labels without broadening concrete VF', async () => {
  const { page, route } = harness(
    { language: 'ORIGINAL' },
    response(['VF', 'VF_SME', 'VFSTF', 'VO', 'VOSTFR', ''], 'fr'),
  )
  await page.applyRoute()
  assert.equal(page.visibleShowtimeCount.value, 5)
  assert.deepEqual(
    page.languageOptions.value.find((option) => option.value === 'VF'),
    { value: 'VF', label: 'VF' },
  )
  assert.deepEqual(
    page.languageOptions.value.find((option) => option.value === 'VOF'),
    { value: 'VOF', label: 'VOF' },
  )
  route.query.language = 'VOF'
  page.hydrateRoute()
  assert.equal(page.visibleShowtimeCount.value, 3)
  assert.equal(page.activeFilterSummary.value, 'VOF')
  assert.equal(page.filmQuery().language, 'VOF')
  route.query.language = 'VF'
  page.hydrateRoute()
  assert.equal(page.activeFilterSummary.value, 'VOF')
  assert.equal(page.visibleShowtimeCount.value, 1)
  assert.equal(page.filmQuery().language, 'VF')
})

test('film still clears unavailable concrete languages and rejects unknown or lowercase query values', async () => {
  const { page, route } = harness({ language: 'VF_SME' }, response(['VF']))
  await page.applyRoute()
  assert.equal(route.query.language, undefined)
  for (const language of ['vof', 'UNKNOWN']) {
    route.query.language = language
    assert.equal(page.hydrateRoute().language, undefined)
    assert.equal(page.activeLanguage.value, 'ALL')
  }
})
