import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { effectScope, ref, watch } from 'vue'
import type { MoviesQuery, MoviesResponse } from '../app/types/api.ts'
import type { useWatchedCatalog } from '../app/composables/useWatchedCatalog.ts'
import * as discovery from '../app/utils/watchedDiscovery.ts'

const inventory: MoviesResponse['items'] = Array.from(
  { length: 2140 },
  (_, index) => ({
    slug: `film-${index}`,
    title: `Film ${index}`,
    runtime_minutes: 90,
    poster_url: null,
    original_language: null,
    updated_at: '2026-10-17T12:00:00Z',
    tmdb_id: null,
    imdb_id: null,
    metacritic_id: null,
    overview: null,
    release_date: null,
    french_release_date: null,
    genres: [],
  }),
)
function publicPage(
  page: number,
  size: number,
  items = inventory,
): MoviesResponse {
  return {
    items: items.slice((page - 1) * size, page * size),
    total: items.length,
    page,
    page_size: size,
    available_genres: [],
    generated_at: '2026-10-17T12:00:00Z',
    catalog_revision: 'r1',
  }
}
const slugs = (value: { items: MoviesResponse['items'] }) =>
  value.items.map((item) => item.slug)

interface CatalogModule {
  useWatchedCatalog?: typeof useWatchedCatalog
}
async function catalogFixture() {
  const hide = ref(true)
  const scopeKey = ref(0)
  const watched = ref(new Set(['film-0']))
  const calls: MoviesQuery[] = []
  let fetch = async (query: MoviesQuery, _signal: AbortSignal) =>
    publicPage(query.page ?? 1, query.page_size ?? 24)
  let refresh = async () => {}
  const exports: CatalogModule = {}
  const source = await readFile(
    new URL('../app/composables/useWatchedCatalog.ts', import.meta.url),
    'utf8',
  )
  const scope = effectScope(true)
  runInNewContext(
    ts.transpileModule(source.replaceAll('import.meta.client', 'true'), {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      watch,
      AbortController,
      onBeforeUnmount() {},
      useWatchedDiscovery: () => ({
        hide,
        personal: ref(true),
        blocked: ref(false),
        watched: { scopeKey, slugs: watched, refreshCatalog: () => refresh() },
      }),
      useMesSeancesApi: () => ({
        movies: (query: MoviesQuery, signal: AbortSignal) => {
          calls.push(query)
          return fetch(query, signal)
        },
      }),
      require: () => discovery,
    },
  )
  return {
    catalog: scope.run(() => exports.useWatchedCatalog!('home'))!,
    hide,
    scopeKey,
    calls,
    setFetch(fn: typeof fetch) {
      fetch = fn
    },
    setRefresh(fn: typeof refresh) {
      refresh = fn
    },
    stop: () => scope.stop(),
  }
}

test('2140-film preview fetches one page, returns six ordered distinct unwatched films without a total or complete-cache pollution', async (t) => {
  const f = await catalogFixture()
  t.after(f.stop)
  const query = { currently_screened: true, sort: 'showtimes_desc' as const }
  const preview = await f.catalog.moviePreview(query, 6)
  assert.deepEqual(
    slugs(preview),
    inventory.slice(1, 7).map((item) => item.slug),
  )
  assert.deepEqual(Object.keys(preview).sort(), ['catalog_revision', 'items'])
  assert.equal(f.calls.length, 1)
  assert.equal(f.calls[0]!.page_size, 12)
  assert.equal(f.calls[0]!.sort, 'showtimes_desc')
  const full = await f.catalog.movies({ ...query, page: 1, page_size: 24 })
  assert.equal(full.total, 2139)
  assert.equal(full.items.length, 24)
  assert.equal(f.calls.length, 23)
  assert.equal(f.calls.at(-1)!.page, 22)
  assert.ok(f.calls.slice(1).every((query) => query.page_size === 100))
})

test('preview crosses hidden-only pages, deduplicates by slug and stops as soon as six slots fill', async () => {
  const items = [0, 0, 1, 1, 2, 3, 3, 4, 5, 6, 7, 8].map(
    (index) => inventory[index]!,
  )
  const calls: number[] = []
  const value = await discovery.collectMoviePreview(
    async (page) => {
      calls.push(page)
      return publicPage(page, 2, items)
    },
    new Set(['film-0']),
    6,
    new AbortController().signal,
  )
  assert.deepEqual(
    slugs(value),
    [1, 2, 3, 4, 5, 6].map((index) => `film-${index}`),
  )
  assert.deepEqual(calls, [1, 2, 3, 4, 5])
})

test('preview exhausts to fewer, all-hidden or empty results without inventing visible totals', async () => {
  for (const [items, hidden, expected] of [
    [inventory.slice(0, 3), new Set(['film-0']), ['film-1', 'film-2']],
    [inventory.slice(0, 3), new Set(['film-0', 'film-1', 'film-2']), []],
    [[], new Set<string>(), []],
  ] as const) {
    const calls: number[] = []
    const result = await discovery.collectMoviePreview(
      async (page) => {
        calls.push(page)
        return publicPage(page, 2, [...items])
      },
      hidden,
      6,
      new AbortController().signal,
    )
    assert.deepEqual(slugs(result), expected)
    assert.equal(calls.length, Math.max(1, Math.ceil(items.length / 2)))
    assert.equal('total' in result, false)
  }
})

test('preview rejects revision/boundary drift and incomplete pages before returning a filled prefix', async () => {
  for (const change of [
    { catalog_revision: 'r2' },
    { total: 2141 },
    { page_size: 3 },
    { page: 99 },
    { available_genres: ['Drame'] },
    { items: [] },
  ]) {
    await assert.rejects(
      discovery.collectMoviePreview(
        async (page) =>
          page === 2
            ? { ...publicPage(page, 2), ...change }
            : publicPage(page, 2),
        new Set(['film-0', 'film-1']),
        1,
        new AbortController().signal,
      ),
    )
  }
})

test('preview rejects pre-abort and abort after a fetch even if the fetch ignores its signal', async () => {
  const before = new AbortController()
  before.abort()
  await assert.rejects(
    discovery.collectMoviePreview(
      async () => {
        assert.fail('aborted walk must not fetch')
      },
      new Set(),
      6,
      before.signal,
    ),
    /cancelled/,
  )
  const during = new AbortController()
  await assert.rejects(
    discovery.collectMoviePreview(
      async (page) => {
        during.abort()
        return publicPage(page, 100)
      },
      new Set(),
      6,
      during.signal,
    ),
    /cancelled/,
  )
})

test('Show keeps the small public request and superseding/owner invalidation fence late previews', async (t) => {
  const f = await catalogFixture()
  t.after(f.stop)
  f.hide.value = false
  assert.deepEqual(
    slugs(await f.catalog.moviePreview({}, 6)),
    inventory.slice(0, 6).map((item) => item.slug),
  )
  assert.equal(f.calls[0]!.page_size, 6)
  f.hide.value = true
  for (const invalidate of [
    () => f.catalog.cancel(),
    () => {
      f.scopeKey.value++
    },
  ]) {
    let release!: (value: MoviesResponse) => void
    let signal!: AbortSignal
    f.setFetch(async (_query, nextSignal) => {
      signal = nextSignal
      return new Promise((resolve) => {
        release = resolve
      })
    })
    const pending = f.catalog.moviePreview({}, 6)
    const rejected = assert.rejects(pending, /cancelled/)
    invalidate()
    assert.equal(signal.aborted, true)
    release(publicPage(1, 12))
    await rejected
  }
})

test('generation cancellation during catalog-revision revalidation cannot publish a stale preview', async (t) => {
  const f = await catalogFixture()
  t.after(f.stop)
  await f.catalog.moviePreview({}, 6)
  let refreshes = 0
  f.setRefresh(async () => {
    refreshes++
    f.catalog.cancel()
  })
  f.setFetch(async (query) => ({
    ...publicPage(query.page ?? 1, query.page_size ?? 12),
    catalog_revision: 'r2',
  }))
  await assert.rejects(f.catalog.moviePreview({}, 6), /cancelled/)
  assert.equal(refreshes, 1)
})
