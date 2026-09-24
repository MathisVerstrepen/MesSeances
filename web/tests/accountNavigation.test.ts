import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import {
  accountFragmentToken,
  accountTokenPaths,
  createAccountNavigation,
} from '../app/utils/accountNavigation.ts'
import { lifetimeFixture } from './helpers/accountLifetime.ts'
import { ref } from 'vue'

test('token arrivals are single-use, normalized, route-bound and cancelled generations cannot revive', () => {
  const token = 'A'.repeat(43)
  assert.equal(accountFragmentToken(`#token=${token}&token=${token}`), '')
  assert.equal(accountFragmentToken('#token=invalid'), '')
  for (const path of accountTokenPaths) {
    const runtime = createAccountNavigation()
    const initial = runtime.begin(path, path, '', token)
    runtime.finish(initial, path, path, true)
    assert.equal(runtime.take(path), token)
    assert.equal(runtime.take(path), '')
    const stale = runtime.begin(path, path, `#token=${token}`)
    const replacement = runtime.begin(path, path, '')
    runtime.finish(stale, path, path, false)
    runtime.finish(replacement, path, path, true)
    assert.equal(runtime.take('/wrong-route'), '')
    assert.equal(runtime.take(path), token)
    const cancelled = runtime.begin(path, path, `#token=${token}`)
    runtime.finish(cancelled, path, path, false)
    assert.equal(runtime.take(path), '')
    const leaving = runtime.begin(path, path, `#token=${token}`)
    runtime.begin('/connexion', '/connexion', '')
    runtime.finish(leaving, path, path, true)
    assert.equal(runtime.take(path), '')
    const back = runtime.begin(path, path, '')
    runtime.finish(back, path, path, true)
    assert.equal(runtime.take(path), '')
  }
})

test('route lifetime fences stale work before unmount and cleans shared listeners', () => {
  const revision = ref(0)
  const fixture = lifetimeFixture(revision)
  let clears = 0
  const lifetime = fixture.useAccountLifetime(() => clears++)
  const operation = lifetime.capture()
  const routeOnly = lifetime.capture(false)
  revision.value++
  assert.equal(operation(), false)
  assert.equal(routeOnly(), true)
  fixture.runtime.begin('/connexion', '/connexion', '')
  assert.equal(routeOnly(), false)
  assert.equal(clears, 1)
  fixture.unmount()
  assert.equal(fixture.runtime.starts.size, 0)
})

test('account boundary uses router replacement, never document reload', async () => {
  const source = await readFile(
    new URL('../app/middleware/account-boundary.global.ts', import.meta.url),
    'utf8',
  )
  assert.doesNotMatch(source, /external: true|location\.reload/)
  assert.match(source, /replace: true/)
})

test('header selects only the account section with the public navigation active treatment', async () => {
  const source = await readFile(
    new URL('../app/components/AppHeader.vue', import.meta.url),
    'utf8',
  )
  const activeFunction = source.match(/function isActive\([^]*?\n\}/)?.[0]
  assert.ok(activeFunction)
  const compiled = ts.transpileModule(activeFunction, {}).outputText
  for (const [path, active] of [
    ['/compte', true],
    ['/compte/', true],
    ['/compte/confirmer-identite', true],
    ['/compte/confirmer-email', true],
    ['/comptex', false],
    ['/connexion', false],
    ['/inscription', false],
    ['/verification', false],
    ['/finaliser', false],
    ['/planning', false],
    ['/recherche', false],
    ['/films', false],
  ] as const) {
    assert.equal(
      runInNewContext(`${compiled}; isActive('/compte')`, {
        route: { path },
      }),
      active,
      path,
    )
  }

  const accountLink = source.match(
    /<NuxtLink\s+:to="accountHref"[^]*?<\/NuxtLink>/,
  )?.[0]
  const publicLink = source.match(
    /<NuxtLink\s+v-for="link in links"[^]*?<\/NuxtLink>/,
  )?.[0]
  assert.ok(accountLink && publicLink)
  assert.match(accountLink, /:prefetch="false"/)
  assert.match(accountLink, /\? 'Mon compte' : 'Connexion'/)
  assert.match(
    accountLink,
    /:class="isActive\('\/compte'\) \? 'bg-ink text-white' : 'text-ink hover:bg-highlight'"/,
  )
  assert.match(
    accountLink,
    /:aria-current="isActive\('\/compte'\) \? 'page' : undefined"/,
  )
  assert.match(accountLink, /class="nav-link relative /)
  const underlineClasses = (link: string) =>
    link.match(/lg:aria-\[current=page\]:after:[^\s"]+/g)
  assert.ok(underlineClasses(publicLink)?.length)
  assert.deepEqual(underlineClasses(accountLink), underlineClasses(publicLink))
})
