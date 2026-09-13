import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { computed, effectScope, watch } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { cinemaDirectoryQuery, parseCinemaDirectoryQuery } from '../app/utils/cinemaDirectoryQuery.ts'
import { createCinemaDirectoryLocation } from '../app/utils/cinemaDirectoryLocation.ts'

function locationHarness() {
  const requests: { success: PositionCallback, failure: PositionErrorCallback | null | undefined, options: PositionOptions | undefined }[] = []
  const location = createCinemaDirectoryLocation(() => ({
    getCurrentPosition(success, failure, options) {
      requests.push({ success, failure, options })
    }
  }))
  return { location, requests }
}

function position(latitude = 50, accuracy = 20): GeolocationPosition {
  return {
    coords: { latitude, longitude: 3, accuracy, altitude: null, altitudeAccuracy: null, heading: null, speed: null, toJSON: () => ({}) },
    timestamp: 0,
    toJSON: () => ({})
  }
}

function locationError(code: number): GeolocationPositionError {
  return { code, message: '', PERMISSION_DENIED: 1, POSITION_UNAVAILABLE: 2, TIMEOUT: 3 }
}

test('directory query restores all independent mode combinations and omits defaults', () => {
  for (const view of ['list', 'map'] as const) {
    for (const location of ['city', 'nearby'] as const) {
      const query = cinemaDirectoryQuery({}, { view, location, search: ' Lille ' })
      assert.deepEqual(parseCinemaDirectoryQuery(query), { view, location, search: 'Lille' })
      assert.equal(query.view, view === 'map' ? 'map' : undefined)
      assert.equal(query.location, location === 'nearby' ? 'nearby' : undefined)
    }
  }
  assert.deepEqual(cinemaDirectoryQuery({}), {})
  assert.deepEqual(parseCinemaDirectoryQuery({}), { view: 'list', location: 'city', search: '' })
})

test('directory query removes invalid, empty, repeated and explicit default values', () => {
  for (const value of [undefined, null, '', 'unknown', ['map'], ['nearby', 'nearby']]) {
    assert.deepEqual(cinemaDirectoryQuery({ view: value, location: value }), {})
  }
  assert.deepEqual(cinemaDirectoryQuery({ view: 'list', location: 'city', q: ['Lille', 'Paris'] }), {})
  assert.deepEqual(cinemaDirectoryQuery({ view: 'map', location: ['nearby', 'nearby'] }), { view: 'map' })
  assert.deepEqual(cinemaDirectoryQuery({ view: ['map', 'map'], location: 'nearby' }), { location: 'nearby' })
})

test('directory changes preserve unrelated query values and do not mutate input', () => {
  const query = { q: ' Lille ', view: 'map', location: 'nearby', tag: ['one', null, 'two'], flag: null }
  const before = structuredClone(query)
  assert.deepEqual(cinemaDirectoryQuery(query, { view: 'list' }), { q: 'Lille', location: 'nearby', tag: query.tag, flag: null })
  assert.deepEqual(cinemaDirectoryQuery(query, { location: 'city' }), { q: 'Lille', view: 'map', tag: query.tag, flag: null })
  assert.deepEqual(cinemaDirectoryQuery(query, { search: '' }), { view: 'map', location: 'nearby', tag: query.tag, flag: null })
  assert.deepEqual(query, before)
})

test('memory history restores independent modes, replaces search and does not retry on unrelated changes', async () => {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/cinemas', component: {} }] })
  await router.push('/cinemas?tag=kept')
  const { location, requests } = locationHarness()
  const scope = effectScope()
  const state = computed(() => parseCinemaDirectoryQuery(router.currentRoute.value.query))
  scope.run(() => watch(() => state.value.location, (mode) => location.setNearbyMode(mode === 'nearby'), { flush: 'sync' }))
  async function traverse(direction: 'back' | 'forward') {
    await new Promise<void>((resolve) => {
      const remove = router.afterEach(() => { remove(); resolve() })
      router[direction]()
    })
  }
  try {
    await router.push({ query: cinemaDirectoryQuery(router.currentRoute.value.query, { view: 'map' }) })
    await router.push({ query: cinemaDirectoryQuery(router.currentRoute.value.query, { location: 'nearby' }) })
    assert.equal(requests.length, 1)
    requests[0]!.failure?.(locationError(1))
    await router.replace({ query: cinemaDirectoryQuery(router.currentRoute.value.query, { search: 'Lille' }) })
    await router.push({ query: cinemaDirectoryQuery(router.currentRoute.value.query, { view: 'list' }) })
    assert.equal(requests.length, 1)
    assert.equal(location.locationStatus.value, 'failed')
    await traverse('back')
    assert.deepEqual(state.value, { view: 'map', location: 'nearby', search: 'Lille' })
    assert.equal(requests.length, 1)
    await traverse('back')
    assert.deepEqual(state.value, { view: 'map', location: 'city', search: '' })
    assert.equal(location.locationStatus.value, 'idle')
    await traverse('back')
    assert.deepEqual(state.value, { view: 'list', location: 'city', search: '' })
    await traverse('forward')
    await traverse('forward')
    assert.deepEqual(state.value, { view: 'map', location: 'nearby', search: 'Lille' })
    assert.equal(requests.length, 2)
    requests[1]!.success(position())
    assert.deepEqual(router.currentRoute.value.query, { tag: 'kept', q: 'Lille', view: 'map', location: 'nearby' })
  } finally {
    scope.stop()
    location.dispose()
  }
})

test('location is inert before mount activation, requests once, and retains active state across same-mode synchronization', () => {
  const { location, requests } = locationHarness()
  location.retry()
  location.setNearbyMode(false)
  assert.equal(requests.length, 0)
  location.setNearbyMode(true)
  location.retry()
  location.setNearbyMode(true)
  assert.equal(requests.length, 1)
  assert.deepEqual(requests[0]!.options, { enableHighAccuracy: false, timeout: 8000, maximumAge: 600000 })
  requests[0]!.success(position())
  assert.equal(location.locationStatus.value, 'active')
  assert.deepEqual(location.userPosition.value, { latitude: 50, longitude: 3 })
  assert.equal(location.locationAccuracyMeters.value, 20)
  location.setNearbyMode(true)
  assert.equal(requests.length, 1)
  location.setNearbyMode(false)
  assert.equal(location.userPosition.value, null)
  assert.equal(location.locationAccuracyMeters.value, null)
})

test('denial, timeout and unavailable failures retain intent without retry loops and permit explicit retry', () => {
  for (const [code, message] of [[1, 'Localisation refusée'], [2, 'Position indisponible'], [3, 'trop de temps']] as const) {
    const { location, requests } = locationHarness()
    location.setNearbyMode(true)
    requests[0]!.failure?.(locationError(code))
    assert.equal(location.locationStatus.value, 'failed')
    assert.ok(location.locationError.value.includes(message))
    location.setNearbyMode(true)
    assert.equal(requests.length, 1)
    location.retry()
    assert.equal(requests.length, 2)
    assert.equal(location.locationError.value, '')
    requests[0]!.success(position(20))
    assert.equal(location.userPosition.value, null)
    requests[1]!.success(position())
    assert.equal(location.locationStatus.value, 'active')
  }
})

test('missing and throwing browser geolocation fail safely without automatic retries', () => {
  for (const getGeolocation of [() => undefined, () => { throw new Error('browser unavailable') }]) {
    let calls = 0
    const location = createCinemaDirectoryLocation(() => { calls++; return getGeolocation() })
    assert.equal(calls, 0)
    location.setNearbyMode(true)
    assert.equal(location.locationStatus.value, 'failed')
    location.setNearbyMode(true)
    assert.equal(calls, 1)
  }
})

test('stale success and error callbacks cannot restore position after city switch, reentry or unmount', () => {
  const { location, requests } = locationHarness()
  location.setNearbyMode(true)
  location.setNearbyMode(false)
  requests[0]!.success(position())
  requests[0]!.failure?.(locationError(1))
  assert.equal(location.locationStatus.value, 'idle')
  assert.equal(location.userPosition.value, null)
  location.setNearbyMode(true)
  requests[0]!.success(position())
  requests[0]!.failure?.(locationError(1))
  assert.equal(location.locationStatus.value, 'requesting')
  assert.equal(location.userPosition.value, null)
  location.dispose()
  requests[1]!.success(position())
  requests[1]!.failure?.(locationError(1))
  location.setNearbyMode(true)
  location.retry()
  assert.equal(requests.length, 2)
  assert.equal(location.locationStatus.value, 'idle')
  assert.equal(location.userPosition.value, null)
  assert.equal(location.locationError.value, '')
})

test('invalid browser coordinates fail and invalid accuracy is not retained', () => {
  const { location, requests } = locationHarness()
  location.setNearbyMode(true)
  requests[0]!.success(position(91))
  assert.equal(location.locationStatus.value, 'failed')
  assert.equal(location.userPosition.value, null)
  location.retry()
  requests[1]!.success(position(50, Number.NaN))
  assert.equal(location.locationStatus.value, 'active')
  assert.equal(location.locationAccuracyMeters.value, null)
})

test('page wires route-derived display, push controls, replace search, mounted activation and disposal', async () => {
  const page = await readFile(new URL('../app/pages/cinemas.vue', import.meta.url), 'utf8')
  assert.match(page, /const routeState = computed\(\(\) => parseCinemaDirectoryQuery\(route.query\)\)/)
  assert.match(page, /function setDisplayMode[\s\S]*?router\.push\(\{ query \}\)/)
  assert.match(page, /function updateSearch[\s\S]*?router\.replace\(\{ query \}\)/)
  assert.match(page, /watch\(locationMode,[\s\S]*?if \(isMounted\) location\.setNearbyMode[\s\S]*?flush: 'sync'/)
  assert.match(page, /onMounted\(async \(\) => \{\s*isMounted = true\s*location\.setNearbyMode/)
  assert.match(page, /onBeforeUnmount\([\s\S]*?location\.dispose\(\)/)
  assert.match(page, /if \(locationMode.value === 'nearby'\) location.retry\(\)/)
  assert.match(page, /v-if="locationMode === 'nearby' && \(locationStatus === 'active' \|\| locationStatus === 'failed'\)"[^\n]*@click="showByCity"/)
  assert.match(page, /function showList\(\) \{\s*setDisplayMode\(\{ view: 'list' \}\)/)
  assert.doesNotMatch(page, /localStorage|sessionStorage|console\./)
})
