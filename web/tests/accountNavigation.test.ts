import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
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
