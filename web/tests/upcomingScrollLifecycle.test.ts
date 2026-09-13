import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import type { Ref } from 'vue'
import type { LocationQuery, NavigationFailure, RouteLocationNormalized } from 'vue-router'
import type { UpcomingMoviesResponse } from '../app/types/api.ts'
import { queriesEqual } from '../app/utils/routeQuery.ts'
import * as upcoming from '../app/utils/upcomingMovies.ts'

// Exercise the real page setup, following searchSelectionLifecycle.test.ts.
const source = await readFile(new URL('../app/pages/films/prochainement.vue', import.meta.url), 'utf8')
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile('upcoming.ts', script, ts.ScriptTarget.Latest, true)
const withoutImports = parsed.statements.filter(statement => !ts.isImportDeclaration(statement)).map(statement => statement.getFullText(parsed)).join('\n').replaceAll('import.meta.server', 'false')
const compiled = ts.transpileModule(withoutImports, { compilerOptions: { target: ts.ScriptTarget.ESNext } }).outputText

function response(page: number): UpcomingMoviesResponse {
  return {
    generated_at: '2026-09-13T12:00:00Z', catalog_revision: 'fixture', timezone: 'Europe/Paris',
    window: { from: '2026-09-16', through: '2027-09-13' }, items: [], page, total: 20, total_weeks: 9, total_pages: 3
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

interface Page {
  pending: Ref<boolean>
  catalog: Ref<UpcomingMoviesResponse | null>
  errorMessage: Ref<string>
  followPageLink: (event: ReturnType<typeof click>, page: number) => void
  loadCatalog: () => Promise<void>
}

function click(overrides = {}) {
  return {
    button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, defaultPrevented: false,
    preventDefault() { this.defaultPrevented = true }, ...overrides
  }
}

async function settle() {
  for (let i = 0; i < 8; i++) await nextTick()
}

async function harness(reducedMotion = false) {
  const scope = effectScope()
  const route = reactive<{ query: LocationQuery }>({ query: {} })
  const calls: Array<{ page: number } & ReturnType<typeof deferred<UpcomingMoviesResponse>>> = []
  const scrolls: Array<{ top: number; behavior: string }> = []
  let mounted = async () => {}
  let unmount = () => {}
  let initial = true
  let navigationFailure = false
  let afterNavigation = (_to: Pick<RouteLocationNormalized, 'query'>, _from: Pick<RouteLocationNormalized, 'query'>, _failure?: Pick<NavigationFailure, 'type'>) => {}
  let navigationError = () => {}
  let renderTick: Promise<void> | null = null
  let page!: Page
  const bindings = {
    ...upcoming, queriesEqual, ref, computed,
    watch: (...args: Parameters<typeof watch>) => scope.run(() => watch(...args)),
    nextTick: () => renderTick ?? nextTick(),
    useRoute: () => route,
    useRouter: () => ({
      push: () => { assert.fail('NuxtLink owns navigation; page must not push again') },
      replace: async ({ query }: { query: LocationQuery }) => { route.query = query },
      afterEach: (callback: typeof afterNavigation) => { afterNavigation = callback; return () => { afterNavigation = () => {} } },
      onError: (callback: typeof navigationError) => { navigationError = callback; return () => { navigationError = () => {} } }
    }),
    useMesSeancesApi: () => ({ upcomingMovies: ({ page: requested }: { page: number }) => {
      if (initial) { initial = false; return Promise.resolve(response(requested)) }
      const request = { page: requested, ...deferred<UpcomingMoviesResponse>() }
      calls.push(request)
      return request.promise
    } }),
    useAsyncData: async (_key: string, load: () => Promise<{ catalog: UpcomingMoviesResponse | null; errorMessage: string }>) => ({ data: ref(await load()) }),
    onMounted: (callback: typeof mounted) => { mounted = callback },
    onBeforeUnmount: (callback: typeof unmount) => { unmount = callback },
    useRuntimeConfig: () => ({ public: { siteUrl: 'https://messeances.fr' } }),
    absoluteSiteUrl: () => 'https://messeances.fr/films/prochainement',
    useSeoMeta: () => {}, useHead: () => {}, getFrenchApiError: () => 'Échec du chargement',
    window: {
      matchMedia: () => ({ matches: reducedMotion }),
      scrollTo: (options: { top: number; behavior: string }) => {
        assert.equal(page.pending.value, false, 'scroll cannot run while the loading panel is displayed')
        scrolls.push(options)
      }
    }
  }
  // SAFETY: The wrapper explicitly returns these actual setup bindings; no production code is replaced.
  page = await new Function(...Object.keys(bindings), `return (async () => { ${compiled}\nreturn { pending, catalog, errorMessage, followPageLink, loadCatalog } })()`)(...Object.values(bindings)) as Page
  await mounted()
  const followPageLink = page.followPageLink
  // Model RouterLink's event order: push starts, preventDefault, emitted handler, async route commit.
  page.followPageLink = (event, target) => {
    const navigates = event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey
      && target >= 1 && target <= 3 && !page.pending.value
    if (navigates) event.preventDefault()
    followPageLink(event, target)
    if (navigates) queueMicrotask(() => {
      if (navigationFailure) afterNavigation({ query: upcoming.upcomingRouteQuery({ page: target }) }, { query: route.query }, { type: 4 })
      else route.query = upcoming.upcomingRouteQuery({ page: target })
    })
  }
  return {
    page, route, calls, scrolls,
    stop: () => { unmount(); scope.stop() },
    failNavigation: (fail: boolean) => { navigationFailure = fail },
    navigationError: () => navigationError(),
    holdRender: () => { const tick = deferred<void>(); renderTick = tick.promise; return tick }
  }
}

test('next and prev scroll only after winning response, pending false and render tick; history never resets', async context => {
  const h = await harness()
  context.after(h.stop)
  assert.deepEqual(h.scrolls, [])
  for (const target of [2, 1]) {
    // NuxtLink has called preventDefault before its emitted navigate reaches this page.
    await h.page.followPageLink(click({ defaultPrevented: true }), target)
    await settle()
    assert.equal(h.page.pending.value, true)
    const tick = h.holdRender()
    const previousScrolls = h.scrolls.length
    h.calls.at(-1)!.resolve(response(target))
    await settle()
    assert.equal(h.page.pending.value, false)
    assert.equal(h.page.catalog.value?.page, target)
    assert.equal(h.scrolls.length, previousScrolls)
    tick.resolve()
    await settle()
    assert.deepEqual(h.scrolls.at(-1), { top: 0, behavior: 'smooth' })
  }
  h.route.query = { page: '2' }
  await settle()
  h.calls.at(-1)!.resolve(response(2))
  await settle()
  assert.equal(h.scrolls.length, 2)
})

test('modified, middle and invalid clicks never arm a later scroll; pending double click is blocked', async context => {
  const h = await harness()
  context.after(h.stop)
  for (const options of [{ ctrlKey: true }, { metaKey: true }, { shiftKey: true }, { altKey: true }, { button: 1 }]) {
    const event = click(options)
    await h.page.followPageLink(event, 2)
    assert.equal(event.defaultPrevented, false)
  }
  for (const target of [0, 1, 4]) await h.page.followPageLink(click(), target)
  assert.equal(h.calls.length, 0)
  assert.deepEqual(h.route.query, {})
  await h.page.followPageLink(click(), 2)
  const second = click()
  await h.page.followPageLink(second, 2)
  await settle()
  assert.equal(second.defaultPrevented, true)
  assert.equal(h.calls.length, 1)
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.equal(h.scrolls.length, 1)
})

test('failed pagination retains intent for explicit retry, with reduced motion instant scrolling', async context => {
  const h = await harness(true)
  context.after(h.stop)
  await h.page.followPageLink(click(), 2)
  await settle()
  h.calls[0]!.reject(new Error('offline'))
  await settle()
  assert.equal(h.page.errorMessage.value, 'Échec du chargement')
  assert.deepEqual(h.scrolls, [])
  const retry = h.page.loadCatalog()
  h.calls[1]!.resolve(response(2))
  await retry
  assert.deepEqual(h.scrolls, [{ top: 0, behavior: 'instant' }])
})

test('superseded response, failed navigation and unmounted page never leak scroll intent', async context => {
  const h = await harness()
  context.after(h.stop)
  h.failNavigation(true)
  await h.page.followPageLink(click(), 2)
  h.failNavigation(false)
  h.route.query = { page: '2' }
  await settle()
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 3)
  await settle()
  h.route.query = { page: '1' }
  await settle()
  h.calls.at(-1)!.resolve(response(1))
  h.calls[1]!.resolve(response(3))
  await settle()
  assert.equal(h.page.catalog.value?.page, 1)
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 2)
  await settle()
  const tick = h.holdRender()
  h.calls.at(-1)!.resolve(response(2))
  await settle()
  h.stop()
  tick.resolve()
  await settle()
  assert.deepEqual(h.scrolls, [])
})

test('winning clamped page keeps pagination scroll intent through query replacement', async context => {
  const h = await harness()
  context.after(h.stop)
  await h.page.followPageLink(click(), 3)
  await settle()
  h.calls[0]!.resolve({ ...response(3), total_pages: 2 })
  await settle()
  assert.equal(h.calls[1]!.page, 2)
  h.calls[1]!.resolve({ ...response(2), total_pages: 2 })
  await settle()
  assert.deepEqual(h.route.query, { page: '2' })
  assert.equal(h.page.catalog.value?.page, 2)
  assert.deepEqual(h.scrolls, [{ top: 0, behavior: 'smooth' }])
})

test('router errors and unmount during fetch discard pending scroll', async context => {
  const h = await harness()
  context.after(h.stop)
  await h.page.followPageLink(click(), 2)
  await settle()
  h.navigationError()
  h.calls[0]!.resolve(response(2))
  await settle()
  assert.deepEqual(h.scrolls, [])
  await h.page.followPageLink(click(), 3)
  await settle()
  h.stop()
  h.calls[1]!.resolve(response(3))
  await settle()
  assert.equal(h.page.catalog.value?.page, 2)
  assert.deepEqual(h.scrolls, [])
})
