import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import { createMemoryHistory, createRouter, type LocationQuery } from 'vue-router'
import { watch } from 'vue'
import type { HistoryOptionsResponse } from '../app/types/api.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { createStatisticsRequest, statisticsBars } from '../app/utils/statistics.ts'
import { createHistoryOptionsRequest, historyDateError, historySelectionOptions, parseHistoryStatisticsQuery, parseStatisticsPageQuery, statisticsCustomDraft, statisticsParisToday, statisticsPeriod, statisticsPeriodRange, statisticsPeriods, statisticsPageDraft, statisticsPageDraftQuery, statisticsPageRoute, statisticsPageSignature } from '../app/utils/statisticsHistory.ts'

test('four named presets resolve inclusive Paris calendar dates across DST, leap days and years', () => {
  assert.deepEqual(statisticsPeriods.map(choice => choice.label), ['7 prochains jours', '30 prochains jours', 'Depuis le début de la collecte', 'Période personnalisée'])
  assert.deepEqual(statisticsPeriod({}), { period: 'next7', error: '' })
  for (const [today, nextEnd, next30End] of [
    ['2026-03-27', '2026-04-02', '2026-04-25'],
    ['2026-10-23', '2026-10-29', '2026-11-21'],
    ['2028-02-27', '2028-03-04', '2028-03-27'],
    ['2028-03-01', '2028-03-07', '2028-03-30'],
    ['2026-12-29', '2027-01-04', '2027-01-27'],
    ['2027-01-02', '2027-01-08', '2027-01-31']
  ] as const) {
    assert.deepEqual(statisticsPeriodRange('next7', today), { from: today, through: nextEnd })
    assert.deepEqual(parseStatisticsPageQuery({}, today), { query: { date: today, date_to: nextEnd }, error: '' })
    assert.deepEqual(parseStatisticsPageQuery({ period: 'next30' }, today).query, { date: today, date_to: next30End })
  }
  for (const [instant, day] of [['2026-03-28T23:30:00Z', '2026-03-29'], ['2026-03-29T22:30:00Z', '2026-03-30'], ['2026-10-24T22:30:00Z', '2026-10-25'], ['2026-10-25T23:30:00Z', '2026-10-26'], ['2026-12-31T23:30:00Z', '2027-01-01']]) assert.equal(statisticsParisToday(new Date(instant!)), day)
  assert.deepEqual(parseStatisticsPageQuery({ period: 'all', date: 'stale', date_to: ['ignored'], city: 'paris' }).query, { city: ['paris'] })
  assert.equal(statisticsPeriodRange('all', '2026-09-15'), null)
})

test('custom requires both valid ordered dates without past, future or length limits', () => {
  for (const date of ['0001-01-01', '0099-12-31', '0100-01-01', '2024-02-29', '9999-12-31']) assert.equal(historyDateError(date), '', date)
  for (const date of ['0000-01-01', '10000-01-01', '2026-02-29', '2026-02-30', '2026-13-01', '2026-1-01', ' 2026-01-01']) assert.ok(historyDateError(date), date)
  assert.ok(historyDateError(undefined, '2026-09-15'))
  assert.ok(historyDateError('2026-09-15', '2025-01-01'))
  const query = { date: '2020-01-01', date_to: '2030-12-31' }
  assert.deepEqual(parseStatisticsPageQuery({ ...query, period: 'custom' }, '2026-09-15'), { query, error: '' })
  const draft = statisticsPageDraft({ ...query, period: 'custom' })
  assert.deepEqual(statisticsPageDraftQuery(draft).query, query)
  for (const invalid of [{}, { date: '2020-01-01' }, { date_to: '2020-01-01' }, { date: '', date_to: '' }, { date: ['2020-01-01'], date_to: '2020-01-01' }, { date: '2020-01-02', date_to: '2020-01-01' }]) assert.ok(parseStatisticsPageQuery({ ...invalid, period: 'custom' }).error)
  const missing = statisticsPageDraft({ period: 'custom', date: '2020-01-01' })
  assert.equal(missing.date_to, '')
  assert.ok(statisticsPageDraftQuery(missing).error)
  assert.equal(parseStatisticsPageQuery({ period: 'custom', date: '0001-01-01', date_to: '9999-12-31' }).error, '')
})

test('period drafts, reset and back/forward preserve filters, strip obsolete mode and never emit preset dates', () => {
  for (const period of ['', null, [], ['all'], ['next7', 'all'], 'invalid', 'NEXT7', ' all ']) assert.ok(parseStatisticsPageQuery({ period }).error)
  const today = '2026-09-15'
  const route = { period: 'custom', date: '2020-01-01', date_to: '2030-01-01', city: ['paris', 'unknown'], theater: ['other'], campaign: ['footer', 'test'], mode: 'history' }
  const before = structuredClone(route)
  const draft = statisticsPageDraft(route, today)
  draft.period = 'next30'
  const parsed = statisticsPageDraftQuery(draft, today)
  assert.deepEqual(parsed.query, { date: today, date_to: '2026-10-14', city: ['paris', 'unknown'], theater: ['other'] })
  const next = statisticsPageRoute(route, draft.period, parsed.query)
  assert.deepEqual(next, { period: 'next30', city: ['paris', 'unknown'], theater: ['other'], campaign: ['footer', 'test'] })
  assert.deepEqual(route, before)
  assert.deepEqual(statisticsPageRoute(route), { campaign: ['footer', 'test'] })
  assert.deepEqual(statisticsPageRoute({ mode: 'invalid', period: ['bad'], date: 'bad' }), {})
  assert.deepEqual(parseStatisticsPageQuery({ mode: 'history' }, today), parseStatisticsPageQuery({}, today))
  assert.deepEqual(statisticsPageRoute({}, 'next7', parsed.query), { city: ['paris', 'unknown'], theater: ['other'] })
  const routes: LocationQuery[] = [{}, route, next, { period: 'all', genre: 'drame' }]
  for (const route of [...routes, ...routes.toReversed()]) {
    assert.deepEqual(statisticsPageDraftQuery(statisticsPageDraft(route, today), today), parseStatisticsPageQuery(route, today))
  }
  assert.notEqual(statisticsPageSignature(route), statisticsPageSignature({ ...route, period: 'all' }))
  assert.equal(statisticsPageSignature(route), statisticsPageSignature({ ...route, campaign: 'other', mode: 'anything' }))
  for (const query of [{ city: Array(51).fill('x') }, { pass: 'é'.repeat(101) }, { language: ['VF'] }, { chain: 'invalid' }]) assert.ok(parseHistoryStatisticsQuery(query).error)
})

test('custom initializes displayed envelope or next7; rolling drafts resolve at each apply/request', () => {
  const draft = statisticsPageDraft({ period: 'all', city: ['paris'] }, '2026-09-15')
  const displayed = { from: '2020-01-01', through: '2030-01-01' }
  const custom = statisticsCustomDraft(draft, displayed, '2026-09-15')
  assert.equal(custom.date, displayed.from)
  assert.equal(custom.date_to, displayed.through)
  assert.deepEqual(custom.city, ['paris'])
  assert.equal(draft.period, 'all')
  assert.deepEqual(statisticsPageDraftQuery(statisticsCustomDraft(draft, null, '2026-09-15')).query, { date: '2026-09-15', date_to: '2026-09-21', city: ['paris'] })
  const rolling = statisticsPageDraft({}, '2026-12-31')
  assert.deepEqual(statisticsPageDraftQuery(rolling, '2027-01-01').query, { date: '2027-01-01', date_to: '2027-01-07' })
  const recovery = statisticsPageDraft({ period: ['bad'], city: ['paris'] }, '2026-09-15')
  recovery.period = 'all'
  assert.deepEqual(statisticsPageDraftQuery(recovery).query, { city: ['paris'] })
})

test('film survives hydration, every period and intersecting edits; removal is draft-only until Apply', () => {
  const today = '2026-09-15'
  const route = { period: 'all', film: 'unknown-film', city: ['paris'], theater: ['ugc-1'], campaign: ['footer', 'test'], date: 'stale', date_to: 'stale', mode: 'history' }
  const before = structuredClone(route)
  const draft = statisticsPageDraft(route, today)
  assert.equal(draft.film, route.film)
  for (const { value: period } of statisticsPeriods) {
    const edited = period === 'custom' ? statisticsCustomDraft(draft, { from: '2020-01-01', through: '2030-01-01' }, today) : { ...draft, period }
    edited.language = 'VF'
    edited.genre = 'drame'
    const parsed = statisticsPageDraftQuery(edited, today)
    assert.equal(parsed.error, '')
    assert.equal(parsed.query.film, route.film)
    const applied = statisticsPageRoute(route, period, parsed.query)
    assert.equal(applied.film, route.film)
    assert.deepEqual(applied.campaign, route.campaign)
    assert.deepEqual(statisticsPageDraftQuery(statisticsPageDraft(applied, today), today), parsed)
  }
  draft.film = ''
  assert.deepEqual(route, before)
  assert.equal(parseStatisticsPageQuery(route).query.film, 'unknown-film')
  const removed = statisticsPageRoute(route, draft.period, statisticsPageDraftQuery(draft, today).query)
  assert.deepEqual(removed, { period: 'all', city: ['paris'], theater: ['ugc-1'], campaign: ['footer', 'test'] })
  assert.deepEqual(statisticsPageRoute(route), { campaign: ['footer', 'test'] })
  assert.equal(statisticsPageDraft(statisticsPageRoute(route), today).period, 'next7')
  assert.equal(statisticsPageDraft(statisticsPageRoute(route), today).film, '')
})

function statisticsRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [{ path: '/statistiques', component: {} }, { path: '/film/:slug', component: {} }] })
}

test('installed router resolves fresh all-history entity links and round-trips reserved characters once', async () => {
  const router = statisticsRouter()
  await router.push('/film/old?date=2026-09-15&theaters=saved&q=search&page=2&sort=next&layout=boxes#sessions')
  for (const value of ['film-42', '&', '+', '/', '?', '#', ',', 'é界😀', 'alias &+/?#,é%']) {
    for (const key of ['film', 'city', 'theater'] as const) {
      const location = { path: '/statistiques', query: { period: 'all', [key]: key === 'film' ? value : [value] } }
      const resolved = router.resolve(location)
      const url = new URL(resolved.href, 'http://fixture.invalid')
      assert.equal(url.pathname, '/statistiques')
      assert.equal(url.hash, '')
      assert.deepEqual([...url.searchParams.keys()], ['period', key])
      assert.deepEqual(url.searchParams.getAll(key), [value])
      const roundTrip = router.resolve(resolved.href)
      assert.deepEqual(parseStatisticsPageQuery(roundTrip.query).query, { [key]: key === 'film' ? value : [value] })
      assert.deepEqual(location.query, { period: 'all', [key]: key === 'film' ? value : [value] })
    }
  }
})

test('router back/forward rebuilds film draft, removal and reset from authoritative URL', async () => {
  const router = statisticsRouter()
  const today = '2026-09-15'
  await router.push({ path: '/statistiques', query: { period: 'all', film: 'film-42', campaign: 'footer' } })
  const original = router.currentRoute.value.fullPath
  let draft = statisticsPageDraft(router.currentRoute.value.query, today)
  const stop = watch(() => statisticsPageSignature(router.currentRoute.value.query), () => { draft = statisticsPageDraft(router.currentRoute.value.query, today) }, { flush: 'sync' })
  try {
    draft.film = ''
    assert.equal(router.currentRoute.value.query.film, 'film-42')
    await router.push({ query: statisticsPageRoute(router.currentRoute.value.query, draft.period, statisticsPageDraftQuery(draft, today).query) })
    const removed = router.currentRoute.value.fullPath
    await router.push({ query: statisticsPageRoute(router.currentRoute.value.query) })
    assert.equal(draft.period, 'next7')
    const go = (delta: number) => new Promise<void>(resolve => {
      const off = router.afterEach(() => { off(); resolve() })
      router.go(delta)
    })
    await go(-1)
    assert.equal(router.currentRoute.value.fullPath, removed)
    assert.equal(draft.film, '')
    assert.equal(draft.period, 'all')
    await go(-1)
    assert.equal(router.currentRoute.value.fullPath, original)
    assert.equal(draft.film, 'film-42')
    await go(1)
    assert.equal(draft.film, '')
    assert.equal(router.currentRoute.value.query.campaign, 'footer')
  } finally { stop() }
})

test('installed fetch serializes history arrays and selected lookup; mode never reaches upstream', async () => {
  const requests: { url: URL; signal?: AbortSignal | null }[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://127.0.0.1:18080/' } }),
    $fetch: createFetch({ fetch: async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({ url: new URL(String(input)), signal: init?.signal })
      return new Response('{}', { headers: { 'content-type': 'application/json' } })
    } })
  })
  try {
    const api = useMesSeancesApi()
    const signal = new AbortController().signal
    const city = ['créteil', 'paris & lille', 'comma,id']
    const theater = ['ugc-1', 'pathé/+?#,2']
    const film = 'alias &+/?#,é'
    await api.historyStatistics(parseStatisticsPageQuery({ period: 'all', mode: 'history', date: 'stale', date_to: 'stale', city, theater, film, chain: 'ugc', language: 'VF', format: 'IMAX', genre: 'drame', pass: 'UGC Illimité', campaign: 'footer' }).query, signal)
    assert.equal(requests[0]!.url.pathname, '/api/v1/statistics/history')
    assert.deepEqual(requests[0]!.url.searchParams.getAll('city'), city)
    assert.deepEqual(requests[0]!.url.searchParams.getAll('theater'), theater)
    assert.deepEqual(requests[0]!.url.searchParams.getAll('film'), [film])
    for (const [key, value] of Object.entries({ chain: 'ugc', language: 'VF', format: 'IMAX', genre: 'drame', pass: 'UGC Illimité' })) assert.equal(requests[0]!.url.searchParams.get(key), value)
    assert.deepEqual([...requests[0]!.url.searchParams.keys()].sort(), ['city', 'city', 'city', 'theater', 'theater', 'film', 'chain', 'language', 'format', 'genre', 'pass'].sort())
    assert.equal(requests[0]!.signal, signal)
    await api.historyStatisticsOptions({ kind: 'theater', q: 'Écran %_ &', selected: theater }, signal)
    assert.equal(requests[1]!.url.pathname, '/api/v1/statistics/history/options')
    assert.equal(requests[1]!.url.searchParams.get('q'), 'Écran %_ &')
    assert.deepEqual(requests[1]!.url.searchParams.getAll('selected'), theater)
    assert.equal(requests[1]!.signal, signal)
    await api.historyStatistics({ city: [], theater: [] })
    assert.equal(requests[2]!.url.search, '')
    await api.historyStatisticsOptions({ kind: 'city', selected: [] })
    assert.equal(requests[3]!.url.search, '?kind=city')
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})

test('known selections outside initial page are not unavailable until selected lookup succeeds', () => {
  const items = [{ value: 'first', label: 'First' }]
  const selected = ['distant', 'unknown']
  assert.deepEqual(historySelectionOptions(items, [], selected, new Set()).map(option => option.label), ['First', 'distant', 'unknown'])
  const known = [{ value: 'distant', label: 'Cinéma lointain' }]
  assert.deepEqual(historySelectionOptions([], known, selected, new Set(selected)), [known[0], { value: 'unknown', label: 'unknown (indisponible)' }])
  assert.deepEqual(historySelectionOptions(known, known, ['distant'], new Set()), known)
  const stable = [...items, ...known]
  assert.deepEqual(historySelectionOptions(stable, [], ['first'], new Set()), stable)
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
const optionsResult = (value: string): HistoryOptionsResponse => ({ items: [{ value, label: value }], selected: [], has_more: false })
test('option debounce cancels immediately and guards late success/error, disposal and aggregate isolation', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const state = { pending: false, error: '', value: '', aggregate: 'unchanged' }
  const request = createHistoryOptionsRequest({ start() { state.pending = true; state.error = '' }, success(result) { state.value = result.items[0]!.value }, error() { state.error = 'failed' }, finish() { state.pending = false } })
  let calls = 0
  request.schedule(async () => { calls++; return optionsResult('discarded') })
  t.mock.timers.tick(249)
  assert.equal(calls, 0)
  request.schedule(async () => { calls++; return optionsResult('latest search') })
  t.mock.timers.tick(249)
  assert.equal(calls, 0)
  t.mock.timers.tick(1)
  await Promise.resolve()
  assert.equal(calls, 1)
  assert.equal(state.value, 'latest search')
  const old = deferred<HistoryOptionsResponse>()
  let oldSignal: AbortSignal | undefined
  const running = request.schedule(signal => { oldSignal = signal; return old.promise }, true)
  request.schedule(async () => optionsResult('replacement'))
  assert.equal(oldSignal?.aborted, true)
  old.resolve(optionsResult('stale'))
  await running
  assert.equal(state.value, 'latest search')
  assert.equal(state.pending, true)
  t.mock.timers.tick(250)
  await Promise.resolve()
  assert.equal(state.value, 'replacement')
  const failure = deferred<HistoryOptionsResponse>()
  const lateFailure = request.schedule(() => failure.promise, true)
  await request.schedule(async () => optionsResult('new mode'), true)
  failure.reject(new Error('stale'))
  await lateFailure
  assert.equal(state.error, '')
  await request.schedule(async () => { throw new Error('current') }, true)
  assert.equal(state.error, 'failed')
  assert.equal(state.value, 'new mode')
  assert.equal(state.aggregate, 'unchanged')
  request.schedule(async () => { calls++; return optionsResult('unmounted') })
  request.cancel()
  t.mock.timers.tick(250)
  assert.equal(calls, 1)
})

test('period transition invalidates aggregate result, loading and error from previous period', async () => {
  const state = { value: '', pending: false, error: '' }
  const request = createStatisticsRequest<string>({ start() { state.value = ''; state.error = ''; state.pending = true }, success(value) { state.value = value }, error() { state.error = 'failed' }, finish() { state.pending = false } })
  const upcoming = deferred<string>()
  const history = deferred<string>()
  let signal: AbortSignal | undefined
  const first = request.run(abort => { signal = abort; return upcoming.promise })
  const second = request.run(() => history.promise)
  assert.equal(signal?.aborted, true)
  upcoming.resolve('upcoming')
  await first
  assert.deepEqual(state, { value: '', error: '', pending: true })
  history.resolve('history')
  await second
  assert.deepEqual(state, { value: 'history', error: '', pending: false })
})

test('film URL changes abort prior requests and isolate late results, errors and pending state', async () => {
  const router = statisticsRouter()
  await router.push({ path: '/statistiques', query: { period: 'all', film: 'film-1' } })
  const state = { value: '', pending: false, error: '' }
  const calls: { film: string | undefined; signal: AbortSignal; result: ReturnType<typeof deferred<string>>; running: Promise<void> }[] = []
  const request = createStatisticsRequest<string>({ start() { state.value = ''; state.error = ''; state.pending = true }, success(value) { state.value = value }, error() { state.error = 'failed' }, finish() { state.pending = false } })
  const stop = watch(() => statisticsPageSignature(router.currentRoute.value.query), () => {
    const film = parseStatisticsPageQuery(router.currentRoute.value.query).query.film
    const result = deferred<string>()
    let signal!: AbortSignal
    const running = request.run(abort => { signal = abort; return result.promise })
    calls.push({ film, result, signal, running })
  }, { immediate: true, flush: 'sync' })
  try {
    await router.push({ query: { period: 'all', film: 'unknown-film' } })
    assert.deepEqual(calls.map(call => call.film), ['film-1', 'unknown-film'])
    assert.equal(calls[0]!.signal.aborted, true)
    calls[0]!.result.resolve('old film')
    await calls[0]!.running
    assert.deepEqual(state, { value: '', pending: true, error: '' })
    await router.push({ query: { period: 'all', film: 'film-3' } })
    calls[1]!.result.reject(new Error('old failure'))
    await calls[1]!.running
    assert.equal(calls[1]!.signal.aborted, true)
    assert.deepEqual(state, { value: '', pending: true, error: '' })
    calls[2]!.result.resolve('current film')
    await calls[2]!.running
    assert.deepEqual(state, { value: 'current film', pending: false, error: '' })
  } finally { stop(); request.cancel() }
})

test('all three actual entity links use loaded IDs, fresh all-history routes and accessible icon-only actions', async () => {
  for (const [file, loaded, identity] of [
    ['film/[slug].vue', 'schedule', 'film: schedule.movie.slug'],
    ['ville/[slug]/cinemas.vue', 'detail', 'city: [detail.city.slug]'],
    ['cinema/[slug].vue', 'response', 'theater: [response.theater.id]']
  ]) {
    const page = await readFile(new URL(`../app/pages/${file}`, import.meta.url), 'utf8')
    const loadedBranch = page.indexOf(`<template v-else-if="${loaded}">`)
    assert.ok(loadedBranch > 0, file)
    const headerStart = page.indexOf('<header', loadedBranch)
    const headerEnd = page.indexOf('</header>', headerStart)
    const header = page.slice(headerStart, headerEnd)
    const links = [...page.matchAll(/<NuxtLink\b[^>]*aria-label="Statistiques"[^>]*>[\s\S]*?<\/NuxtLink>/g)]
    assert.equal(links.length, 1, file)
    const link = links[0]!
    assert.ok(link.index! > headerStart && link.index! < headerEnd, file)
    assert.ok(link[0].includes(`:to="{ path: '/statistiques', query: { period: 'all', ${identity} } }"`), file)
    assert.doesNotMatch(link[0], /v-if|v-show|encodeURIComponent|route\.|preferences|@click/)
    for (const pattern of [/size-11 shrink-0/, /border-2 border-ink/, /focus-visible:outline-3/, /focus-visible:outline-offset-3/, /title="Statistiques"/]) assert.match(link[0], pattern)
    assert.match(link[0], />\s*<ChartNoAxesCombined :size="20" aria-hidden="true" \/>\s*<\/NuxtLink>$/)
    assert.match(page, /import \{[^}]*ChartNoAxesCombined[^}]*\} from '@lucide\/vue'/)
    assert.match(page, /<ShareButton/)
    if (loaded === 'schedule') {
      assert.match(header, /<div class="absolute right-4 top-4 z-20 flex flex-col-reverse items-center gap-3[^"]*sm:flex-row[^"]*">\s*<NuxtLink[\s\S]*?<\/NuxtLink>\s*<MovieExternalLinksMenu[^>]*\/>\s*<\/div>/)
      assert.doesNotMatch(header, /pt-24/)
      assert.match(header, /max-w-\[calc\(100%_-_7rem\)\]/)
      assert.match(header, /<h1 class="[^"]*uppercase \[overflow-wrap:anywhere\]"/)
      assert.match(header, /externalLinks\.length \? 'sm:pr-28' : 'sm:pr-16'/)
      assert.ok(headerEnd < page.indexOf('<section v-if="hasNoSessions"'))
      assert.match(page, /responseSlug !== slug.value/)
      assert.match(page, /redirectCode: 308, replace: true/)
    } else if (loaded === 'detail') {
      assert.match(header, /<div class="mt-6 flex flex-wrap items-center gap-3">\s*<NuxtLink[\s\S]*?<\/NuxtLink>\s*<ShareButton class="shrink-0" \/>/)
    } else {
      assert.match(header, /<div class="flex items-center justify-between gap-4[^"]*">\s*<p[^>]*>\{\{ pageDescription \}\}<\/p>\s*<NuxtLink/)
      assert.ok(header.indexOf('{{ pageDescription }}') < header.indexOf(link[0]), file)
    }
  }
})

test('visible escaped film selection is outside result states and uses draft removal plus existing Apply/SEO guards', async () => {
  const page = await readFile(new URL('../app/pages/statistiques.vue', import.meta.url), 'utf8')
  const selection = page.slice(page.indexOf('<div v-if="draft.film"'), page.indexOf('<div class="grid min-w-0 gap-5'))
  assert.match(selection, /Film : \{\{ selectedFilmLabel \}\}/)
  assert.match(selection, /overflow-wrap:anywhere/)
  assert.match(selection, /<button type="button"[^>]*aria-label="Retirer le film"[^>]*@click="draft.film = ''">\s*<X :size="16" aria-hidden="true" \/>\s*<\/button>/)
  assert.match(selection, /size-11 shrink-0/)
  assert.doesNotMatch(selection, />Retirer le film</)
  assert.doesNotMatch(selection, /router|apply|v-html|pending|error|data\./)
  assert.ok(page.indexOf('<div v-if="draft.film"') > page.indexOf('<form'))
  assert.ok(page.indexOf('<div v-if="draft.film"') < page.indexOf('</form>'))
  assert.ok(page.indexOf('</form>') < page.indexOf(':aria-busy="pending"'))
  assert.match(page, /@submit.prevent="apply"/)
  assert.match(page, /statisticsPageDraftQuery\(draft.value, current\)/)
  assert.match(page, /watch\(signature, \(\) => \{\s*draft.value = statisticsPageDraft/)
  assert.match(page, /initialActive = false\s*initialController.abort\(\)/)
  assert.match(page, /statisticsQueryKeys.some\(key => route.query\[key\] !== undefined\) \? 'noindex,follow'/)
  assert.match(page, /absoluteSiteUrl\(useRuntimeConfig\(\).public.siteUrl, '\/statistiques'\)/)
  assert.match(page, /rel: 'canonical', href: canonicalUrl/)
  assert.match(page, /api\.movieShowtimes\(film, \{ date: statisticsParisToday\(\) \}\)/)
  assert.doesNotMatch(page, /v-html|api\.movies\(|filmOptions|key: 'film'/)
})

test('city header counts stay side by side on mobile with a vertical divider', async () => {
  const page = await readFile(new URL('../app/pages/ville/[slug]/cinemas.vue', import.meta.url), 'utf8')
  assert.match(page, /<dl class="grid grid-cols-2 border-t-2 border-ink lg:grid-cols-1 lg:border-l-2 lg:border-t-0">/)
  assert.match(page, /class="min-w-0 border-l-2 border-ink p-5 sm:p-6 lg:border-l-0 lg:border-t-2"/)
})

test('bounded notices keep full denominators and remote selectors remain opt-in', async () => {
  assert.equal(statisticsBars([{ value: 'genre', label: 'Genre', count: 10 }], 1000)[0]!.share, '1,0 %')
  const [page, local, wrapper, control] = await Promise.all(['pages/statistiques.vue', 'components/StatisticsLocalTable.vue', 'components/StatisticsHistorySelect.vue', 'components/StatisticsMultiSelect.vue'].map(path => readFile(new URL(`../app/${path}`, import.meta.url), 'utf8')))
  for (const text of ['100 premiers genres', 'La collecte historique n’a pas encore commencé.', 'inclut les séances passées et futures enregistrées, sans filtre de date.', 'Aucune séance enregistrée pour ces filtres.', 'Toutes les séances enregistrées par MesSéances depuis le début de la collecte historique']) assert.ok(page.includes(text), text)
  assert.match(page, /:total="data.totals.movies"/)
  assert.match(page, /:total="data.totals.showtimes"/)
  assert.match(local, /100 premières villes/)
  assert.match(local, /100 premiers cinémas/)
  assert.match(local, /Tri et pagination limités/)
  assert.match(wrapper, /Affinez la recherche pour voir les autres options\./)
  assert.match(wrapper, /historyStatisticsOptions/)
  assert.match(wrapper, /watch\(\(\) => JSON.stringify\(props.modelValue\)/)
  assert.doesNotMatch(wrapper, /api\.statistics|api\.historyStatistics\(|useRouter|v-html/)
  assert.match(control, /props.externalSearch \? props.options : statisticsSearchOptions/)
  assert.match(control, /type="radio"/)
  assert.match(control, /type="checkbox"/)
})
