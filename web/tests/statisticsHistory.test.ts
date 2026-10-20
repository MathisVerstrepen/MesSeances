import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import {
  createMemoryHistory,
  createRouter,
  type LocationQuery,
} from 'vue-router'
import { watch } from 'vue'
import type { HistoryOptionsResponse } from '../app/types/api.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import {
  createStatisticsRequest,
  statisticsBars,
  statisticsOptionsWithSelection,
} from '../app/utils/statistics.ts'
import {
  createHistoryOptionsRequest,
  historySelectionOptions,
  parseStatisticsPageQuery,
  statisticsParisToday,
  statisticsPeriod,
  statisticsPeriods,
  statisticsPageDraft,
  statisticsPageDraftQuery,
  statisticsPageRoute,
  statisticsPageSignature,
} from '../app/utils/statisticsHistory.ts'

test('exactly four presets send explicit server-owned periods, filters and no dates', () => {
  assert.deepEqual(
    statisticsPeriods.map((choice) => choice.label),
    [
      '7 prochains jours',
      '30 prochains jours',
      'Semaine courante',
      'Depuis le début de la collecte',
    ],
  )
  assert.deepEqual(statisticsPeriod({}), { period: 'next7', error: '' })
  assert.deepEqual(
    statisticsPeriods.map(({ value }) => value),
    ['next7', 'next30', 'week', 'all'],
  )
  assert.deepEqual(parseStatisticsPageQuery({}), {
    query: { period: 'next7' },
    error: '',
  })
  for (const { value: period } of statisticsPeriods) {
    assert.deepEqual(parseStatisticsPageQuery({ period }), {
      query: { period },
      error: '',
    })
    assert.deepEqual(
      parseStatisticsPageQuery({
        period,
        date: 'stale',
        date_to: ['ignored'],
        city: ['paris'],
        theater: ['ugc-1'],
        chain: 'ugc',
        language: 'VF',
        format: 'IMAX',
        genre: 'drame',
        pass: 'UGC Illimité',
        film: 'film-42',
      }),
      {
        query: {
          period,
          city: ['paris'],
          theater: ['ugc-1'],
          chain: 'ugc',
          language: 'VF',
          format: 'IMAX',
          genre: 'drame',
          pass: 'UGC Illimité',
          film: 'film-42',
        },
        error: '',
      },
    )
  }
})

test('Paris today remains correct across midnight, DST and year rollover for chart and refresh', () => {
  for (const [instant, day] of [
    ['2026-03-28T23:30:00Z', '2026-03-29'],
    ['2026-03-29T22:30:00Z', '2026-03-30'],
    ['2026-10-24T22:30:00Z', '2026-10-25'],
    ['2026-10-25T23:30:00Z', '2026-10-26'],
    ['2026-12-31T23:30:00Z', '2027-01-01'],
  ])
    assert.equal(statisticsParisToday(new Date(instant!)), day)
})

test('period drafts, reset and back/forward preserve filters, strip obsolete mode and never emit preset dates', () => {
  for (const period of [
    '',
    null,
    [],
    ['all'],
    ['next7', 'all'],
    'invalid',
    'custom',
    'NEXT7',
    ' all ',
  ])
    assert.ok(parseStatisticsPageQuery({ period }).error)
  const route = {
    period: 'week',
    date: '2020-01-01',
    date_to: '2030-01-01',
    city: ['paris', 'unknown'],
    theater: ['other'],
    campaign: ['footer', 'test'],
    mode: 'history',
  }
  const before = structuredClone(route)
  const draft = statisticsPageDraft(route)
  assert.equal('date' in draft, false)
  assert.equal('date_to' in draft, false)
  draft.period = 'next30'
  const parsed = statisticsPageDraftQuery(draft)
  assert.deepEqual(parsed.query, {
    period: 'next30',
    city: ['paris', 'unknown'],
    theater: ['other'],
  })
  const next = statisticsPageRoute(route, draft.period, parsed.query)
  assert.deepEqual(next, {
    period: 'next30',
    city: ['paris', 'unknown'],
    theater: ['other'],
    campaign: ['footer', 'test'],
  })
  assert.deepEqual(route, before)
  assert.deepEqual(statisticsPageRoute(route), { campaign: ['footer', 'test'] })
  assert.deepEqual(
    statisticsPageRoute({ mode: 'invalid', period: ['bad'], date: 'bad' }),
    {},
  )
  assert.deepEqual(
    parseStatisticsPageQuery({ mode: 'history' }),
    parseStatisticsPageQuery({}),
  )
  assert.deepEqual(statisticsPageRoute({}, 'next7', parsed.query), {
    city: ['paris', 'unknown'],
    theater: ['other'],
  })
  const routes: LocationQuery[] = [
    {},
    route,
    next,
    { period: 'all', genre: 'drame' },
  ]
  for (const route of [...routes, ...routes.toReversed()]) {
    assert.deepEqual(
      statisticsPageDraftQuery(statisticsPageDraft(route)),
      parseStatisticsPageQuery(route),
    )
  }
  assert.notEqual(
    statisticsPageSignature(route),
    statisticsPageSignature({ ...route, period: 'all' }),
  )
  assert.equal(
    statisticsPageSignature(route),
    statisticsPageSignature({ ...route, campaign: 'other', mode: 'anything' }),
  )
  for (const query of [
    { city: Array(51).fill('x') },
    { pass: 'é'.repeat(101) },
    { language: ['VF'] },
    { chain: 'invalid' },
  ])
    assert.ok(parseStatisticsPageQuery(query).error)
})

test('invalid period recovery retains filters without falling back to all', () => {
  const recovery = statisticsPageDraft({ period: ['bad'], city: ['paris'] })
  assert.equal(recovery.period, 'next7')
  recovery.period = 'all'
  assert.deepEqual(statisticsPageDraftQuery(recovery).query, {
    period: 'all',
    city: ['paris'],
  })
})

test('film survives hydration, every period and intersecting edits; removal is draft-only until Apply', () => {
  const route = {
    period: 'all',
    film: 'unknown-film',
    city: ['paris'],
    theater: ['ugc-1'],
    campaign: ['footer', 'test'],
    date: 'stale',
    date_to: 'stale',
    mode: 'history',
  }
  const before = structuredClone(route)
  const draft = statisticsPageDraft(route)
  assert.equal(draft.film, route.film)
  for (const { value: period } of statisticsPeriods) {
    const edited = { ...draft, period }
    edited.language = 'VF'
    edited.genre = 'drame'
    const parsed = statisticsPageDraftQuery(edited)
    assert.equal(parsed.error, '')
    assert.equal(parsed.query.film, route.film)
    const applied = statisticsPageRoute(route, period, parsed.query)
    assert.equal(applied.film, route.film)
    assert.deepEqual(applied.campaign, route.campaign)
    assert.deepEqual(
      statisticsPageDraftQuery(statisticsPageDraft(applied)),
      parsed,
    )
  }
  draft.film = ''
  assert.deepEqual(route, before)
  assert.equal(parseStatisticsPageQuery(route).query.film, 'unknown-film')
  const removed = statisticsPageRoute(
    route,
    draft.period,
    statisticsPageDraftQuery(draft).query,
  )
  assert.deepEqual(removed, {
    period: 'all',
    city: ['paris'],
    theater: ['ugc-1'],
    campaign: ['footer', 'test'],
  })
  assert.deepEqual(statisticsPageRoute(route), { campaign: ['footer', 'test'] })
  assert.equal(statisticsPageDraft(statisticsPageRoute(route)).period, 'next7')
  assert.equal(statisticsPageDraft(statisticsPageRoute(route)).film, '')
})

function statisticsRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/statistiques', component: {} },
      { path: '/film/:slug', component: {} },
    ],
  })
}

test('VOF survives every period, shared reload, API transport and router back/forward', async () => {
  const router = statisticsRouter()
  const urls: URL[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080' } }),
    $fetch: createFetch({
      fetch: async (input: RequestInfo | URL) => {
        urls.push(new URL(input instanceof Request ? input.url : String(input)))
        return new Response('{}', {
          headers: { 'content-type': 'application/json' },
        })
      },
    }),
  })
  await router.push({
    path: '/statistiques',
    query: {
      period: 'all',
      language: 'VOF',
      city: ['paris'],
      film: 'missing-film',
      campaign: ['footer', 'test'],
    },
  })
  let draft = statisticsPageDraft(router.currentRoute.value.query)
  const stop = watch(
    () => statisticsPageSignature(router.currentRoute.value.query),
    () => {
      draft = statisticsPageDraft(router.currentRoute.value.query)
    },
    { flush: 'sync' },
  )
  const snapshots: string[] = []
  try {
    const api = useMesSeancesApi()
    for (const { value: period } of statisticsPeriods) {
      draft = { ...draft, period }
      const parsed = statisticsPageDraftQuery(draft)
      assert.equal(parsed.error, '')
      assert.equal(parsed.query.language, 'VOF')
      await router.push({
        query: statisticsPageRoute(
          router.currentRoute.value.query,
          period,
          parsed.query,
        ),
      })
      const route = router.currentRoute.value
      snapshots.push(route.fullPath)
      assert.equal(draft.period, period)
      assert.equal(draft.language, 'VOF')
      assert.deepEqual(route.query.campaign, ['footer', 'test'])
      assert.equal(route.query.film, 'missing-film')
      const shared = new URL(route.fullPath, 'http://fixture.invalid')
      assert.deepEqual(shared.searchParams.getAll('language'), ['VOF'])
      assert.equal(shared.searchParams.has('date'), false)
      const reloaded = router.resolve(shared.pathname + shared.search)
      assert.deepEqual(parseStatisticsPageQuery(reloaded.query), parsed)
      assert.deepEqual(
        statisticsPageDraftQuery(statisticsPageDraft(reloaded.query)),
        parsed,
      )
      await api.historyStatistics(parsed.query)
      const request = urls.at(-1)!
      assert.equal(request.pathname, '/api/v1/statistics/history')
      assert.deepEqual(request.searchParams.getAll('language'), ['VOF'])
      assert.equal(request.searchParams.get('period'), period)
      for (const key of ['date', 'date_to', 'campaign', 'mode'])
        assert.equal(request.searchParams.has(key), false)
      // An empty intersection or absent inventory option must not clear the applied VOF draft.
      assert.deepEqual(statisticsBars([], 0), [])
      assert.deepEqual(statisticsOptionsWithSelection([], [draft.language]), [
        { value: 'VOF', label: 'VOF (indisponible)' },
      ])
      assert.equal(statisticsPageDraftQuery(draft).query.language, 'VOF')
    }
    const go = (delta: number) =>
      new Promise<void>((resolve) => {
        const off = router.afterEach(() => {
          off()
          resolve()
        })
        router.go(delta)
      })
    for (const [delta, indices] of [
      [-1, [2, 1, 0]],
      [1, [1, 2, 3]],
    ] as const) {
      for (const index of indices) {
        await go(delta)
        assert.equal(router.currentRoute.value.fullPath, snapshots[index])
        assert.equal(draft.language, 'VOF')
        assert.equal(draft.period, statisticsPeriods[index]!.value)
        assert.deepEqual(
          statisticsPageDraftQuery(draft),
          parseStatisticsPageQuery(router.currentRoute.value.query),
        )
      }
    }
    draft.language = 'VF'
    assert.equal(router.currentRoute.value.query.language, 'VOF')
    await router.push({
      query: statisticsPageRoute(
        router.currentRoute.value.query,
        draft.period,
        statisticsPageDraftQuery(draft).query,
      ),
    })
    assert.equal(draft.language, 'VF')
    await go(-1)
    assert.equal(draft.language, 'VOF')
    await router.push({
      query: statisticsPageRoute(router.currentRoute.value.query),
    })
    assert.equal(draft.language, '')
    assert.deepEqual(router.currentRoute.value.query, {
      campaign: ['footer', 'test'],
    })
  } finally {
    stop()
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})

test('every statistics period accepts padded VOF but rejects malformed and combined version selectors', () => {
  for (const { value: period } of statisticsPeriods) {
    const route = { period, date: '2026-09-15', date_to: '2026-09-21' }
    assert.equal(
      parseStatisticsPageQuery({ ...route, language: ' VOF ' }).query.language,
      'VOF',
    )
    for (const language of [
      'vof',
      'Vof',
      'ORIGINAL',
      'ALL',
      'VOF,VOSTFR',
      'VOF+VOSTFR',
      '',
      null,
      ['VOF'],
      ['VOF', 'VOF'],
      ['VOF', 'VF'],
    ]) {
      assert.ok(
        parseStatisticsPageQuery({ ...route, language }).error,
        `${period}: ${JSON.stringify(language)}`,
      )
    }
  }
})

test('installed router resolves fresh all-history entity links and round-trips reserved characters once', async () => {
  const router = statisticsRouter()
  await router.push(
    '/film/old?date=2026-09-15&theaters=saved&q=search&page=2&sort=next&layout=boxes#sessions',
  )
  for (const value of [
    'film-42',
    '&',
    '+',
    '/',
    '?',
    '#',
    ',',
    'é界😀',
    'alias &+/?#,é%',
  ]) {
    for (const key of ['film', 'city', 'theater'] as const) {
      const location = {
        path: '/statistiques',
        query: { period: 'all', [key]: key === 'film' ? value : [value] },
      }
      const resolved = router.resolve(location)
      const url = new URL(resolved.href, 'http://fixture.invalid')
      assert.equal(url.pathname, '/statistiques')
      assert.equal(url.hash, '')
      assert.deepEqual([...url.searchParams.keys()], ['period', key])
      assert.deepEqual(url.searchParams.getAll(key), [value])
      const roundTrip = router.resolve(resolved.href)
      assert.deepEqual(parseStatisticsPageQuery(roundTrip.query).query, {
        period: 'all',
        [key]: key === 'film' ? value : [value],
      })
      assert.deepEqual(location.query, {
        period: 'all',
        [key]: key === 'film' ? value : [value],
      })
    }
  }
})

test('router back/forward rebuilds film draft, removal and reset from authoritative URL', async () => {
  const router = statisticsRouter()
  await router.push({
    path: '/statistiques',
    query: { period: 'all', film: 'film-42', campaign: 'footer' },
  })
  const original = router.currentRoute.value.fullPath
  let draft = statisticsPageDraft(router.currentRoute.value.query)
  const stop = watch(
    () => statisticsPageSignature(router.currentRoute.value.query),
    () => {
      draft = statisticsPageDraft(router.currentRoute.value.query)
    },
    { flush: 'sync' },
  )
  try {
    draft.film = ''
    assert.equal(router.currentRoute.value.query.film, 'film-42')
    await router.push({
      query: statisticsPageRoute(
        router.currentRoute.value.query,
        draft.period,
        statisticsPageDraftQuery(draft).query,
      ),
    })
    const removed = router.currentRoute.value.fullPath
    await router.push({
      query: statisticsPageRoute(router.currentRoute.value.query),
    })
    assert.equal(draft.period, 'next7')
    const go = (delta: number) =>
      new Promise<void>((resolve) => {
        const off = router.afterEach(() => {
          off()
          resolve()
        })
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
  } finally {
    stop()
  }
})

test('installed fetch serializes history arrays and selected lookup; mode never reaches upstream', async () => {
  const requests: { url: URL; signal?: AbortSignal | null }[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({
      public: { apiBase: 'http://127.0.0.1:18080/' },
    }),
    $fetch: createFetch({
      fetch: async (input: RequestInfo | URL, init?: RequestInit) => {
        requests.push({ url: new URL(String(input)), signal: init?.signal })
        return new Response('{}', {
          headers: { 'content-type': 'application/json' },
        })
      },
    }),
  })
  try {
    const api = useMesSeancesApi()
    const signal = new AbortController().signal
    const city = ['créteil', 'paris & lille', 'comma,id']
    const theater = ['ugc-1', 'pathé/+?#,2']
    const film = 'alias &+/?#,é'
    await api.historyStatistics(
      parseStatisticsPageQuery({
        period: 'all',
        mode: 'history',
        date: 'stale',
        date_to: 'stale',
        city,
        theater,
        film,
        chain: 'ugc',
        language: 'VF',
        format: 'IMAX',
        genre: 'drame',
        pass: 'UGC Illimité',
        campaign: 'footer',
      }).query,
      signal,
    )
    assert.equal(requests[0]!.url.pathname, '/api/v1/statistics/history')
    assert.deepEqual(requests[0]!.url.searchParams.getAll('city'), city)
    assert.deepEqual(requests[0]!.url.searchParams.getAll('theater'), theater)
    assert.deepEqual(requests[0]!.url.searchParams.getAll('film'), [film])
    for (const [key, value] of Object.entries({
      chain: 'ugc',
      language: 'VF',
      format: 'IMAX',
      genre: 'drame',
      pass: 'UGC Illimité',
      period: 'all',
    }))
      assert.equal(requests[0]!.url.searchParams.get(key), value)
    assert.deepEqual(
      [...requests[0]!.url.searchParams.keys()].sort(),
      [
        'city',
        'city',
        'city',
        'theater',
        'theater',
        'film',
        'chain',
        'language',
        'format',
        'genre',
        'pass',
        'period',
      ].sort(),
    )
    assert.equal(requests[0]!.signal, signal)
    await api.historyStatisticsOptions(
      { kind: 'theater', q: 'Écran %_ &', selected: theater },
      signal,
    )
    assert.equal(
      requests[1]!.url.pathname,
      '/api/v1/statistics/history/options',
    )
    assert.equal(requests[1]!.url.searchParams.get('q'), 'Écran %_ &')
    assert.deepEqual(requests[1]!.url.searchParams.getAll('selected'), theater)
    assert.equal(requests[1]!.signal, signal)
    await api.historyStatistics({ period: 'next7', city: [], theater: [] })
    assert.equal(requests[2]!.url.search, '?period=next7')
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
  assert.deepEqual(
    historySelectionOptions(items, [], selected, new Set()).map(
      (option) => option.label,
    ),
    ['First', 'distant', 'unknown'],
  )
  const known = [{ value: 'distant', label: 'Cinéma lointain' }]
  assert.deepEqual(
    historySelectionOptions([], known, selected, new Set(selected)),
    [known[0], { value: 'unknown', label: 'unknown (indisponible)' }],
  )
  assert.deepEqual(
    historySelectionOptions(known, known, ['distant'], new Set()),
    known,
  )
  const stable = [...items, ...known]
  assert.deepEqual(
    historySelectionOptions(stable, [], ['first'], new Set()),
    stable,
  )
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const optionsResult = (value: string): HistoryOptionsResponse => ({
  items: [{ value, label: value }],
  selected: [],
  has_more: false,
})
test('option debounce cancels immediately and guards late success/error, disposal and aggregate isolation', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const state = { pending: false, error: '', value: '', aggregate: 'unchanged' }
  const request = createHistoryOptionsRequest({
    start() {
      state.pending = true
      state.error = ''
    },
    success(result) {
      state.value = result.items[0]!.value
    },
    error() {
      state.error = 'failed'
    },
    finish() {
      state.pending = false
    },
  })
  let calls = 0
  request.schedule(async () => {
    calls++
    return optionsResult('discarded')
  })
  t.mock.timers.tick(249)
  assert.equal(calls, 0)
  request.schedule(async () => {
    calls++
    return optionsResult('latest search')
  })
  t.mock.timers.tick(249)
  assert.equal(calls, 0)
  t.mock.timers.tick(1)
  await Promise.resolve()
  assert.equal(calls, 1)
  assert.equal(state.value, 'latest search')
  const old = deferred<HistoryOptionsResponse>()
  let oldSignal: AbortSignal | undefined
  const running = request.schedule((signal) => {
    oldSignal = signal
    return old.promise
  }, true)
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
  await request.schedule(async () => {
    throw new Error('current')
  }, true)
  assert.equal(state.error, 'failed')
  assert.equal(state.value, 'new mode')
  assert.equal(state.aggregate, 'unchanged')
  request.schedule(async () => {
    calls++
    return optionsResult('unmounted')
  })
  request.cancel()
  t.mock.timers.tick(250)
  assert.equal(calls, 1)
})

test('period transition invalidates aggregate result, loading and error from previous period', async () => {
  const state = { value: '', pending: false, error: '' }
  const request = createStatisticsRequest<string>({
    start() {
      state.value = ''
      state.error = ''
      state.pending = true
    },
    success(value) {
      state.value = value
    },
    error() {
      state.error = 'failed'
    },
    finish() {
      state.pending = false
    },
  })
  const upcoming = deferred<string>()
  const history = deferred<string>()
  let signal: AbortSignal | undefined
  const first = request.run((abort) => {
    signal = abort
    return upcoming.promise
  })
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
  await router.push({
    path: '/statistiques',
    query: { period: 'all', film: 'film-1' },
  })
  const state = { value: '', pending: false, error: '' }
  const calls: {
    film: string | undefined
    signal: AbortSignal
    result: ReturnType<typeof deferred<string>>
    running: Promise<void>
  }[] = []
  const request = createStatisticsRequest<string>({
    start() {
      state.value = ''
      state.error = ''
      state.pending = true
    },
    success(value) {
      state.value = value
    },
    error() {
      state.error = 'failed'
    },
    finish() {
      state.pending = false
    },
  })
  const stop = watch(
    () => statisticsPageSignature(router.currentRoute.value.query),
    () => {
      const film = parseStatisticsPageQuery(router.currentRoute.value.query)
        .query.film
      const result = deferred<string>()
      let signal!: AbortSignal
      const running = request.run((abort) => {
        signal = abort
        return result.promise
      })
      calls.push({ film, result, signal, running })
    },
    { immediate: true, flush: 'sync' },
  )
  try {
    await router.push({ query: { period: 'all', film: 'unknown-film' } })
    assert.deepEqual(
      calls.map((call) => call.film),
      ['film-1', 'unknown-film'],
    )
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
    assert.deepEqual(state, {
      value: 'current film',
      pending: false,
      error: '',
    })
  } finally {
    stop()
    request.cancel()
  }
})

test('all three actual entity links use loaded IDs, fresh all-history routes and accessible icon-only actions', async () => {
  for (const [file, loaded, identity] of [
    ['film/[slug].vue', 'schedule', 'film: schedule.movie.slug'],
    ['ville/[slug]/cinemas.vue', 'detail', 'city: [detail.city.slug]'],
    ['cinema/[slug].vue', 'response', 'theater: [response.theater.id]'],
  ]) {
    const page = await readFile(
      new URL(`../app/pages/${file}`, import.meta.url),
      'utf8',
    )
    const loadedBranch = page.indexOf(`<template v-else-if="${loaded}">`)
    assert.ok(loadedBranch > 0, file)
    const headerStart = page.indexOf('<header', loadedBranch)
    const headerEnd = page.indexOf('</header>', headerStart)
    const header = page.slice(headerStart, headerEnd)
    const links = [
      ...page.matchAll(
        /<NuxtLink\b[^>]*aria-label="Statistiques"[^>]*>[\s\S]*?<\/NuxtLink>/g,
      ),
    ]
    assert.equal(links.length, loaded === 'response' ? 2 : 1, file)
    for (const link of links) {
      assert.ok(link.index! > headerStart && link.index! < headerEnd, file)
      assert.ok(
        link[0].includes(
          `:to="{ path: '/statistiques', query: { period: 'all', ${identity} } }"`,
        ),
        file,
      )
      assert.doesNotMatch(
        link[0],
        /v-if|v-show|encodeURIComponent|route\.|preferences|@click/,
      )
      for (const pattern of [
        /\bsize-11\b/,
        /border-2 border-ink/,
        /focus-visible:outline-3/,
        /focus-visible:outline-offset-3/,
        /title="Statistiques"/,
      ])
        assert.match(link[0], pattern)
      if (loaded !== 'response') assert.match(link[0], /size-11 shrink-0/)
      assert.match(
        link[0],
        />\s*<ChartNoAxesCombined :size="20" aria-hidden="true" \/>\s*<\/NuxtLink>$/,
      )
    }
    assert.match(
      page,
      /import \{[^}]*ChartNoAxesCombined[^}]*\} from '@lucide\/vue'/,
    )
    assert.match(page, /<ShareButton/)
    if (loaded === 'response') {
      // Complementary breakpoint classes keep exactly one statistics action
      // visible: desktop hero overlay or the mobile address strip.
      const desktopLink = links[0]!
      const mobileLink = links[1]!
      const mobileStripStart = page.indexOf(
        'class="flex items-center gap-4 border-t-2 border-ink',
        headerStart,
      )
      assert.ok(mobileStripStart > desktopLink.index!)
      assert.ok(mobileStripStart < mobileLink.index!)
      assert.match(
        header,
        /absolute bottom-8 right-8 hidden items-center gap-2 lg:flex/,
      )
      assert.match(
        page.slice(mobileStripStart, mobileLink.index!),
        /^class="[^"\n]*lg:hidden"/,
      )
      assert.match(mobileLink[0], /inline-flex size-11 shrink-0/)
      assert.match(header, /ml-auto flex shrink-0 items-center gap-2/)
      const mobileLinkClass = mobileLink[0].match(/\bclass="([^"]*)"/)?.[1]
      assert.ok(mobileLinkClass)
      assert.doesNotMatch(mobileLinkClass, /\bhidden\b|lg:/)
      assert.doesNotMatch(header, /\{\{ pageDescription \}\}/)
      assert.match(page, /viewQuery\('activity'\)/)
      assert.match(page, />\s*Activité\s*<\/NuxtLink>/)
    } else if (loaded === 'schedule') {
      assert.match(
        header,
        /<div\s+class="absolute right-4 top-4 z-20 flex flex-col-reverse items-center gap-3[^"]*sm:flex-row[^"]*"\s*>\s*<NuxtLink[\s\S]*?<\/NuxtLink>\s*<MovieExternalLinksMenu[^>]*\/>\s*<\/div>/,
      )
      assert.doesNotMatch(header, /pt-24/)
      assert.match(header, /max-w-\[calc\(100%_-_7rem\)\]/)
      assert.match(
        header,
        /<h1\s+class="[^"]*uppercase \[overflow-wrap:anywhere\]"/,
      )
      assert.match(header, /externalLinks\.length \? 'sm:pr-28' : 'sm:pr-16'/)
      assert.ok(headerEnd < page.search(/<section\s+v-if="hasNoSessions"/))
      assert.match(page, /responseSlug !== slug.value/)
      assert.match(page, /redirectCode: 308, replace: true/)
    } else if (loaded === 'detail') {
      assert.match(
        header,
        /<div class="mt-6 flex flex-wrap items-center gap-3">\s*<NuxtLink[\s\S]*?<\/NuxtLink>\s*<ShareButton class="shrink-0" \/>/,
      )
    }
  }
})

test('visible escaped film selection is outside result states and uses draft removal plus existing Apply/SEO guards', async () => {
  const page = await readFile(
    new URL('../app/pages/statistiques.vue', import.meta.url),
    'utf8',
  )
  const selectionStart = page.search(/<div\s+v-if="draft.film"/)
  const selectionEnd = page.search(/<div\s+class="grid min-w-0 gap-5/)
  assert.ok(selectionStart >= 0)
  assert.ok(selectionEnd > selectionStart)
  const selection = page.slice(selectionStart, selectionEnd)
  assert.match(selection, /Film : \{\{ selectedFilmLabel \}\}/)
  assert.match(selection, /overflow-wrap:anywhere/)
  assert.match(
    selection,
    /<button\s+type="button"[^>]*aria-label="Retirer le film"[^>]*@click="draft.film = ''"\s*>\s*<X :size="16" aria-hidden="true" \/>\s*<\/button>/,
  )
  assert.match(selection, /size-11 shrink-0/)
  assert.doesNotMatch(selection, />\s*Retirer le film\s*</)
  assert.doesNotMatch(selection, /router|apply|v-html|pending|error|data\./)
  assert.ok(selectionStart > page.indexOf('<form'))
  assert.ok(selectionStart < page.indexOf('</form>'))
  assert.ok(page.indexOf('</form>') < page.indexOf(':aria-busy="pending"'))
  assert.match(page, /@submit.prevent="apply"/)
  assert.match(page, /statisticsPageDraftQuery\(draft.value\)/)
  assert.match(
    page,
    /watch\(\s*signature,\s*\(\) => \{\s*draft.value = statisticsPageDraft/,
  )
  assert.match(page, /initialActive = false\s*initialController.abort\(\)/)
  assert.match(
    page,
    /statisticsQueryKeys.some\((?:key|\(key\)) => route.query\[key\] !== undefined\)\s*\? 'noindex,follow'/,
  )
  assert.match(
    page,
    /absoluteSiteUrl\(\s*useRuntimeConfig\(\).public.siteUrl,\s*'\/statistiques',?\s*\)/,
  )
  assert.match(page, /rel: 'canonical', href: canonicalUrl/)
  assert.match(
    page,
    /api\.movieShowtimes\(film,\s*\{\s*date: statisticsParisToday\(\),?\s*\}\)/,
  )
  assert.doesNotMatch(page, /v-html|api\.movies\(|filmOptions|key: 'film'/)
})

test('bounded notices keep full denominators and remote selectors remain opt-in', async () => {
  assert.equal(
    statisticsBars([{ value: 'genre', label: 'Genre', count: 10 }], 1000)[0]!
      .share,
    '1,0 %',
  )
  const [page, local, wrapper, control] = await Promise.all(
    [
      'pages/statistiques.vue',
      'components/StatisticsLocalTable.vue',
      'components/StatisticsHistorySelect.vue',
      'components/StatisticsMultiSelect.vue',
    ].map((path) =>
      readFile(new URL(`../app/${path}`, import.meta.url), 'utf8'),
    ),
  )
  for (const text of [
    '100 premiers genres',
    'La collecte historique n’a pas encore commencé.',
    'inclut les séances passées et futures enregistrées, sans filtre de date.',
    'Aucune séance enregistrée pour ces filtres.',
    'Toutes les séances enregistrées par MesSéances depuis le début de la collecte historique',
  ])
    assert.ok(page.replace(/\s+/g, ' ').includes(text), text)
  assert.match(page, /:total="data.totals.movies"/)
  assert.match(page, /:total="data.totals.showtimes"/)
  assert.match(local, /100 premières villes/)
  assert.match(local, /100 premiers cinémas/)
  assert.match(local, /Tri et pagination limités/)
  assert.match(wrapper, /Affinez la recherche pour voir les autres options\./)
  assert.match(wrapper, /historyStatisticsOptions/)
  assert.match(wrapper, /watch\(\s*\(\) => JSON.stringify\(props.modelValue\)/)
  assert.doesNotMatch(
    wrapper,
    /api\.statistics|api\.historyStatistics\(|useRouter|v-html/,
  )
  assert.match(
    control,
    /props.externalSearch\s*\? props.options\s*: statisticsSearchOptions/,
  )
  assert.match(control, /type="radio"/)
  assert.match(control, /type="checkbox"/)
})
