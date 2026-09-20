import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import {
  computed,
  effectScope,
  isRef,
  nextTick,
  reactive,
  ref,
  shallowRef,
  watch,
  type Ref,
} from 'vue'
import type { LocationQuery } from 'vue-router'
import type { MovieShowtimesQuery, StatisticsQuery } from '../app/types/api.ts'
import { queriesEqual } from '../app/utils/routeQuery.ts'
import * as statistics from '../app/utils/statistics.ts'
import * as history from '../app/utils/statisticsHistory.ts'
import { safeBackdropUrl } from '../app/utils/safeImageUrl.ts'

// Execute the real page setup with controlled transport and Nuxt lifecycle, as in upcomingScrollLifecycle.test.ts.
const source = await readFile(
  new URL('../app/pages/statistiques.vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'statistics.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const withoutImports = parsed.statements
  .filter((statement) => !ts.isImportDeclaration(statement))
  .map((statement) => statement.getFullText(parsed))
  .join('\n')
  .replaceAll('import.meta.server', 'false')
const compiled = ts.transpileModule(withoutImports, {
  compilerOptions: { target: ts.ScriptTarget.ESNext },
}).outputText

interface MovieResult {
  movie: { slug: string; title: string }
  backdrop_url?: string | null
  currently_screened: boolean
  theaters: never[]
}
interface TitleResult {
  film: string
  title: string
  backdrop?: string | null
}
interface Page {
  selectedFilmLabel: Ref<string>
  backdropUrl: Ref<string | null>
  backdropAvailable: Ref<boolean>
  backdropFailed: Ref<boolean>
  draft: Ref<ReturnType<typeof history.statisticsPageDraft>>
  error: Ref<string>
  apply: () => Promise<void>
  reset: () => Promise<void>
}

function movie(title = 'Le Voyage de Chihiro', slug = 'film-230'): MovieResult {
  return { movie: { slug, title }, currently_screened: false, theaters: [] }
}

async function settle() {
  for (let i = 0; i < 10; i++) await nextTick()
}

async function harness(
  query: LocationQuery,
  options: {
    historyError?: boolean
    lookup?: (film: string) => Promise<MovieResult>
  } = {},
) {
  const scope = effectScope()
  const route = reactive({ query })
  const movieCalls: { film: string; query: MovieShowtimesQuery }[] = []
  const historyCalls: StatisticsQuery[] = []
  let unmount = () => {}
  let titleData: Ref<TitleResult | undefined> | undefined
  const bindings = {
    ...statistics,
    ...history,
    queriesEqual,
    safeBackdropUrl,
    computed,
    ref,
    shallowRef,
    watch: (...args: Parameters<typeof watch>) =>
      scope.run(() => watch(...args)),
    useRoute: () => route,
    useRouter: () => ({
      push: async ({ query: next }: { query: LocationQuery }) => {
        route.query = next
      },
    }),
    useState: (_key: string, initialize: () => string) => ref(initialize()),
    useMesSeancesApi: () => ({
      historyStatistics: async (query: StatisticsQuery) => {
        historyCalls.push(query)
        if (options.historyError) throw new Error('history unavailable')
        return {
          options: null,
          limits: { options: undefined },
          totals: { showtimes: 0, movies: 0, theaters: 0, cities: 0 },
        }
      },
      movieShowtimes: async (film: string, query: MovieShowtimesQuery) => {
        movieCalls.push({ film, query })
        return options.lookup ? options.lookup(film) : movie()
      },
    }),
    useAsyncData: async <T>(
      key: string | Ref<string>,
      load: () => Promise<T>,
      options: { lazy?: boolean; server?: boolean },
    ) => {
      assert.notEqual(options.server, false, 'title must be SSR eligible')
      const data = shallowRef(await load())
      if (isRef(key)) {
        assert.equal(options.lazy, true)
        // SAFETY: The only reactive-key request in this page returns the title envelope.
        titleData = data as Ref<TitleResult | undefined>
        scope.run(() =>
          watch(
            key,
            async (current) => {
              // Keep the prior value pending, as Nuxt does on reactive-key changes.
              const result = await load()
              if (key.value === current) data.value = result
            },
            { flush: 'sync' },
          ),
        )
      }
      return { data, pending: ref(false) }
    },
    onMounted: () => {},
    onBeforeUnmount: (callback: () => void) => {
      unmount = callback
    },
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    absoluteSiteUrl: () => 'https://messeances.fr/statistiques',
    useSeoMeta: () => {},
    useHead: () => {},
    getApiErrorStatus: () => 503,
    getApiErrorCode: () => 'history_unavailable',
    getFrenchApiError: () => 'Indisponible',
    document: { removeEventListener: () => {} },
  }
  // SAFETY: The wrapper explicitly returns these actual page setup bindings.
  const page = (await new Function(
    ...Object.keys(bindings),
    `return (async () => { ${compiled}\nreturn { selectedFilmLabel, backdropUrl, backdropAvailable, backdropFailed, draft, error, apply, reset } })()`,
  )(...Object.values(bindings))) as Page
  return {
    page,
    route,
    movieCalls,
    historyCalls,
    titleData: titleData!,
    stop: () => {
      unmount()
      scope.stop()
    },
  }
}

test('selected title resolves with zero rows or failed history, including redirected and ended films', async (context) => {
  for (const historyError of [false, true]) {
    const h = await harness(
      { period: 'all', film: 'old-film-alias', city: 'no-intersection' },
      {
        historyError,
        lookup: async () => movie('Le Voyage de Chihiro', 'film-230'),
      },
    )
    context.after(h.stop)
    assert.equal(h.page.selectedFilmLabel.value, 'Le Voyage de Chihiro')
    assert.deepEqual(h.movieCalls, [
      {
        film: 'old-film-alias',
        query: { date: history.statisticsParisToday() },
      },
    ])
    assert.equal(h.route.query.film, 'old-film-alias')
    assert.equal(h.historyCalls[0]?.film, 'old-film-alias')
    assert.equal(Boolean(h.page.error.value), historyError)
  }
})

test('title requests use only valid film identity, independently of other invalid filters', async (context) => {
  for (const film of [
    undefined,
    null,
    '',
    '  ',
    [],
    ['film-230'],
    ['film-230', 'film-231'],
    'é'.repeat(101),
    '\0film',
    '\ud800',
  ]) {
    const h = await harness(film === undefined ? {} : { film })
    context.after(h.stop)
    assert.deepEqual(h.movieCalls, [], JSON.stringify(film))
    assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
  }
  const h = await harness({
    film: ' film-230 ',
    period: 'invalid',
    language: 'invalid',
  })
  context.after(h.stop)
  assert.equal(h.movieCalls[0]?.film, 'film-230')
  assert.equal(h.page.selectedFilmLabel.value, 'Le Voyage de Chihiro')
  assert.deepEqual(h.historyCalls, [])
})

test('unknown, unavailable and blank titles retain readable fallback and removable draft', async (context) => {
  for (const lookup of [
    async () => {
      throw new Error('not found')
    },
    async () => {
      throw new Error('offline')
    },
    async () => movie('  '),
  ]) {
    const h = await harness(
      { period: 'all', film: 'unknown-film', campaign: 'test' },
      { lookup },
    )
    context.after(h.stop)
    assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
    assert.equal(h.page.draft.value.film, 'unknown-film')
    h.page.draft.value.film = ''
    assert.equal(h.route.query.film, 'unknown-film')
    await h.page.apply()
    await settle()
    assert.equal(h.route.query.film, undefined)
    assert.equal(h.route.query.campaign, 'test')
    assert.equal(h.movieCalls.length, 1)
  }
})

test('other filter edits and period navigation do not refetch metadata; reset and history restore identity', async (context) => {
  const h = await harness({ film: 'film-230', period: 'all', campaign: 'test' })
  context.after(h.stop)
  h.page.draft.value.city = ['paris']
  h.page.draft.value.period = 'next30'
  await h.page.apply()
  await settle()
  assert.equal(h.movieCalls.length, 1)
  assert.equal(h.route.query.film, 'film-230')
  assert.equal(h.page.selectedFilmLabel.value, 'Le Voyage de Chihiro')
  const previous = { ...h.route.query }
  await h.page.reset()
  await settle()
  assert.deepEqual(h.route.query, { campaign: 'test' })
  assert.equal(h.page.draft.value.film, '')
  assert.equal(h.movieCalls.length, 1)
  h.route.query = previous
  await settle()
  assert.equal(h.page.draft.value.film, 'film-230')
  assert.equal(h.page.selectedFilmLabel.value, 'Le Voyage de Chihiro')
})

test('rapid film changes never show previous or late title and retain requested alias identity', async (context) => {
  let resolveSecond!: (value: MovieResult) => void
  let resolveThird!: (value: MovieResult) => void
  const h = await harness(
    { film: 'film-230' },
    {
      lookup: (film) => {
        if (film === 'film-231')
          return new Promise((resolve) => {
            resolveSecond = resolve
          })
        if (film === 'redirect-alias')
          return new Promise((resolve) => {
            resolveThird = resolve
          })
        return Promise.resolve(movie())
      },
    },
  )
  context.after(h.stop)
  h.route.query = { film: 'film-231' }
  assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
  h.route.query = { film: 'redirect-alias' }
  assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
  resolveThird(movie('Nouveau film', 'film-999'))
  await settle()
  assert.equal(h.page.selectedFilmLabel.value, 'Nouveau film')
  resolveSecond(movie('Réponse obsolète', 'film-231'))
  await settle()
  assert.equal(h.page.selectedFilmLabel.value, 'Nouveau film')
  // Explicitly model Nuxt carrying another key's payload to verify the page's own guard.
  h.titleData.value = { film: 'film-231', title: 'Réponse obsolète' }
  assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
  h.route.query = { film: ['film-231', 'redirect-alias'] }
  assert.equal(h.page.selectedFilmLabel.value, 'Titre indisponible')
  await settle()
  assert.equal(h.movieCalls.length, 3)
})

test('movie title stays plain text and never becomes route or API identity', async (context) => {
  const title =
    '<img src=x onerror=alert(1)> & Une très longue histoire'.repeat(10)
  const h = await harness(
    { film: 'film-230', period: 'all' },
    { lookup: async () => movie(title) },
  )
  context.after(h.stop)
  assert.equal(h.page.selectedFilmLabel.value, title)
  assert.match(source, /Film : \{\{ selectedFilmLabel \}\}/)
  assert.doesNotMatch(source, /v-html/)
  await h.page.apply()
  assert.deepEqual(h.route.query, { film: 'film-230', period: 'all' })
  assert.equal(h.historyCalls[0]?.film, 'film-230')
})

const backdrop = 'https://image.tmdb.org/t/p/w780/selected-film.jpg'

test('backdrop reuses selected metadata and existing validation, with no extra API lookup', async (context) => {
  for (const url of [
    backdrop,
    undefined,
    null,
    'https://untrusted.example/image.jpg',
    'https://image.tmdb.org/t/p/w500/poster.jpg',
  ]) {
    const h = await harness(
      { film: 'old-film-alias', period: 'all' },
      {
        historyError: true,
        lookup: async () => ({ ...movie(), backdrop_url: url }),
      },
    )
    context.after(h.stop)
    assert.equal(h.page.backdropUrl.value, url === backdrop ? backdrop : null)
    assert.equal(h.page.backdropAvailable.value, url === backdrop)
    assert.equal(h.movieCalls.length, 1)
    assert.equal(h.route.query.film, 'old-film-alias')
  }
})

test('draft removal immediately hides backdrop before Apply; metadata cannot restore it', async (context) => {
  const h = await harness(
    { film: 'film-230' },
    { lookup: async () => ({ ...movie(), backdrop_url: backdrop }) },
  )
  context.after(h.stop)
  assert.equal(h.page.backdropAvailable.value, true)
  h.page.draft.value.film = ''
  assert.equal(h.page.backdropAvailable.value, false)
  assert.equal(h.page.backdropUrl.value, null)
  assert.equal(h.route.query.film, 'film-230')
  h.titleData.value = { film: 'film-230', title: 'Loaded again', backdrop }
  assert.equal(h.page.backdropAvailable.value, false)
  await h.page.apply()
  await settle()
  assert.equal(h.route.query.film, undefined)
  assert.equal(h.movieCalls.length, 1)
})

test('failed image falls back and resets for changed image or film, including shared image URLs', async (context) => {
  const h = await harness(
    { film: 'film-230' },
    { lookup: async () => ({ ...movie(), backdrop_url: backdrop }) },
  )
  context.after(h.stop)
  h.page.backdropFailed.value = true
  assert.equal(h.page.backdropAvailable.value, false)
  h.page.draft.value.period = 'all'
  await h.page.apply()
  await settle()
  assert.equal(
    h.page.backdropAvailable.value,
    false,
    'same film/image does not retry on unrelated filters',
  )
  h.titleData.value = {
    film: 'film-230',
    title: 'New image',
    backdrop: 'https://image.tmdb.org/t/p/w780/new-image.jpg',
  }
  assert.equal(h.page.backdropAvailable.value, true)
  h.page.backdropFailed.value = true
  h.route.query = { film: 'film-231' }
  assert.equal(
    h.page.backdropAvailable.value,
    false,
    'old metadata hidden synchronously',
  )
  await settle()
  assert.equal(h.page.backdropAvailable.value, true)
  assert.equal(h.page.backdropUrl.value, backdrop)
  h.page.backdropFailed.value = true
  h.route.query = { film: 'film-232' }
  await settle()
  assert.equal(
    h.page.backdropAvailable.value,
    true,
    'different film retries even with identical image',
  )
})

test('route changes reject stale backdrops and late metadata, including route removal', async (context) => {
  let resolveNext!: (value: MovieResult) => void
  const h = await harness(
    { film: 'film-230' },
    {
      lookup: (film) =>
        film === 'film-231'
          ? new Promise((resolve) => {
              resolveNext = resolve
            })
          : Promise.resolve({ ...movie(), backdrop_url: backdrop }),
    },
  )
  context.after(h.stop)
  h.route.query = { film: 'film-231' }
  assert.equal(h.page.backdropAvailable.value, false)
  h.titleData.value = { film: 'film-230', title: 'Stale', backdrop }
  assert.equal(h.page.backdropAvailable.value, false)
  h.route.query = {}
  resolveNext({ ...movie('Late', 'film-231'), backdrop_url: backdrop })
  await settle()
  assert.equal(h.page.backdropAvailable.value, false)
  assert.equal(h.page.draft.value.film, '')
})

test('backdrop is decorative behind entire unclipped form, with contrast for labels and controls', () => {
  const form = source.match(/<form\b[\s\S]*?<\/form>/)![0]
  const image = form.match(/<img\b[^>]+>/)![0]
  assert.match(image, /v-if="backdropAvailable"/)
  assert.match(image, /alt=""\s+aria-hidden="true"/)
  assert.match(image, /absolute inset-0 -z-20 size-full object-cover/)
  assert.match(image, /@error="backdropFailed = true"/)
  assert.match(form, /relative isolate/)
  assert.match(form, /\[&_:focus-visible\]:outline-solid/)
  assert.match(form, /absolute inset-0 -z-10 bg-black\/80/)
  assert.doesNotMatch(form, /overflow-hidden|overflow-clip/)
  assert.match(form, /\[&_legend\]:text-white/)
  assert.match(
    form,
    /text-white hover:bg-white\/20 focus-visible:outline-white/,
  )
  assert.match(source, /bg-surface px-3 text-sm font-bold text-ink/)
})
