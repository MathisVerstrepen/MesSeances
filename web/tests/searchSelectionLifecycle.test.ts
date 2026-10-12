import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { isBroadTheaterSelection } from '../app/utils/cinemaSelection.ts'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { Ref } from 'vue'
import type { LocationQuery } from 'vue-router'
import type { SlotQuery, SlotResult } from '../app/types/api.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import * as date from '../app/utils/date.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as showtimeFilters from '../app/utils/showtimeFilters.ts'
import * as showtimeResults from '../app/utils/showtimeResults.ts'
import { partitionWatchlistResults } from '../app/utils/watchlistResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { absoluteSiteUrl } from '../app/utils/siteUrl.ts'
import { isValidShortLinkTarget } from '../app/utils/shortLinkTarget.ts'

// Execute the page's actual setup and route watchers without mounting Nuxt or a DOM.
const source = await readFile(
  new URL('../app/pages/recherche.vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'recherche.ts',
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
const submitDisabledExpression = source.match(
  /<button\s+type="submit"[\s\S]*?:disabled="([^"]+)"/,
)?.[1]
assert.ok(submitDisabledExpression, 'Search submit button must bind disabled')
const evaluateSubmitDisabled = new Function(
  'pending',
  'isLoading',
  'isInitialized',
  'draftTheaterIds',
  'draftBroadScope',
  'hasValidSelectedDate',
  `return (${submitDisabledExpression})`,
)

test('all-cinema label uses neutral ink while selection errors retain alert styling', () => {
  assert.match(
    source,
    /<p\s+v-else\s+class="[^"]*\btext-ink"\s*>\s*Tous les cinémas\s*<\/p>/,
  )
  assert.match(
    source,
    /id="theater-selection-message"\s+class="[^"]*\btext-red-700"\s+role="alert"/,
  )
})

interface PageState {
  form: { language: string; date: string }
  draftTheaterIds: Ref<string[]>
  draftBroadScope: Ref<boolean>
  hasValidSelectedDate: Ref<boolean>
  isLoading: Ref<boolean>
  isTheaterListOpen: Ref<boolean>
  pending: Ref<boolean>
  selectedOnly: Ref<boolean>
  watchlistOnly: Ref<boolean>
  selectedCount: Ref<number>
  visibleResults: Ref<ShowtimeResultViewModel[]>
  shareTarget: Ref<string | null>
  isFilterSheetOpen: Ref<boolean>
  timeRange: Ref<{ focusStart: () => void } | null>
  openTimeSlotEditor: (event: MouseEvent) => void
  isResolvingInitialSearch: Ref<boolean>
  initializePreferences: () => Promise<void>
  canonicalizeShowtimeSelection: () => Promise<void>
  setSelectedOnly: (event: { target: { checked: boolean } }) => Promise<void>
  toggleShowtimeSelection: (key: string) => Promise<void>
  clearShowtimeSelection: () => Promise<void>
  errorMessage: Ref<string>
  setResultGrouping: (grouping: string) => Promise<void>
  setResultLayout: (layout: string) => Promise<void>
  submitSearch: () => Promise<void>
  toggleSearchTheater: (theaterId: string) => void
}

const searchQuery = {
  theaters: 'ugc-25',
  date: '2026-09-13',
  start_after: '18:00',
  finish_before: '23:30',
}
const response: SlotResult[] = [12, 13].map((id, index) => ({
  showtime: {
    provider: 'ugc',
    id: `ugc-showing-${id}`,
    movie: {
      slug: `film-${id}`,
      title: `Film ${id}`,
      original_language: null,
      runtime_minutes: 90,
      updated_at: '2026-09-13T00:00:00Z',
    },
    start_time: `2026-09-13T${18 + index * 2}:00:00+02:00`,
    end_time: `2026-09-13T${20 + index * 2}:00:00+02:00`,
    estimated_end_time: null,
    estimated_end_ads_minutes: null,
    language: 'VF',
    format: '2D',
    room: '',
    booking_url: null,
  },
  theater: { provider: 'ugc', id: 'ugc-25', name: 'UGC', city: 'Lille' },
  poster_url: null,
  backdrop_url: null,
  effective_start_time: `2026-09-13T${18 + index * 2}:00:00+02:00`,
  effective_end_time: `2026-09-13T${20 + index * 2}:00:00+02:00`,
  buffer_ads_minutes: 15,
  slack_before_minutes: 0,
  slack_after_minutes: 0,
}))

function harness(
  query: LocationQuery,
  searchSlot: (query: SlotQuery) => Promise<SlotResult[]> = async () =>
    response,
) {
  const route = reactive({ query })
  const navigate = async ({ query: nextQuery }: { query: LocationQuery }) => {
    route.query = nextQuery
  }
  let searchCalls = 0
  const activeTheaterIds = ref(['ugc-25', 'ugc-46', 'ugc-45'])
  const isInitialized = ref(true)
  const selectionScopeKey = ref(0)
  const theaters = ref([
    { id: 'ugc-25', available_dates: ['2026-09-13'] },
    { id: 'ugc-46', available_dates: ['2026-09-13'] },
    { id: 'ugc-45', available_dates: ['2026-09-13'] },
    { id: 'other', available_dates: ['2026-09-14'] },
  ])
  const searchActionBarVisible = ref(false)
  let unmount = () => {}
  const watchlist = {
    owner: ref(''),
    ready: ref(false),
    scopeKey: ref(0),
    slugs: ref(new Set<string>()),
  }
  const bindings = {
    isBroadTheaterSelection,
    ...date,
    ...routeQuery,
    ...showtimeFilters,
    ...showtimeResults,
    partitionWatchlistResults,
    useWatchlist: () => watchlist,
    computed,
    reactive,
    ref,
    watch,
    nextTick,
    buildCompleteSearchShareTarget,
    absoluteSiteUrl,
    useRoute: () => route,
    useState: () => searchActionBarVisible,
    useRouter: () => ({ replace: navigate, push: navigate }),
    useMesSeancesApi: () => ({
      searchSlot: (query: SlotQuery) => {
        searchCalls++
        return searchSlot(query)
      },
    }),
    usePageCinemaSelection: () => ({
      theaters,
      activeTheaterIds,
      activeTheaters: ref([
        { id: 'ugc-25', available_dates: ['2026-09-13'] },
        { id: 'ugc-46', available_dates: ['2026-09-13'] },
        { id: 'ugc-45', available_dates: ['2026-09-13'] },
      ]),
      isInitialized,
      selectionScopeKey,
      isLoading: ref(false),
      error: ref(''),
      initialize: async () => {},
      isSharedSelectionDifferent: ref(false),
    }),
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    useSeoMeta: () => {},
    useHead: () => {},
    onMounted: () => {},
    onBeforeUnmount: (callback: () => void) => {
      unmount = callback
    },
    document: { removeEventListener: () => {} },
    getFrenchApiError: () => 'Recherche impossible',
  }
  const scope = effectScope()
  // SAFETY: The setup wrapper explicitly returns these page bindings; lifecycle tests exercise their runtime shape.
  const page = scope.run(() =>
    new Function(
      ...Object.keys(bindings),
      `${compiled}\nreturn { form, draftTheaterIds, draftBroadScope, hasValidSelectedDate, isLoading, isTheaterListOpen, pending, selectedOnly, watchlistOnly, selectedCount, visibleResults, shareTarget, isFilterSheetOpen, timeRange, openTimeSlotEditor, isResolvingInitialSearch, initializePreferences, canonicalizeShowtimeSelection, setSelectedOnly, toggleShowtimeSelection, clearShowtimeSelection, setResultGrouping, setResultLayout, submitSearch, toggleSearchTheater, errorMessage }`,
    )(...Object.values(bindings)),
  ) as PageState
  return {
    page,
    route,
    activeTheaterIds,
    isInitialized,
    selectionScopeKey,
    theaters,
    searchActionBarVisible,
    unmount: () => unmount(),
    watchlist,
    isSubmitDisabled: () =>
      evaluateSubmitDisabled(
        page.pending.value,
        page.isLoading.value,
        isInitialized.value,
        page.draftTheaterIds.value,
        page.draftBroadScope.value,
        page.hasValidSelectedDate.value,
      ),
    stop: () => scope.stop(),
    searchCalls: () => searchCalls,
  }
}

test('empty-result desktop recovery only focuses the existing editor, preserving every criterion and request', async () => {
  const f = harness(
    { ...searchQuery, language: 'VOSTFR', format: '2D', include_ads: '0' },
    async () => [],
  )
  try {
    await f.page.initializePreferences()
    await nextTick()
    f.page.watchlistOnly.value = true
    let focusCalls = 0
    f.page.timeRange.value = {
      focusStart: () => {
        focusCalls++
      },
    }
    const before = JSON.stringify({
      query: f.route.query,
      form: f.page.form,
      theaters: f.page.draftTheaterIds.value,
      broad: f.page.draftBroadScope.value,
      watchlist: f.page.watchlistOnly.value,
      selected: f.page.selectedOnly.value,
      share: f.page.shareTarget.value,
    })
    const requests = f.searchCalls()
    assert.equal(f.page.visibleResults.value.length, 0)
    // SAFETY: Desktop recovery does not read event fields; native Event suffices for this branch.
    f.page.openTimeSlotEditor(new Event('click') as MouseEvent)
    await nextTick()
    assert.equal(focusCalls, 1)
    assert.equal(f.page.isFilterSheetOpen.value, false)
    assert.equal(f.searchCalls(), requests)
    assert.equal(
      JSON.stringify({
        query: f.route.query,
        form: f.page.form,
        theaters: f.page.draftTheaterIds.value,
        broad: f.page.draftBroadScope.value,
        watchlist: f.page.watchlistOnly.value,
        selected: f.page.selectedOnly.value,
        share: f.page.shareTarget.value,
      }),
      before,
    )
  } finally {
    f.stop()
  }
})

test('watchlist updates only private projection, preserves share selection and never refetches', async () => {
  const f = harness(searchQuery)
  try {
    await f.page.initializePreferences()
    await nextTick()
    const calls = f.searchCalls()
    const share = f.page.shareTarget.value
    const query = JSON.stringify(f.route.query)
    f.watchlist.owner.value = 'alice'
    assert.deepEqual(f.page.visibleResults.value, [])
    f.watchlist.slugs.value = new Set(['film-13'])
    f.watchlist.ready.value = true
    assert.deepEqual(
      f.page.visibleResults.value.map((item) => item.movieSlug),
      ['film-13', 'film-12'],
    )
    f.page.watchlistOnly.value = true
    assert.deepEqual(
      f.page.visibleResults.value.map((item) => item.movieSlug),
      ['film-13'],
    )
    assert.equal(f.page.shareTarget.value, share)
    assert.equal(JSON.stringify(f.route.query), query)
    assert.equal(f.searchCalls(), calls)
    f.watchlist.slugs.value = new Set()
    assert.equal(f.page.visibleResults.value.length, 0)
    f.watchlist.owner.value = ''
    f.watchlist.scopeKey.value++
    assert.equal(f.page.watchlistOnly.value, false)
    assert.equal(f.page.visibleResults.value.length, 2)
    assert.equal(f.searchCalls(), calls)
  } finally {
    f.stop()
  }
})

test('complete empty theater marker searches catalog IDs despite saved preferences and shares the marker', async () => {
  const requests: SlotQuery[] = []
  const h = harness({ ...searchQuery, theaters: '' }, async (query) => {
    requests.push(query)
    return response
  })
  try {
    await h.page.initializePreferences()
    await nextTick()
    assert.equal(requests.length, 1)
    assert.equal(requests[0]?.theaters, 'ugc-25,ugc-46,ugc-45,other')
    assert.equal(h.route.query.theaters, '')
    assert.equal(
      new URL(
        h.page.shareTarget.value!,
        'https://messeances.fr',
      ).searchParams.get('theaters'),
      '',
    )
    assert.deepEqual(h.page.draftTheaterIds.value, [])
  } finally {
    h.stop()
  }
})

test('fresh empty scope submits catalog IDs without changing selection; empty partial draft stays invalid', async () => {
  const requests: SlotQuery[] = []
  const h = harness({}, async (query) => {
    requests.push(query)
    return response
  })
  try {
    h.activeTheaterIds.value = []
    await h.page.initializePreferences()
    assert.equal(h.isSubmitDisabled(), false)
    await h.page.submitSearch()
    await nextTick()
    assert.equal(h.route.query.theaters, '')
    assert.equal(requests[0]?.theaters, 'ugc-25,ugc-46,ugc-45,other')
    assert.deepEqual(h.page.draftTheaterIds.value, [])
    h.route.query = {}
    h.activeTheaterIds.value = ['ugc-25']
    await nextTick()
    assert.equal(h.isSubmitDisabled(), false)
    h.page.toggleSearchTheater('ugc-25')
    assert.equal(h.isSubmitDisabled(), true)
    const before = requests.length
    await h.page.submitSearch()
    assert.equal(requests.length, before)
    assert.equal(h.route.query.theaters, undefined)
  } finally {
    h.stop()
  }
})

test('broad fresh and restored searches compute first ten in catalog order without truncating scope', async (context) => {
  const catalog = Array.from({ length: 13 }, (_, index) => ({
    id: `cinema-${13 - index}`,
    available_dates: [searchQuery.date],
  }))
  const allIds = catalog.map((theater) => theater.id)
  for (const mode of [
    'fresh-empty',
    'fresh-full',
    'restored-empty',
    'restored-full',
    'subset',
  ] as const) {
    const requests: SlotQuery[] = []
    const subset = allIds.slice(1)
    const restoredIds =
      mode === 'subset' ? subset : mode === 'restored-full' ? allIds : []
    const h = harness(
      mode.startsWith('fresh')
        ? {}
        : {
            ...searchQuery,
            theaters: restoredIds.join(','),
          },
      async (query) => {
        requests.push(query)
        return response
      },
    )
    context.after(h.stop)
    h.theaters.value = catalog
    h.activeTheaterIds.value = mode === 'fresh-empty' ? [] : allIds
    const savedIds = [...h.activeTheaterIds.value]
    await h.page.initializePreferences()
    if (mode.startsWith('fresh')) await h.page.submitSearch()
    await settle()
    assert.equal(requests.length, 1, mode)
    assert.equal(
      requests[0]?.theaters,
      (mode === 'subset' ? subset : allIds.slice(0, 10)).join(','),
      mode,
    )
    assert.deepEqual(h.activeTheaterIds.value, savedIds, mode)
    assert.equal(
      h.route.query.theaters,
      mode.startsWith('fresh') ? '' : restoredIds.join(','),
      mode,
    )
  }
})

test('empty catalog blocks broad restored request instead of sending nationwide', async (context) => {
  const h = harness({ ...searchQuery, theaters: '', date: date.todayInParis() })
  context.after(h.stop)
  h.theaters.value = []
  h.activeTheaterIds.value = []
  await h.page.initializePreferences()
  await settle()
  assert.equal(h.searchCalls(), 0)
  assert.equal(h.page.pending.value, false)
  assert.match(h.page.errorMessage.value, /Aucun cinéma disponible/)
})

test('prompt clearance follows form visibility through submit, filter editing, bare route, restoration and unmount', async (context) => {
  const h = harness({})
  context.after(h.stop)
  assert.equal(h.searchActionBarVisible.value, true)
  await h.page.initializePreferences()
  await h.page.submitSearch()
  await settle()
  assert.equal(h.searchActionBarVisible.value, false)
  const appliedQuery = { ...h.route.query }
  h.page.isFilterSheetOpen.value = true
  assert.equal(h.searchActionBarVisible.value, true)
  h.page.isFilterSheetOpen.value = false
  assert.equal(h.searchActionBarVisible.value, false)
  h.route.query = {}
  await settle()
  assert.equal(h.searchActionBarVisible.value, true)
  h.route.query = appliedQuery
  await settle()
  assert.equal(h.searchActionBarVisible.value, false)
  h.route.query = {}
  await settle()
  h.unmount()
  assert.equal(h.searchActionBarVisible.value, false)
  const restored = harness(searchQuery)
  context.after(restored.stop)
  assert.equal(restored.searchActionBarVisible.value, false)
  await restored.page.initializePreferences()
  assert.equal(restored.searchActionBarVisible.value, false)
})

test('search submit button preserves loading, initialization and date guards for broad and selected scope', async (context) => {
  for (const theaterIds of [[], ['ugc-25']]) {
    const h = harness({})
    context.after(h.stop)
    h.activeTheaterIds.value = theaterIds
    await h.page.initializePreferences()
    assert.equal(h.isSubmitDisabled(), false)

    for (const guard of [h.page.pending, h.page.isLoading]) {
      guard.value = true
      assert.equal(h.isSubmitDisabled(), true)
      guard.value = false
      assert.equal(h.isSubmitDisabled(), false)
    }

    h.isInitialized.value = false
    assert.equal(h.isSubmitDisabled(), true)
    h.isInitialized.value = true
    assert.equal(h.isSubmitDisabled(), false)

    const validDate = h.page.form.date
    h.page.form.date = ''
    assert.equal(h.isSubmitDisabled(), true)
    h.page.form.date = validDate
    assert.equal(h.isSubmitDisabled(), false)
  }
})

test('incomplete, repeated, and malformed nationwide route scope remains invalid', async () => {
  for (const query of [
    { theaters: '', date: searchQuery.date },
    { ...searchQuery, theaters: ['', ''] },
    { ...searchQuery, theaters: ',' },
  ]) {
    const h = harness(query)
    try {
      await h.page.initializePreferences()
      assert.equal(h.searchCalls(), 0)
    } finally {
      h.stop()
    }
  }
})

test('selection invalidation fences a pending search without rewriting its URL until selection resolves', async () => {
  let resolve!: (value: SlotResult[]) => void
  const pending = new Promise<SlotResult[]>((yes) => {
    resolve = yes
  })
  const f = harness({ ...searchQuery }, () => pending)
  try {
    const initialized = f.page.initializePreferences()
    await nextTick()
    f.isInitialized.value = false
    f.activeTheaterIds.value = []
    f.selectionScopeKey.value++
    assert.equal(f.route.query.theaters, 'ugc-25')
    resolve(response)
    await initialized
    assert.equal(f.page.visibleResults.value.length, 0)
    assert.equal(f.page.pending.value, false)
  } finally {
    f.stop()
  }
})

test('theater filter starts collapsed and supports a draft subset of saved theaters', async (context) => {
  const { page, route, stop } = harness({})
  context.after(stop)

  await page.initializePreferences()
  assert.equal(page.isTheaterListOpen.value, false)
  assert.deepEqual(page.draftTheaterIds.value, ['ugc-25', 'ugc-46', 'ugc-45'])

  page.toggleSearchTheater('ugc-46')
  page.toggleSearchTheater('ugc-45')
  assert.deepEqual(page.draftTheaterIds.value, ['ugc-25'])

  page.toggleSearchTheater('ugc-46')
  assert.deepEqual(page.draftTheaterIds.value, ['ugc-25', 'ugc-46'])

  await page.submitSearch()
  assert.equal(route.query.theaters, 'ugc-25,ugc-46')
})

async function settle() {
  await nextTick()
  await nextTick()
}

test('shared selected-only state survives loading and validates only after results arrive', async (context) => {
  let resolveSearch!: (results: SlotResult[]) => void
  const search = new Promise<SlotResult[]>((resolve) => {
    resolveSearch = resolve
  })
  const { page, route, stop, searchCalls } = harness(
    { ...searchQuery, selected: 'u12,u99', selected_only: '1' },
    () => search,
  )
  context.after(stop)
  const initialization = page.initializePreferences()
  await settle()
  await page.canonicalizeShowtimeSelection()
  assert.equal(page.pending.value, true)
  assert.equal(page.selectedCount.value, 0)
  assert.equal(route.query.selected, 'u12,u99')
  assert.equal(route.query.selected_only, '1')
  const loadingShare = new URL(page.shareTarget.value!, 'https://messeances.fr')
  assert.equal(loadingShare.searchParams.get('selected'), 'u12,u99')
  assert.equal(loadingShare.searchParams.get('selected_only'), '1')

  resolveSearch(response)
  await initialization
  await settle()
  assert.equal(route.query.selected, 'u12')
  assert.equal(page.selectedOnly.value, true)
  assert.deepEqual(
    page.visibleResults.value.map((result) => result.key),
    ['ugc:ugc-showing-12'],
  )
  assert.equal(
    new URL(page.shareTarget.value!, 'https://messeances.fr').searchParams.get(
      'selected',
    ),
    'u12',
  )
  assert.equal(searchCalls(), 1)
})

test('complete route search stays in loading state until initial results resolve', async (context) => {
  let resolveSearch!: (results: SlotResult[]) => void
  const search = new Promise<SlotResult[]>((resolve) => {
    resolveSearch = resolve
  })
  const { page, stop } = harness(searchQuery, () => search)
  context.after(stop)

  assert.equal(page.isResolvingInitialSearch.value, true)
  const initialization = page.initializePreferences()
  await settle()
  assert.equal(page.isResolvingInitialSearch.value, true)
  assert.equal(page.pending.value, true)

  resolveSearch(response)
  await initialization
  assert.equal(page.isResolvingInitialSearch.value, false)
  assert.equal(page.pending.value, false)
})

test('toggle and same-search navigation preserve selections, draft filters and mobile sheet without refetching', async (context) => {
  const { page, route, stop, searchCalls } = harness({
    ...searchQuery,
    selected: 'u12',
    campaign: 'test',
  })
  context.after(stop)
  await page.initializePreferences()
  page.form.language = 'VOSTFR'
  page.isFilterSheetOpen.value = true
  await page.setSelectedOnly({ target: { checked: true } })
  await settle()
  assert.equal(route.query.selected_only, '1')
  assert.equal(route.query.campaign, 'test')
  assert.equal(page.form.language, 'VOSTFR')
  assert.equal(page.isFilterSheetOpen.value, true)
  assert.equal(page.visibleResults.value.length, 1)

  // Simulate back/forward navigation changing only selection display state.
  const enabledQuery = { ...route.query }
  route.query = { ...route.query, selected_only: '0' }
  await settle()
  assert.equal(route.query.selected_only, undefined)
  assert.equal(route.query.selected, 'u12')
  assert.equal(page.visibleResults.value.length, 2)
  route.query = enabledQuery
  await settle()
  assert.equal(page.selectedOnly.value, true)
  page.isFilterSheetOpen.value = false
  await page.setResultGrouping('chronological')
  await page.setResultLayout('boxes')
  await settle()
  assert.equal(route.query.selected_only, '1')
  assert.equal(route.query.selected, 'u12')
  assert.equal(searchCalls(), 1)
})

test('clearing or deselecting the last session removes selected-only mode and restores results', async (context) => {
  for (const clear of [true, false]) {
    const { page, route, stop } = harness({
      ...searchQuery,
      selected: 'u12',
      selected_only: '1',
    })
    context.after(stop)
    await page.initializePreferences()
    if (clear) await page.clearShowtimeSelection()
    else await page.toggleShowtimeSelection('ugc:ugc-showing-12')
    await settle()
    assert.equal(route.query.selected, undefined)
    assert.equal(route.query.selected_only, undefined)
    assert.equal(page.selectedCount.value, 0)
    assert.equal(page.selectedOnly.value, false)
    assert.equal(page.visibleResults.value.length, 2)
    await page.toggleShowtimeSelection('ugc:ugc-showing-13')
    await settle()
    assert.equal(page.selectedOnly.value, false)
  }
})

test('absent, stale and malformed selections or flags canonicalize to the normal view', async (context) => {
  for (const selection of [
    { selected_only: '1' },
    { selected: 'u99', selected_only: '1' },
    { selected: 'invalid', selected_only: '1' },
    { selected: 'u12', selected_only: 'true' },
    { selected: 'u12', selected_only: ['1', '1'] },
  ]) {
    const { page, route, stop } = harness({ ...searchQuery, ...selection })
    context.after(stop)
    await page.initializePreferences()
    await settle()
    assert.equal(route.query.selected_only, undefined)
    assert.equal(page.selectedOnly.value, false)
    assert.equal(page.visibleResults.value.length, 2)
  }
})

test('VOF hydrates, submits and shares as one scalar while preserving selection-reset semantics', async (context) => {
  const requests: SlotQuery[] = []
  const { page, route, stop } = harness(
    {
      ...searchQuery,
      language: 'VOF',
      selected: 'u12',
      selected_only: '1',
      campaign: 'test',
    },
    async (query) => {
      requests.push(query)
      return response
    },
  )
  context.after(stop)
  await page.initializePreferences()
  await settle()
  assert.equal(page.form.language, 'VOF')
  assert.equal(route.query.language, 'VOF')
  assert.equal(requests.at(-1)?.language, 'VOF')
  assert.deepEqual(
    new URL(
      page.shareTarget.value!,
      'https://messeances.fr',
    ).searchParams.getAll('language'),
    ['VOF'],
  )
  assert.equal(isValidShortLinkTarget(page.shareTarget.value!), true)
  await page.submitSearch()
  await settle()
  assert.equal(route.query.selected, 'u12')
  assert.equal(route.query.selected_only, '1')

  page.form.language = 'VF'
  await page.submitSearch()
  await settle()
  assert.equal(route.query.selected, undefined)
  assert.equal(route.query.selected_only, undefined)
  assert.equal(requests.at(-1)?.language, 'VF')
  await page.toggleShowtimeSelection('ugc:ugc-showing-12')
  await page.setSelectedOnly({ target: { checked: true } })
  await settle()
  page.form.language = 'VOF'
  await page.submitSearch()
  await settle()
  assert.equal(route.query.language, 'VOF')
  assert.equal(route.query.campaign, 'test')
  assert.equal(route.query.selected, undefined)
  assert.equal(route.query.selected_only, undefined)
  assert.equal(requests.at(-1)?.language, 'VOF')
  assert.deepEqual(
    new URL(
      page.shareTarget.value!,
      'https://messeances.fr',
    ).searchParams.getAll('language'),
    ['VOF'],
  )
})

test('changed search submission and bare-route navigation clear both selection query keys', async (context) => {
  const { page, route, stop } = harness({
    ...searchQuery,
    selected: 'u12',
    selected_only: '1',
  })
  context.after(stop)
  await page.initializePreferences()
  await page.submitSearch()
  assert.equal(route.query.selected_only, '1')
  page.form.language = 'VF'
  await page.submitSearch()
  await settle()
  assert.equal(route.query.selected, undefined)
  assert.equal(route.query.selected_only, undefined)
  route.query = { selected: 'u12', selected_only: '1' }
  await settle()
  assert.deepEqual(route.query, {})
  assert.equal(page.selectedCount.value, 0)
})
