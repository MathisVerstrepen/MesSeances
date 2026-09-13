import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { Ref } from 'vue'
import type { LocationQuery } from 'vue-router'
import type { SlotResult } from '../app/types/api.ts'
import type { ShowtimeResultViewModel } from '../app/types/showtimeResults.ts'
import * as date from '../app/utils/date.ts'
import * as routeQuery from '../app/utils/routeQuery.ts'
import * as showtimeFilters from '../app/utils/showtimeFilters.ts'
import * as showtimeResults from '../app/utils/showtimeResults.ts'
import { buildCompleteSearchShareTarget } from '../app/utils/searchShareTarget.ts'
import { absoluteSiteUrl } from '../app/utils/siteUrl.ts'

// Execute the page's actual setup and route watchers without mounting Nuxt or a DOM.
const source = await readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile('recherche.ts', script, ts.ScriptTarget.Latest, true)
const withoutImports = parsed.statements.filter((statement) => !ts.isImportDeclaration(statement)).map((statement) => statement.getFullText(parsed)).join('\n')
const compiled = ts.transpileModule(withoutImports, { compilerOptions: { target: ts.ScriptTarget.ESNext } }).outputText

interface PageState {
  form: { language: string }
  pending: Ref<boolean>
  selectedOnly: Ref<boolean>
  selectedCount: Ref<number>
  visibleResults: Ref<ShowtimeResultViewModel[]>
  shareTarget: Ref<string | null>
  isFilterSheetOpen: Ref<boolean>
  initializePreferences: () => Promise<void>
  canonicalizeShowtimeSelection: () => Promise<void>
  setSelectedOnly: (event: { target: { checked: boolean } }) => Promise<void>
  toggleShowtimeSelection: (key: string) => Promise<void>
  clearShowtimeSelection: () => Promise<void>
  setResultGrouping: (grouping: string) => Promise<void>
  setResultLayout: (layout: string) => Promise<void>
  submitSearch: () => Promise<void>
}

const searchQuery = { theaters: 'ugc-25', date: '2026-09-13', start_after: '18:00', finish_before: '23:30' }
const response: SlotResult[] = [12, 13].map((id, index) => ({
  showtime: {
    provider: 'ugc', id: `ugc-showing-${id}`,
    movie: { slug: `film-${id}`, title: `Film ${id}`, runtime_minutes: 90, updated_at: '2026-09-13T00:00:00Z' },
    start_time: `2026-09-13T${18 + index * 2}:00:00+02:00`, end_time: `2026-09-13T${20 + index * 2}:00:00+02:00`,
    language: 'VF', format: '2D', room: '', booking_url: null
  },
  theater: { provider: 'ugc', id: 'ugc-25', name: 'UGC', city: 'Lille' },
  poster_url: null, backdrop_url: null,
  effective_start_time: `2026-09-13T${18 + index * 2}:00:00+02:00`,
  effective_end_time: `2026-09-13T${20 + index * 2}:00:00+02:00`,
  buffer_ads_minutes: 15, slack_before_minutes: 0, slack_after_minutes: 0
}))

function harness(query: LocationQuery, searchSlot: () => Promise<SlotResult[]> = async () => response) {
  const route = reactive({ query })
  const navigate = async ({ query: nextQuery }: { query: LocationQuery }) => { route.query = nextQuery }
  let searchCalls = 0
  const bindings = {
    ...date, ...routeQuery, ...showtimeFilters, ...showtimeResults,
    computed, reactive, ref, watch, nextTick, buildCompleteSearchShareTarget, absoluteSiteUrl,
    useRoute: () => route,
    useRouter: () => ({ replace: navigate, push: navigate }),
    useMesSeancesApi: () => ({ searchSlot: () => { searchCalls++; return searchSlot() } }),
    usePageCinemaSelection: () => ({
      activeTheaterIds: ref(['ugc-25']), activeTheaters: ref([{ available_dates: ['2026-09-13'] }]),
      isInitialized: ref(true), isLoading: ref(false), error: ref(''), initialize: async () => {}, isSharedSelectionDifferent: ref(false)
    }),
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    useSeoMeta: () => {}, useHead: () => {}, onMounted: () => {}, onBeforeUnmount: () => {},
    getFrenchApiError: () => 'Recherche impossible'
  }
  const scope = effectScope()
  // SAFETY: The setup wrapper explicitly returns these page bindings; lifecycle tests exercise their runtime shape.
  const page = scope.run(() => new Function(...Object.keys(bindings), `${compiled}\nreturn { form, pending, selectedOnly, selectedCount, visibleResults, shareTarget, isFilterSheetOpen, initializePreferences, canonicalizeShowtimeSelection, setSelectedOnly, toggleShowtimeSelection, clearShowtimeSelection, setResultGrouping, setResultLayout, submitSearch }`)(...Object.values(bindings))) as PageState
  return { page, route, stop: () => scope.stop(), searchCalls: () => searchCalls }
}

async function settle() {
  await nextTick()
  await nextTick()
}

test('shared selected-only state survives loading and validates only after results arrive', async (context) => {
  let resolveSearch!: (results: SlotResult[]) => void
  const search = new Promise<SlotResult[]>((resolve) => { resolveSearch = resolve })
  const { page, route, stop, searchCalls } = harness({ ...searchQuery, selected: 'u12,u99', selected_only: '1' }, () => search)
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
  assert.deepEqual(page.visibleResults.value.map((result) => result.key), ['ugc:ugc-showing-12'])
  assert.equal(new URL(page.shareTarget.value!, 'https://messeances.fr').searchParams.get('selected'), 'u12')
  assert.equal(searchCalls(), 1)
})

test('toggle and same-search navigation preserve selections, draft filters and mobile sheet without refetching', async (context) => {
  const { page, route, stop, searchCalls } = harness({ ...searchQuery, selected: 'u12', campaign: 'test' })
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
    const { page, route, stop } = harness({ ...searchQuery, selected: 'u12', selected_only: '1' })
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
    { selected: 'u12', selected_only: ['1', '1'] }
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

test('changed search submission and bare-route navigation clear both selection query keys', async (context) => {
  const { page, route, stop } = harness({ ...searchQuery, selected: 'u12', selected_only: '1' })
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
