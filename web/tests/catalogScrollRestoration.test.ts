import assert from 'node:assert/strict'
import test from 'node:test'
import { createMemoryHistory, createRouter } from 'vue-router'
import type { RouterScrollBehavior } from 'vue-router'
import {
  installCatalogScrollRestoration,
  waitForCatalogRender,
} from '../app/utils/catalogScrollRestoration.ts'

function dom(t: import('node:test').TestContext, initial = 'true') {
  let busy = initial
  const queried: string[] = []
  const callbacks = new Set<() => void>()
  const oldDocument = Object.getOwnPropertyDescriptor(globalThis, 'document')
  const oldObserver = Object.getOwnPropertyDescriptor(
    globalThis,
    'MutationObserver',
  )
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: {
      documentElement: {},
      getElementById: (id: string) => {
        queried.push(id)
        return { getAttribute: () => busy }
      },
    },
  })
  Object.defineProperty(globalThis, 'MutationObserver', {
    configurable: true,
    value: class {
      callback: () => void
      constructor(callback: () => void) {
        this.callback = callback
      }
      observe() {
        callbacks.add(this.callback)
      }
      disconnect() {
        callbacks.delete(this.callback)
      }
    },
  })
  t.after(() => {
    if (oldDocument) Object.defineProperty(globalThis, 'document', oldDocument)
    else Reflect.deleteProperty(globalThis, 'document')
    if (oldObserver)
      Object.defineProperty(globalThis, 'MutationObserver', oldObserver)
    else Reflect.deleteProperty(globalThis, 'MutationObserver')
  })
  return {
    callbacks,
    queried,
    render: () => {
      busy = 'false'
      for (const callback of callbacks) callback()
    },
  }
}

test('waits for rendered success, empty or failed catalog and disconnects', async (t) => {
  const page = dom(t)
  const waiting = waitForCatalogRender(
    new AbortController().signal,
    'films-catalog',
  )
  assert.equal(page.callbacks.size, 1)
  page.render()
  assert.equal(await waiting, true)
  assert.equal(page.callbacks.size, 0)
  assert.equal(
    await waitForCatalogRender(new AbortController().signal, 'films-catalog'),
    true,
  )
})

test('stalled catalog has bounded wait with no late observer', async (t) => {
  const page = dom(t)
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const waiting = waitForCatalogRender(
    new AbortController().signal,
    'films-catalog',
    100,
  )
  t.mock.timers.tick(100)
  assert.equal(await waiting, true)
  assert.equal(page.callbacks.size, 0)
  page.render()
})

test('superseding navigation cancels wait and cleans observer/timer', async (t) => {
  const page = dom(t)
  const controller = new AbortController()
  const waiting = waitForCatalogRender(controller.signal, 'films-catalog')
  controller.abort()
  assert.equal(await waiting, false)
  assert.equal(page.callbacks.size, 0)
  assert.equal(
    await waitForCatalogRender(controller.signal, 'films-catalog'),
    false,
  )
})

function routerFixture(original: RouterScrollBehavior) {
  let beforeEach!: () => void
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: {} }],
    scrollBehavior: original,
  })
  router.beforeEach = (callback) => {
    beforeEach = () => {
      callback(router.resolve('/films'), router.resolve('/film/test'), () => {})
    }
    return () => {}
  }
  installCatalogScrollRestoration(router, (callback) => callback())
  const scroll = (to: string, from: string, saved = { left: 0, top: 2000 }) =>
    router.options.scrollBehavior!(
      router.resolve(to),
      router.resolve(from),
      saved,
    )
  return { scroll, navigate: () => beforeEach() }
}

test('extends saved catalog restore only after framework readiness', async (t) => {
  const page = dom(t)
  let resolve!: () => void
  const ready = new Promise<void>((yes) => {
    resolve = yes
  })
  const fixture = routerFixture(async (_to, _from, saved) => {
    await ready
    return saved
  })
  const waiting = fixture.scroll('/films', '/film/test')
  assert.equal(page.callbacks.size, 0)
  resolve()
  await Promise.resolve()
  await Promise.resolve()
  assert.equal(page.callbacks.size, 1)
  page.render()
  assert.deepEqual(await waiting, { left: 0, top: 2000 })
})

test('preserves Nuxt query/hash/meta/fresh-route decisions without waiting', async (t) => {
  const page = dom(t)
  for (const result of [
    false,
    { top: 0, left: 0 },
    { el: '#target', top: 12 },
  ]) {
    const fixture = routerFixture(() => result)
    assert.equal(await fixture.scroll('/films', '/film/test'), result)
  }
  const fixture = routerFixture((_to, _from, saved) => saved)
  assert.deepEqual(await fixture.scroll('/films', '/films'), {
    left: 0,
    top: 2000,
  })
  assert.deepEqual(await fixture.scroll('/other', '/film/test'), {
    left: 0,
    top: 2000,
  })
  assert.equal(page.callbacks.size, 0)
})

test('never restores stale position after navigation, even during Nuxt readiness', async (t) => {
  const page = dom(t)
  const fixture = routerFixture((_to, _from, saved) => saved)
  const waiting = fixture.scroll('/films', '/film/test')
  await Promise.resolve()
  fixture.navigate()
  assert.equal(await waiting, false)
  assert.equal(page.callbacks.size, 0)
  const deferred = routerFixture(async (_to, _from, saved) => {
    await Promise.resolve()
    return saved
  })
  const stale = deferred.scroll('/films', '/film/test')
  deferred.navigate()
  assert.equal(await stale, false)
  assert.equal(page.callbacks.size, 0)
})

for (const [path, id] of [
  ['/films/', 'films-catalog'],
  ['/films/prochainement/', 'upcoming-catalog'],
  [
    '/films/prochainement?vue=historique&annee=2025&mois=10&page=2',
    'upcoming-catalog',
  ],
  ['/planning', 'planning-catalog'],
  ['/recherche?date=2026-10-13', 'search-catalog'],
  ['/film/film-test?language=VF', 'film-catalog'],
  ['/statistiques?film=film-test', 'statistics-catalog'],
] as const) {
  test(`waits for own readiness on ${path}, not a previous page`, async (t) => {
    const page = dom(t)
    const fixture = routerFixture((_to, _from, saved) => saved)
    const waiting = fixture.scroll(path, '/cinemas')
    await Promise.resolve()
    assert.deepEqual(page.queried, [id])
    assert.equal(page.callbacks.size, 1)
    page.render()
    assert.deepEqual(await waiting, { top: 2000, left: 0 })
    assert.equal(page.callbacks.size, 0)
    await fixture.scroll(path, path)
    assert.equal(page.callbacks.size, 0)
  })
}
