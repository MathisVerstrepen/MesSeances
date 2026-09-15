import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import type { LocationQuery } from 'vue-router'
import type { HistoryOptionsResponse } from '../app/types/api.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { createStatisticsRequest, statisticsBars } from '../app/utils/statistics.ts'
import { createHistoryOptionsRequest, historyDateError, historySelectionOptions, parseHistoryStatisticsQuery, parseStatisticsPageQuery, statisticsCustomDraft, statisticsParisToday, statisticsPeriod, statisticsPeriodRange, statisticsPeriods, statisticsPageDraft, statisticsPageDraftQuery, statisticsPageRoute, statisticsPageSignature } from '../app/utils/statisticsHistory.ts'

test('four named presets resolve inclusive Paris calendar dates across DST, leap days and years', () => {
  assert.deepEqual(statisticsPeriods.map(choice => choice.label), ['7 prochains jours', '30 derniers jours', 'Depuis le début de la collecte', 'Période personnalisée'])
  assert.deepEqual(statisticsPeriod({}), { period: 'next7', error: '' })
  for (const [today, nextEnd, lastStart] of [
    ['2026-03-27', '2026-04-02', '2026-02-26'],
    ['2026-10-23', '2026-10-29', '2026-09-24'],
    ['2028-02-27', '2028-03-04', '2028-01-29'],
    ['2028-03-01', '2028-03-07', '2028-02-01'],
    ['2026-12-29', '2027-01-04', '2026-11-30'],
    ['2027-01-02', '2027-01-08', '2026-12-04']
  ] as const) {
    assert.deepEqual(statisticsPeriodRange('next7', today), { from: today, through: nextEnd })
    assert.deepEqual(parseStatisticsPageQuery({}, today), { query: { date: today, date_to: nextEnd }, error: '' })
    assert.deepEqual(parseStatisticsPageQuery({ period: 'last30' }, today).query, { date: lastStart, date_to: today })
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
  draft.period = 'last30'
  const parsed = statisticsPageDraftQuery(draft, today)
  assert.deepEqual(parsed.query, { date: '2026-08-17', date_to: today, city: ['paris', 'unknown'], theater: ['other'] })
  const next = statisticsPageRoute(route, draft.period, parsed.query)
  assert.deepEqual(next, { period: 'last30', city: ['paris', 'unknown'], theater: ['other'], campaign: ['footer', 'test'] })
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
    await api.historyStatistics(parseStatisticsPageQuery({ period: 'all', mode: 'history', city, theater, campaign: 'footer' }).query, signal)
    assert.equal(requests[0]!.url.pathname, '/api/v1/statistics/history')
    assert.deepEqual(requests[0]!.url.searchParams.getAll('city'), city)
    assert.deepEqual(requests[0]!.url.searchParams.getAll('theater'), theater)
    assert.deepEqual([...requests[0]!.url.searchParams.keys()], ['city', 'city', 'city', 'theater', 'theater'])
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
