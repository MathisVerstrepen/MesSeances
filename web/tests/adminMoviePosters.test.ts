import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { useAdminMoviePosters } from '../app/composables/useAdminMoviePosters.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type { AdminMoviePoster, AdminMoviePostersResponse } from '../app/types/api.ts'

function poster(name: string, language: string | null = 'fr'): AdminMoviePoster {
  return { url: `https://image.tmdb.org/t/p/w500/${name}.jpg`, width: 1000, height: 1500, language }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((onResolve, onReject) => { resolve = onResolve; reject = onReject })
  return { promise, resolve, reject }
}

test('loads every language and every result without a display limit, rejecting unsafe image URLs and duplicates', async () => {
  const all = Array.from({ length: 120 }, (_, index) => poster(String(index), index % 3 === 0 ? null : index % 2 ? 'ja' : 'fr'))
  const response = deferred<AdminMoviePostersResponse>()
  const picker = useAdminMoviePosters(() => response.promise)
  const loading = picker.load('1')
  assert.equal(picker.status.value, 'loading')
  response.resolve({ posters: [...all, all[0]!, { ...poster('bad'), url: 'https://evil.example/poster.jpg' }, { ...poster('bad'), url: 'https://image.tmdb.org/t/p/w500/../bad.jpg' }, { ...poster('bad'), url: 'https://ugc.fr/poster.jpg' }] })
  await loading
  assert.equal(picker.status.value, 'ready')
  assert.deepEqual(picker.posters.value, all)
})

test('aborts movie changes and ignores a late successful response even when fetch ignores its signal', async () => {
  const first = deferred<AdminMoviePostersResponse>()
  const second = deferred<AdminMoviePostersResponse>()
  const signals: AbortSignal[] = []
  const picker = useAdminMoviePosters((id, signal) => {
    signals.push(signal)
    return id === '1' ? first.promise : second.promise
  })
  const oldLoad = picker.load('1')
  const newLoad = picker.load('2')
  assert.equal(signals[0]?.aborted, true)
  assert.equal(signals[1]?.aborted, false)
  second.resolve({ posters: [poster('second')] })
  await newLoad
  first.resolve({ posters: [poster('first')] })
  await oldLoad
  assert.deepEqual(picker.posters.value, [poster('second')])
  assert.equal(picker.status.value, 'ready')
})

test('close/unmount reset aborts requests, clears images, and ignores late errors across reopening', async () => {
  const first = deferred<AdminMoviePostersResponse>()
  let signal: AbortSignal | undefined
  let calls = 0
  const picker = useAdminMoviePosters((_id, requestSignal) => {
    signal = requestSignal
    return ++calls === 1 ? first.promise : Promise.resolve({ posters: [poster('reopened')] })
  })
  const loading = picker.load('1')
  picker.reset()
  assert.equal(signal?.aborted, true)
  assert.equal(picker.status.value, 'idle')
  assert.deepEqual(picker.posters.value, [])
  await picker.load('1')
  first.reject(new Error('late failure'))
  await loading
  assert.equal(picker.status.value, 'ready')
  assert.deepEqual(picker.posters.value, [poster('reopened')])
  picker.reset()
  assert.deepEqual(picker.posters.value, [])
})

test('shows errors without exposing upstream messages and retries to an empty or populated result', async () => {
  let calls = 0
  const picker = useAdminMoviePosters(() => {
    calls += 1
    if (calls === 1) return Promise.reject(new Error('private upstream detail'))
    return Promise.resolve({ posters: calls === 2 ? [] : [poster('retry')] })
  })
  await picker.load('1')
  assert.equal(picker.status.value, 'error')
  assert.deepEqual(picker.posters.value, [])
  await picker.load('1')
  assert.equal(picker.status.value, 'ready')
  assert.deepEqual(picker.posters.value, [])
  await picker.load('1')
  assert.deepEqual(picker.posters.value, [poster('retry')])
})

test('poster API GET encodes movie IDs, includes credentials and signal, and leaves retry to the UI', async () => {
  interface PosterFetchOptions {
    credentials: 'include'
    signal?: AbortSignal
    retry: false
  }
  const controller = new AbortController()
  const response = { posters: [poster('api')] }
  const calls: Array<{ url: string, options: PosterFetchOptions }> = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: (url: string, options: PosterFetchOptions) => {
      calls.push({ url, options })
      return Promise.resolve(response)
    }
  })
  assert.deepEqual(await useMesSeancesApi().adminMoviePosters('9007199254740993/a', controller.signal), response)
  assert.deepEqual(calls, [{
    url: 'http://localhost:8080/api/v1/admin/movies/9007199254740993%2Fa/posters',
    options: { credentials: 'include', signal: controller.signal, retry: false }
  }])
})

test('dialog wires native focus, dismissal, cancellation and draft-only selection through safe lazy previews', async () => {
  const [picker, grid, api, image] = await Promise.all([
    readFile(new URL('../app/components/admin/AdminMoviePosterPicker.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/components/admin/AdminMoviesGrid.client.vue', import.meta.url), 'utf8'),
    readFile(new URL('../app/composables/useMesSeancesApi.ts', import.meta.url), 'utf8'),
    readFile(new URL('../app/components/PosterImage.vue', import.meta.url), 'utf8')
  ])
  assert.match(picker, /<dialog/)
  assert.match(picker, /dialog.value.showModal\(\)/)
  assert.match(picker, /:aria-labelledby="titleId"/)
  assert.match(picker, /aria-haspopup="dialog"/)
  assert.match(picker, /:aria-pressed=/)
  assert.match(picker, /closeButton.value\?\.focus/)
  assert.match(picker, /trigger\?\.isConnected/)
  assert.match(picker, /@cancel.prevent="closeModal\(\)"/)
  assert.match(picker, /@click.self="closeModal\(\)"/)
  assert.match(picker, /watch\(\(\) => props.movieId, \(\) => closeModal/)
  assert.match(picker, /onBeforeUnmount\(\(\) => closeModal/)
  assert.match(picker, /function closeModal[^]*?reset\(\)/)
  assert.match(picker, /emit\('select', url\)/)
  assert.doesNotMatch(picker, /adminUpdateMovie|\bPATCH\b|<img|v-html/)
  assert.match(picker, /<PosterImage :src="poster.url"/)
  assert.match(image, /loading="lazy"/)
  assert.match(grid, /<AdminMoviePosterPicker[^>]+:key="selectedDetailsItem.id"[^>]+@select="selectPoster"/)
  assert.match(grid, /function selectPoster\(url: string\) \{[^}]+updateDraft\(item, 'poster_url', url\)/)
  assert.match(api, /adminMoviePosters\(id: string, signal\?: AbortSignal\) \{\s+return withAdminRedirect/)
})
