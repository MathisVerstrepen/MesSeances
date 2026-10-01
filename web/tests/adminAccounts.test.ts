import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { createSSRApp, h, type Component } from 'vue'
import * as accounts from '../app/composables/useAdminAccounts.ts'
import {
  getFrenchAdminApiError,
  useMesSeancesApi,
} from '../app/composables/useMesSeancesApi.ts'
import type {
  AdminAccountItem,
  AdminAccountsQuery,
  AdminAccountsResponse,
} from '../app/types/api.ts'

function account(overrides: Partial<AdminAccountItem> = {}): AdminAccountItem {
  return {
    email: 'owner@example.test',
    username: 'owner',
    state: 'complete',
    created_at: '2026-10-01T12:34:00Z',
    email_verified_at: '2026-10-01T12:35:00Z',
    has_password: true,
    google_linked: false,
    ...overrides,
  }
}

function response(
  items = [account()],
  total = items.length,
  offset = 0,
): AdminAccountsResponse {
  return { items, total, limit: 50, offset }
}

function failure(status: number, code = 'unknown') {
  return {
    status,
    data: { error: { code, message: 'private diagnostic owner@example.test' } },
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

test('French labels cover every state, stored method combination, nulls and Paris dates', () => {
  assert.deepEqual(accounts.adminAccountStateLabels, {
    complete: 'Complet',
    pending_email: 'E-mail à vérifier',
    pending_username: 'Pseudo à choisir',
    expired: 'Inscription expirée',
  })
  for (const [has_password, google_linked, label] of [
    [false, false, 'Aucune'],
    [true, false, 'Mot de passe'],
    [false, true, 'Google'],
    [true, true, 'Mot de passe, Google'],
  ] as const) {
    assert.equal(
      accounts.adminAccountMethodLabels({ has_password, google_linked }),
      label,
    )
  }
  assert.equal(
    accounts.formatAdminAccountDate('2026-10-01T12:34:00Z'),
    '1 oct. 2026, 14:34',
  )
  assert.equal(
    accounts.formatAdminAccountDate('2026-01-01T23:30:00Z'),
    '2 janv. 2026, 00:30',
  )
  assert.equal(accounts.formatAdminAccountDate('invalid'), 'Date indisponible')
})

interface AdminAccountsFetchOptions {
  credentials: 'include'
  query: AdminAccountsQuery
  signal?: AbortSignal
  retry: false
  cache: 'no-store'
  referrerPolicy: 'no-referrer'
  timeout: number
}

test('API uses fixed protected endpoint, credentials, no caching, bounded abortable read and existing 401 wrapper', async () => {
  const calls: Array<{ url: string; options: AdminAccountsFetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: AdminAccountsFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve(response())
    },
  })
  const signal = new AbortController().signal
  await useMesSeancesApi().adminAccounts({ limit: 50, offset: 100 }, signal)
  assert.deepEqual(calls, [
    {
      url: 'http://localhost:8080/api/v1/admin/accounts',
      options: {
        credentials: 'include',
        query: { limit: 50, offset: 100 },
        signal,
        retry: false,
        cache: 'no-store',
        referrerPolicy: 'no-referrer',
        timeout: 15000,
      },
    },
  ])
  Object.assign(globalThis, {
    $fetch: async () => {
      throw failure(401)
    },
  })
  await assert.rejects(
    useMesSeancesApi().adminAccounts({ limit: 50, offset: 0 }),
    { status: 401 },
  )
  const source = await readFile(
    new URL('../app/composables/useMesSeancesApi.ts', import.meta.url),
    'utf8',
  )
  assert.match(source, /adminAccounts\([^]*?return withAdminRedirect\(/)
  assert.match(
    source,
    /getApiErrorStatus\(error\) === 401[^]*?navigateTo\('\/admin\/login'\)/,
  )
})

test('pagination requests exactly 50 rows, handles zero totals and bounds previous/next', async () => {
  const queries: AdminAccountsQuery[] = []
  const listing = accounts.useAdminAccounts({
    adminAccounts: async (query) => {
      queries.push(query)
      return response([account()], 101, query.offset)
    },
  })
  assert.equal(listing.loading.value, true)
  assert.equal(listing.loaded.value, false)
  assert.deepEqual(queries, [], 'constructing state must never fetch on SSR')
  await listing.load()
  assert.equal(listing.total.value, 101)
  assert.equal(listing.pageCount.value, 3)
  await listing.changePage(0)
  await listing.changePage(1)
  await listing.changePage(1.5)
  await listing.changePage(4)
  await listing.changePage(2)
  await listing.changePage(3)
  await listing.changePage(2)
  assert.deepEqual(queries, [
    { limit: 50, offset: 0 },
    { limit: 50, offset: 50 },
    { limit: 50, offset: 100 },
    { limit: 50, offset: 50 },
  ])
  listing.dispose()
  const empty = accounts.useAdminAccounts({
    adminAccounts: async () => response([], 0),
  })
  await empty.load()
  assert.equal(empty.loaded.value, true)
  assert.equal(empty.loading.value, false)
  assert.equal(empty.total.value, 0)
  assert.equal(empty.pageCount.value, 1)
  assert.deepEqual(empty.items.value, [])
  empty.dispose()
})

test('rows and total clear before reload or failure; retry retains requested page, never raw diagnostics', async () => {
  for (const cause of [
    failure(503, 'admin_accounts_unavailable'),
    failure(503, 'admin_unavailable'),
    failure(400, 'invalid_query'),
    failure(401),
    new Error('owner@example.test'),
  ]) {
    const pending = deferred<AdminAccountsResponse>()
    let reads = 0
    const queries: AdminAccountsQuery[] = []
    const listing = accounts.useAdminAccounts({
      adminAccounts: (query) => {
        queries.push(query)
        return ++reads === 2
          ? pending.promise
          : Promise.resolve(response([account()], 51, query.offset))
      },
    })
    await listing.load()
    const reload = listing.changePage(2)
    assert.equal(listing.loading.value, true)
    assert.equal(listing.loaded.value, false)
    assert.deepEqual(listing.items.value, [])
    assert.equal(listing.total.value, 0)
    await listing.changePage(1)
    assert.equal(reads, 2, 'duplicate pagination blocked during read')
    pending.reject(cause)
    await reload
    assert.equal(listing.loading.value, false)
    assert.deepEqual(listing.items.value, [])
    assert.equal(listing.total.value, 0)
    assert.notEqual(listing.error.value, '')
    assert.doesNotMatch(listing.error.value, /private|owner@example.test/)
    if ('status' in cause && cause.status === 401)
      assert.match(listing.error.value, /Reconnectez-vous/)
    await listing.load()
    assert.equal(listing.error.value, '')
    assert.equal(listing.page.value, 2)
    assert.deepEqual(queries.at(-1), { limit: 50, offset: 50 })
    listing.dispose()
  }
  assert.doesNotMatch(
    getFrenchAdminApiError(failure(503, 'admin_accounts_unavailable')),
    /private/,
  )
})

test('obsolete successes and failures cannot replace newer results even if transport ignores abort', async () => {
  for (const reject of [false, true]) {
    const old = deferred<AdminAccountsResponse>()
    let reads = 0
    let oldSignal: AbortSignal | undefined
    const listing = accounts.useAdminAccounts({
      adminAccounts: (_query, signal) => {
        if (++reads === 1) {
          oldSignal = signal
          return old.promise
        }
        return Promise.resolve(
          response([account({ email: 'latest@example.test' })]),
        )
      },
    })
    const first = listing.load()
    await listing.load()
    assert.equal(oldSignal?.aborted, true)
    if (reject) old.reject(failure(500))
    else old.resolve(response())
    await first
    assert.equal(listing.items.value[0]?.email, 'latest@example.test')
    assert.equal(listing.error.value, '')
    assert.equal(listing.loading.value, false)
    listing.dispose()
  }
})

test('departing page aborts read, clears private state and ignores late completion or retry', async () => {
  for (const reject of [false, true]) {
    const pending = deferred<AdminAccountsResponse>()
    let reads = 0
    let signal: AbortSignal | undefined
    const listing = accounts.useAdminAccounts({
      adminAccounts: (_query, nextSignal) => {
        signal = nextSignal
        return ++reads === 1 ? Promise.resolve(response()) : pending.promise
      },
    })
    await listing.load()
    const loading = listing.load()
    listing.dispose()
    assert.equal(signal?.aborted, true)
    assert.deepEqual(listing.items.value, [])
    assert.equal(listing.total.value, 0)
    assert.equal(listing.page.value, 1)
    if (reject) pending.reject(failure(500))
    else pending.resolve(response())
    await loading
    await listing.load()
    assert.equal(reads, 2)
    assert.deepEqual(listing.items.value, [])
    assert.equal(listing.loaded.value, false)
    assert.equal(listing.error.value, '')
  }
})

test('shrinking database clamps out-of-range page and performs one fresh read of last page', async () => {
  const queries: AdminAccountsQuery[] = []
  const listing = accounts.useAdminAccounts({
    adminAccounts: async (query) => {
      queries.push(query)
      return response(query.offset ? [] : [account()], 1, query.offset)
    },
  })
  await listing.load(3)
  assert.deepEqual(queries, [
    { limit: 50, offset: 100 },
    { limit: 50, offset: 0 },
  ])
  assert.equal(listing.page.value, 1)
  assert.equal(listing.items.value.length, 1)
  assert.equal(listing.loading.value, false)
  assert.equal(listing.loaded.value, true)
  listing.dispose()
})

const pageSource = await readFile(
  new URL('../app/pages/admin/accounts.vue', import.meta.url),
  'utf8',
)
const require = createRequire(import.meta.url)
type Listing = ReturnType<typeof accounts.useAdminAccounts>

interface PageModule {
  default?: Component
}

interface PageHooks {
  mounted?: () => void
  leave?: () => void
  unmount?: () => void
}

async function renderPage(listing: Listing) {
  const { descriptor } = parse(pageSource)
  const script = compileScript(descriptor, {
    id: 'admin-accounts',
    inlineTemplate: true,
  })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: PageModule = {}
  const hooks: PageHooks = {}
  const localRequire = (id: string) =>
    id === '~/composables/useAdminAccounts'
      ? { ...accounts, useAdminAccounts: () => listing }
      : require(id)
  const context = {
    require: localRequire,
    exports,
    definePageMeta: () => {},
    useHead: () => {},
    useMesSeancesApi: () => ({}),
    onMounted: (hook: () => void) => {
      hooks.mounted = hook
    },
    onBeforeRouteLeave: (hook: () => void) => {
      hooks.leave = hook
    },
    onBeforeUnmount: (hook: () => void) => {
      hooks.unmount = hook
    },
  }
  new Function(...Object.keys(context), compiled)(...Object.values(context))
  assert.ok(exports.default)
  const app = createSSRApp(exports.default)
  app.component('NuxtLink', {
    setup:
      (_props, { slots }) =>
      () =>
        h('a', slots.default?.()),
  })
  app.component('EditorialStatePanel', {
    setup:
      (_props, { slots }) =>
      () =>
        h('div', slots.default?.()),
  })
  return { html: await renderToString(app), hooks }
}

test('real SFC renders loading, empty, sanitized errors/retry and escaped semantic table with French count/pagination', async () => {
  let reads = 0
  const listing = accounts.useAdminAccounts({
    adminAccounts: async () => {
      reads += 1
      return response()
    },
  })
  const initial = await renderPage(listing)
  assert.equal(reads, 0, 'SSR must not fetch or serialize private rows')
  assert.match(initial.html, /Chargement des comptes/)
  assert.doesNotMatch(initial.html, /owner@example.test|<table/)
  assert.ok(initial.hooks.mounted)
  initial.hooks.mounted()
  await Promise.resolve()
  assert.equal(reads, 1)
  listing.items.value = [
    account({
      email: '<script>alert(1)</script>@example.test',
      username: null,
      email_verified_at: null,
      state: 'expired',
      has_password: true,
      google_linked: true,
    }),
  ]
  listing.total.value = 1234
  const ready = await renderPage(listing)
  assert.match(ready.html, /1\u202f234 comptes/)
  assert.match(ready.html, /<table\b/)
  assert.equal((ready.html.match(/scope="col"/g) ?? []).length, 6)
  assert.match(ready.html, /scope="row"/)
  assert.match(
    ready.html,
    /role="region" aria-label="Tableau des comptes, défilement horizontal"/,
  )
  assert.match(ready.html, /tabindex="0"/)
  assert.match(
    ready.html,
    /&lt;script&gt;alert\(1\)&lt;\/script&gt;@example.test/,
  )
  assert.doesNotMatch(ready.html, /<script>/)
  assert.match(ready.html, /Non choisi/)
  assert.match(ready.html, /Non vérifié/)
  assert.match(ready.html, /Inscription expirée/)
  assert.match(ready.html, /Mot de passe, Google/)
  assert.match(ready.html, /datetime="2026-10-01T12:34:00Z"/)
  assert.match(ready.html, /1 oct. 2026, 14:34/)
  assert.match(ready.html, /Page 1 sur 25/)
  assert.match(ready.html, /disabled[^>]*>\s*Précédente/)
  listing.items.value = []
  listing.total.value = 0
  const empty = await renderPage(listing)
  assert.match(empty.html, /0 comptes/)
  assert.match(empty.html, /Aucun compte enregistré/)
  assert.doesNotMatch(empty.html, /<table/)
  listing.error.value = 'Impossible de charger les comptes. Réessayez.'
  const failed = await renderPage(listing)
  assert.match(failed.html, /role="alert"/)
  assert.match(failed.html, /Réessayer/)
  assert.doesNotMatch(failed.html, /<table/)
  assert.ok(ready.hooks.leave)
  ready.hooks.leave()
  assert.deepEqual(listing.items.value, [])
  assert.equal(listing.total.value, 0)
})

test('page authenticates, fetches on mount only, tears down on route leave/unmount and avoids public persistence/URL PII', async () => {
  assert.match(pageSource, /definePageMeta\(\{ middleware: 'admin-auth' \}\)/)
  assert.match(pageSource, /onMounted\(/)
  assert.match(pageSource, /onBeforeRouteLeave\(/)
  assert.match(pageSource, /onBeforeUnmount\(dispose\)/)
  assert.match(pageSource, /name: 'robots', content: 'noindex, nofollow'/)
  assert.match(pageSource, /name: 'referrer', content: 'no-referrer'/)
  assert.match(pageSource, /@click="load\(\)"/)
  const composable = await readFile(
    new URL('../app/composables/useAdminAccounts.ts', import.meta.url),
    'utf8',
  )
  assert.doesNotMatch(
    pageSource + composable,
    /localStorage|sessionStorage|useState|useAsyncData|useFetch|v-html|console\.|router\.|route\.query|mailto:/,
  )
  const dashboard = await readFile(
    new URL('../app/pages/admin/index.vue', import.meta.url),
    'utf8',
  )
  assert.match(dashboard, /to="\/admin\/accounts"/)
})
