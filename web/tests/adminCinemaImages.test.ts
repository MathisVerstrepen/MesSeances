import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext, type Context } from 'node:vm'
import ts from 'typescript'
import { computed, ref, watch, effectScope, nextTick, type Ref } from 'vue'
import * as queryUtils from '../app/utils/routeQuery.ts'
import * as images from '../app/utils/adminCinemaImages.ts'
import * as cinemaFilters from '../app/utils/adminCinemaFilters.ts'
import type { LocationQuery } from 'vue-router'
import type { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type {
  AdminTheater,
  AdminTheatersResponse,
  AdminTheaterImageResult,
  AdminTheatersQuery,
} from '../app/types/api.ts'

async function compile<T>(
  path: string,
  globals: Context = {},
  suffix = '',
): Promise<T> {
  const raw = await readFile(new URL(path, import.meta.url), 'utf8')
  const source = raw.includes('<script')
    ? raw.split('<script setup lang="ts">')[1]!.split('</script>')[0]!
    : raw
  const exports = {}
  runInNewContext(
    ts.transpileModule(
      source
        .replaceAll('import.meta.server', 'false')
        .replaceAll('import.meta.client', 'true') + suffix,
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      },
    ).outputText,
    {
      exports,
      require: () => ({}),
      Blob,
      File,
      FormData,
      URL,
      AbortController,
      AbortSignal,
      TextEncoder,
      Uint8Array,
      Error,
      ...globals,
    },
  )
  // SAFETY: each fixture selects the exports declared by its compiled source.
  return exports as T
}

const utility = images
const file = () => new File(['synthetic'], 'cinema.png', { type: 'image/png' })
const settle = () => new Promise<void>((resolve) => setImmediate(resolve))
const path = utility.adminCinemaImagePath('ugc', '42')
const imageResult: AdminTheaterImageResult = {
  image_revision: 1,
  image: { url: `${path}/1`, width: 800, height: 400, size_bytes: 100 },
}

test('file validation bounds input before Blob allocation, accepts exact cap and rejects unsupported MIME', () => {
  assert.equal(utility.cinemaImageFileError(file()), '')
  assert.equal(
    utility.cinemaImageFileError(
      new File([new Uint8Array(5242880)], 'a.webp', { type: 'image/webp' }),
    ),
    '',
  )
  for (const invalid of [
    new File([], 'empty.png', { type: 'image/png' }),
    new File(['x'], 'x.svg', { type: 'image/svg+xml' }),
    new File(['x'], 'x.png', { type: 'text/html' }),
    new File(['x'], 'x.jpg'),
    new File([new Uint8Array(5242881)], 'x.jpg', { type: 'image/jpeg' }),
  ])
    assert.ok(utility.cinemaImageFileError(invalid))
})

test('URL validation requires bounded direct HTTPS source without browser fetching', () => {
  for (const valid of [
    'https://photos.example.test/a.png?size=2',
    'https://example.test:443/%20photo.webp',
  ])
    assert.equal(utility.cinemaImageURLError(valid), '')
  for (const invalid of [
    '',
    'http://example.test/a',
    'https://user:pass@example.test/a',
    'https://example.test/a#part',
    'https://127.0.0.1/a',
    'https://[::1]/a',
    'https://example.test:444/a',
    'https://localhost/a',
    'https://example.test./a',
    'https://example.test/a b',
    'https://example.test/\\a',
    `https://example.test/${'a'.repeat(2048)}`,
  ])
    assert.ok(utility.cinemaImageURLError(invalid), invalid)
})

test('preview binds canonical safe revision, provider identity and same browser origin', () => {
  const origin = 'https://messeances.fr'
  assert.equal(
    utility.cinemaImagePreviewURL('', `${path}/1`, 'ugc', '42', 1, origin),
    `${origin}${path}/1`,
  )
  assert.equal(
    utility.cinemaImagePreviewURL(origin, `${path}/1`, 'ugc', '42', 1, origin),
    `${origin}${path}/1`,
  )
  for (const unsafe of [
    `https://evil.test${path}/1`,
    `//evil.test${path}/1`,
    `${path}/01`,
    `${path}/1?x=1`,
    `${path}/1#x`,
    `${path}/2`,
    `${path}/%31`,
    '/api/v1/admin/theaters/pathe/42/image/1',
  ])
    assert.equal(
      utility.cinemaImagePreviewURL('', unsafe, 'ugc', '42', 1, origin),
      '',
    )
  for (const revision of [0, -1, 1.5, Number.MAX_SAFE_INTEGER + 1])
    assert.equal(
      utility.cinemaImagePreviewURL(
        '',
        `${path}/${revision}`,
        'ugc',
        '42',
        revision,
        origin,
      ),
      '',
    )
  assert.equal(
    utility.cinemaImagePreviewURL(
      'https://api.other.test',
      `${path}/1`,
      'ugc',
      '42',
      1,
      origin,
    ),
    '',
  )
  assert.equal(
    utility.adminCinemaImagePath('ugc', 'a/b?x'),
    '/api/v1/admin/theaters/ugc/a%2Fb%3Fx/image',
  )
})

test('local candidate replacement and clearing revoke every allocated Blob exactly once', async () => {
  let created = 0
  const revoked: string[] = []
  const local = await compile<typeof images>(
    '../app/utils/adminCinemaImages.ts',
    {
      URL: {
        createObjectURL: () => `blob:${++created}`,
        revokeObjectURL: (url: string) => revoked.push(url),
      },
    },
  )
  const draft = local.newCinemaImageDraft()
  assert.equal(local.selectCinemaImageFile(draft, file()), '')
  assert.equal(draft.candidate, 'blob:1')
  local.selectCinemaImageFile(draft, file())
  assert.deepEqual(revoked, ['blob:1'])
  assert.ok(
    local.selectCinemaImageFile(
      draft,
      new File(['x'], 'bad.svg', { type: 'image/svg+xml' }),
    ),
  )
  assert.equal(draft.candidate, 'blob:2')
  assert.ok(draft.file)
  assert.deepEqual(revoked, ['blob:1'])
  draft.url = 'https://private.example.test/source'
  local.clearCinemaImageDraft(draft)
  local.clearCinemaImageDraft(draft)
  assert.equal(draft.file, null)
  assert.equal(draft.url, '')
  assert.deepEqual(revoked, ['blob:1', 'blob:2'])
  local.selectCinemaImageFile(
    draft,
    new File(['x'], 'bad.svg', { type: 'image/svg+xml' }),
  )
  assert.equal(created, 2)
})

interface UploadEvents {
  onprogress?:
    | ((event: {
        lengthComputable: boolean
        loaded: number
        total: number
      }) => void)
    | null
  onload?: (() => void) | null
}
class FakeXHR {
  static instances: FakeXHR[] = []
  upload: UploadEvents = {}
  headers: Record<string, string> = {}
  withCredentials = false
  timeout = 0
  status = 200
  responseText = JSON.stringify(imageResult)
  responseURL = `https://messeances.fr${path}`
  onload?: (() => void) | null
  onabort?: (() => void) | null
  onerror?: (() => void) | null
  ontimeout?: (() => void) | null
  onprogress?: ((event: { loaded: number }) => void) | null
  method = ''
  target = ''
  body: FormData | null = null
  constructor() {
    FakeXHR.instances.push(this)
  }
  open(method: string, target: string) {
    this.method = method
    this.target = target
  }
  setRequestHeader(key: string, value: string) {
    this.headers[key] = value
  }
  send(body: FormData) {
    this.body = body
  }
  abort() {
    this.onabort?.()
  }
}

async function uploadFixture() {
  const transport = await compile<typeof images>(
    '../app/utils/adminCinemaImages.ts',
    {
      XMLHttpRequest: FakeXHR,
      window: { location: { origin: 'https://messeances.fr' } },
    },
  )
  const controller = new AbortController()
  const progress: (number | null)[] = []
  const pending = transport.uploadAdminCinemaImage(
    '',
    'ugc',
    '42',
    file(),
    0,
    controller.signal,
    (n) => progress.push(n),
  )
  return {
    transport,
    controller,
    progress,
    pending,
    xhr: FakeXHR.instances.at(-1)!,
  }
}

test('upload sends strict multipart identity/revision, credentials, real progress and no account CSRF or content type', async () => {
  const f = await uploadFixture()
  assert.equal(f.xhr.method, 'POST')
  assert.equal(f.xhr.target, path)
  assert.equal(f.xhr.withCredentials, true)
  assert.equal(f.xhr.timeout, 25000)
  assert.deepEqual(Object.keys(f.xhr.headers), [])
  assert.deepEqual([...f.xhr.body!.keys()], ['expected_revision', 'image'])
  assert.equal(f.xhr.body!.get('expected_revision'), '0')
  assert.ok(f.xhr.body!.get('image') instanceof File)
  f.xhr.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 })
  f.xhr.upload.onprogress?.({ lengthComputable: false, loaded: 7, total: 0 })
  f.xhr.upload.onprogress?.({
    lengthComputable: true,
    loaded: 999,
    total: 1000,
  })
  f.xhr.upload.onload?.()
  assert.deepEqual(f.progress, [50, null, 99, 100])
  f.xhr.onload?.()
  assert.equal((await f.pending).image_revision, 1)
  assert.equal(f.xhr.onload, null)
  assert.equal(f.xhr.upload.onprogress, null)
})

test('upload rejects redirected, oversized, malformed and mismatched results; sanitized errors never retain raw response', async () => {
  for (const configure of [
    (xhr: FakeXHR) => {
      xhr.responseURL = 'https://evil.test/image'
    },
    (xhr: FakeXHR) => {
      xhr.responseText = 'x'.repeat(4097)
    },
    (xhr: FakeXHR) => {
      xhr.responseText = 'not json'
    },
    (xhr: FakeXHR) => {
      xhr.responseText = JSON.stringify({ ...imageResult, image_revision: 0 })
    },
    (xhr: FakeXHR) => {
      xhr.responseText = JSON.stringify({
        ...imageResult,
        image: { ...imageResult.image, url: 'https://evil.test/image' },
      })
    },
    (xhr: FakeXHR) => {
      xhr.status = 409
      xhr.responseText =
        '{"error":{"code":"cinema_image_conflict","message":"private URL/password"}}'
    },
  ]) {
    const f = await uploadFixture()
    configure(f.xhr)
    f.xhr.onload?.()
    await assert.rejects(f.pending, (cause: unknown) => {
      assert.ok(cause instanceof f.transport.AdminCinemaImageError)
      assert.doesNotMatch(JSON.stringify(cause), /private|password|evil/)
      if (f.xhr.status === 409)
        assert.equal(cause.code, 'cinema_image_conflict')
      return true
    })
  }
})

test('upload cancellation, timeout, network error and response overrun detach all callbacks without retries', async () => {
  for (const fail of ['abort', 'timeout', 'network', 'overrun'] as const) {
    const f = await uploadFixture()
    if (fail === 'abort') f.controller.abort()
    if (fail === 'timeout') f.xhr.ontimeout?.()
    if (fail === 'network') f.xhr.onerror?.()
    if (fail === 'overrun') f.xhr.onprogress?.({ loaded: 4097 })
    await assert.rejects(f.pending, f.transport.AdminCinemaImageError)
    assert.equal(f.xhr.onload, null)
    assert.equal(f.xhr.upload.onload, null)
  }
  const f = await uploadFixture()
  f.xhr.onload?.()
  await f.pending
  const count = FakeXHR.instances.length
  const aborted = new AbortController()
  aborted.abort()
  await assert.rejects(
    f.transport.uploadAdminCinemaImage(
      '',
      'ugc',
      '42',
      file(),
      0,
      aborted.signal,
      () => {},
    ),
  )
  assert.equal(FakeXHR.instances.length, count)
})

test('stored preview fetch includes cookies, prevents redirects, caps bytes and fences canceled reads', async () => {
  let calls = 0
  let response = () =>
    new Response('stored-webp', { headers: { 'Content-Type': 'image/webp' } })
  const transport = await compile<typeof images>(
    '../app/utils/adminCinemaImages.ts',
    {
      window: { location: { origin: 'https://messeances.fr' } },
      fetch: async (url: string, options: RequestInit) => {
        calls++
        assert.equal(url, `https://messeances.fr${path}/1`)
        assert.equal(options.credentials, 'include')
        assert.equal(options.cache, 'no-store')
        assert.equal(options.redirect, 'error')
        assert.equal(options.referrerPolicy, 'no-referrer')
        return response()
      },
    },
  )
  const signal = new AbortController().signal
  assert.equal(
    (
      await transport.fetchAdminCinemaImage(
        `https://messeances.fr${path}/1`,
        signal,
      )
    ).type,
    'image/webp',
  )
  await assert.rejects(
    transport.fetchAdminCinemaImage('https://evil.test/image', signal),
  )
  assert.equal(calls, 1)
  for (const make of [
    () => new Response('bad', { headers: { 'Content-Type': 'text/html' } }),
    () =>
      new Response(new Uint8Array(1048577), {
        headers: { 'Content-Type': 'image/webp' },
      }),
    () =>
      new Response('small', {
        headers: { 'Content-Type': 'image/webp', 'Content-Length': '1048577' },
      }),
    () =>
      Response.json(
        { error: { code: 'unauthorized', message: 'secret' } },
        { status: 401 },
      ),
  ]) {
    response = make
    await assert.rejects(
      transport.fetchAdminCinemaImage(`https://messeances.fr${path}/1`, signal),
      (cause: unknown) => {
        assert.ok(cause instanceof transport.AdminCinemaImageError)
        assert.doesNotMatch(JSON.stringify(cause), /secret/)
        return true
      },
    )
  }
  const canceled = new AbortController()
  response = () => {
    canceled.abort()
    return new Response('x', { headers: { 'Content-Type': 'image/webp' } })
  }
  await assert.rejects(
    transport.fetchAdminCinemaImage(
      `https://messeances.fr${path}/1`,
      canceled.signal,
    ),
  )
})

test('client methods use fixed endpoints, credentialed abortable requests, no retry and existing 401 redirect', async () => {
  const calls: {
    url: string
    options: RequestInit & {
      query?: AdminTheatersQuery
      retry?: boolean
    }
  }[] = []
  const redirects: string[] = []
  let expired = false
  const compiled = await compile<{ useMesSeancesApi: typeof useMesSeancesApi }>(
    '../app/composables/useMesSeancesApi.ts',
    {
      require: () => ({
        ...utility,
        uploadAdminCinemaImage: async () => {
          throw { status: 401 }
        },
      }),
      useRuntimeConfig: () => ({ public: { apiBase: '' } }),
      useRoute: () => ({ path: '/admin/cinemas' }),
      navigateTo: async (url: string) => {
        redirects.push(url)
      },
      getApiErrorStatus: (cause: { status?: number }) => cause.status,
      $fetch: async (url: string, options: RequestInit) => {
        calls.push({ url, options })
        if (expired) throw { status: 401 }
        return imageResult
      },
    },
  )
  const api = compiled.useMesSeancesApi()
  const signal = new AbortController().signal
  await api.adminTheaters(
    { limit: 20, offset: 40, q: 'Lille', provider: 'ugc' },
    signal,
  )
  await api.adminImportTheaterImage(
    'ugc',
    'a/b',
    { expected_revision: 1, url: 'https://example.test/photo' },
    signal,
  )
  await api.adminRemoveTheaterImage(
    'ugc',
    'a/b',
    { expected_revision: 1 },
    signal,
  )
  assert.equal(calls[0]!.url, '/api/v1/admin/theaters')
  assert.equal(calls[0]!.options.query?.offset, 40)
  assert.equal(calls[0]!.options.query?.q, 'Lille')
  assert.equal(calls[0]!.options.query?.provider, 'ugc')
  assert.equal(calls[1]!.url, '/api/v1/admin/theaters/ugc/a%2Fb/image/import')
  assert.equal(calls[1]!.options.method, 'POST')
  assert.equal(calls[2]!.options.method, 'DELETE')
  for (const call of calls) {
    assert.equal(call.options.credentials, 'include')
    assert.equal(call.options.signal, signal)
    assert.equal(call.options.retry, false)
    assert.equal(call.options.cache, 'no-store')
  }
  expired = true
  await assert.rejects(api.adminTheaters({ limit: 20, offset: 0 }, signal))
  await assert.rejects(
    api.adminUploadTheaterImage('ugc', '42', file(), 0, signal, () => {}),
  )
  assert.deepEqual(redirects, ['/admin/login', '/admin/login'])
})

interface PageModel {
  result: Ref<AdminTheatersResponse | null>
  page: Ref<number>
  search: Ref<string>
  provider: Ref<string>
  hasFilters: Ref<boolean>
  pending: Ref<boolean>
  loadError: Ref<string>
  successMessage: Ref<string>
  editor: (item: AdminTheater) => images.CinemaImageDraft & {
    error: string
    pending: string | null
    confirmation: boolean
    preview: string
    previewState: string
  }
  switchSource: (item: AdminTheater, source: 'file' | 'url') => void
  mutate: (item: AdminTheater, remove?: boolean) => Promise<void>
  loadInventory: (background?: boolean) => Promise<boolean>
  applyRoute: () => Promise<void>
  changeSearch: () => void
  changeProvider: (event: Event) => void
  resetFilters: () => void
  changePage: (page: number) => void
}

function theater(): AdminTheater {
  return {
    provider: 'ugc',
    provider_theater_id: '42',
    theater_id: 'generation-1',
    slug: 'cinema',
    name: 'Le cinéma',
    city: 'Lille',
    address: '12 rue Test',
    postal_code: '59000',
    image_revision: 0,
    image: null,
  }
}

async function pageFixture(initialQuery: LocationQuery = {}) {
  let mount = () => {}
  let unmount = () => {}
  let leave = () => {}
  let reads = 0
  let failure = ''
  let failureStatus = 409
  let revision = 0
  let uploads = 0
  let imports = 0
  let removals = 0
  let importsEnabled = true
  let previewCreated = 0
  let previewReads = 0
  let previewSignal: AbortSignal | undefined
  let deferPreview = false
  const resolvePreviews: ((value: Blob) => void)[] = []
  let storedImage = false
  let total = 41
  let empty = false
  let resolveWrite: ((result: AdminTheaterImageResult) => void) | null = null
  let deferred = false
  let writeSignal: AbortSignal | undefined
  let listFailure = false
  let deferList = false
  let resolveList: ((value: AdminTheatersResponse) => void) | null = null
  const revoked: string[] = []
  const query = ref<LocationQuery>(initialQuery)
  const listCalls: { query: AdminTheatersQuery; signal: AbortSignal }[] = []
  const navigations: { method: string; query: LocationQuery }[] = []
  let clock = 0
  let timerID = 0
  const timers = new Map<number, { at: number; callback: () => void }>()
  class SelectElement {
    value: string
    constructor(value: string) {
      this.value = value
    }
  }
  const scope = effectScope()
  const pageUtility = {
    ...utility,
    fetchAdminCinemaImage: async (_url: string, signal: AbortSignal) => {
      previewReads++
      previewSignal = signal
      if (deferPreview)
        return new Promise<Blob>((resolve) => {
          resolvePreviews.push(resolve)
        })
      return new Blob(['stored-webp'], { type: 'image/webp' })
    },
    clearCinemaImageDraft: (draft: images.CinemaImageDraft) => {
      if (draft.candidate) revoked.push(draft.candidate)
      draft.candidate = ''
      draft.url = ''
      draft.file = null
    },
  }
  const exports = await compile<{ model: PageModel }>(
    '../app/pages/admin/cinemas.vue',
    {
      require: (name: string) =>
        name.includes('adminCinemaImages')
          ? pageUtility
          : name.includes('adminCinemaFilters')
            ? cinemaFilters
            : name.includes('routeQuery')
              ? queryUtils
              : {},
      ref,
      computed,
      HTMLSelectElement: SelectElement,
      setTimeout: (callback: () => void, delay: number) => {
        timers.set(++timerID, { at: clock + delay, callback })
        return timerID
      },
      clearTimeout: (id: number) => timers.delete(id),
      watch: (...args: Parameters<typeof watch>) =>
        scope.run(() => watch(...args)),
      useRoute: () => ({
        get query() {
          return query.value
        },
      }),
      useRouter: () => ({
        replace: async ({ query: next }: { query: LocationQuery }) => {
          navigations.push({ method: 'replace', query: next })
          query.value = next
        },
        push: async ({ query: next }: { query: LocationQuery }) => {
          navigations.push({ method: 'push', query: next })
          query.value = next
        },
      }),
      definePageMeta: (meta: { middleware: string }) =>
        assert.equal(meta.middleware, 'admin-auth'),
      useHead: () => {},
      useRuntimeConfig: () => ({ public: { apiBase: '' } }),
      onMounted: (fn: () => void) => {
        mount = fn
      },
      onBeforeUnmount: (fn: () => void) => {
        unmount = fn
      },
      onBeforeRouteLeave: (fn: () => void) => {
        leave = fn
      },
      URL: {
        createObjectURL: () => `blob:stored-${++previewCreated}`,
        revokeObjectURL: (url: string) => revoked.push(url),
      },
      window: { location: { origin: 'https://messeances.fr' } },
      getApiErrorStatus: (cause: { status?: number }) => cause.status,
      getApiErrorCode: (cause: { data?: { error?: { code?: string } } }) =>
        cause.data?.error?.code,
      useMesSeancesApi: () => ({
        adminTheaters: async (
          query: AdminTheatersQuery,
          signal: AbortSignal,
        ) => {
          reads++
          listCalls.push({ query: { ...query }, signal })
          if (listFailure) throw { status: 503 }
          if (deferList) {
            deferList = false
            return new Promise<AdminTheatersResponse>((resolve) => {
              resolveList = resolve
            })
          }
          return {
            items: empty
              ? []
              : [
                  {
                    ...theater(),
                    image_revision: revision,
                    image: storedImage
                      ? { ...imageResult.image!, url: `${path}/${revision}` }
                      : null,
                  },
                ],
            limit: 20,
            offset: 0,
            total,
            imports_enabled: importsEnabled,
          }
        },
        adminUploadTheaterImage: async (
          _provider: string,
          _id: string,
          _file: File,
          expected: number,
          signal: AbortSignal,
        ) => {
          uploads++
          writeSignal = signal
          if (failure) {
            if (failure === 'cinema_image_conflict') revision = 2
            throw new utility.AdminCinemaImageError(failureStatus, failure)
          }
          if (deferred)
            return new Promise<AdminTheaterImageResult>((resolve) => {
              resolveWrite = resolve
            })
          assert.equal(expected, revision)
          return { image_revision: ++revision, image: null }
        },
        adminImportTheaterImage: async (
          _provider: string,
          _id: string,
          input: { expected_revision: number; url: string },
          signal: AbortSignal,
        ) => {
          imports++
          writeSignal = signal
          assert.equal(input.expected_revision, revision)
          assert.equal(input.url, 'https://photos.example.test/cinema.png')
          if (failure)
            throw new utility.AdminCinemaImageError(failureStatus, failure)
          storedImage = true
          return {
            image_revision: ++revision,
            image: { ...imageResult.image!, url: `${path}/${revision}` },
          }
        },
        adminRemoveTheaterImage: async (
          _provider: string,
          _id: string,
          input: { expected_revision: number },
          signal: AbortSignal,
        ) => {
          removals++
          writeSignal = signal
          assert.equal(input.expected_revision, revision)
          storedImage = false
          return { image_revision: ++revision, image: null }
        },
      }),
    },
    '\nexports.model = { result, editor, switchSource, mutate, loadInventory, applyRoute, page, search, provider, hasFilters, changeSearch, changeProvider, resetFilters, changePage, pending, loadError, successMessage };',
  )
  mount()
  await nextTick()
  await nextTick()
  const model = exports.model
  return {
    model,
    query,
    listCalls,
    navigations,
    selectProvider: (value: string) => {
      const event = new Event('change')
      Object.defineProperty(event, 'target', {
        value: new SelectElement(value),
      })
      model.changeProvider(event)
    },
    advance: async (ms: number) => {
      clock += ms
      for (const [id, timer] of timers) {
        if (timer.at > clock) continue
        timers.delete(id)
        timer.callback()
      }
      await settle()
    },
    timerCount: () => timers.size,
    revoked,
    reads: () => reads,
    uploads: () => uploads,
    imports: () => imports,
    removals: () => removals,
    previewReads: () => previewReads,
    previewCreated: () => previewCreated,
    previewSignal: () => previewSignal,
    deferPreview: () => {
      deferPreview = true
    },
    finishPreview: () => {
      for (const resolve of resolvePreviews.splice(0))
        resolve(new Blob(['stored-webp'], { type: 'image/webp' }))
    },
    image: () => {
      storedImage = true
      revision = 1
    },
    importsDisabled: () => {
      importsEnabled = false
    },
    empty: () => {
      empty = true
      total = 0
    },
    signal: () => writeSignal,
    fail: (code: string, status = 409) => {
      failure = code
      failureStatus = status
    },
    listFail: () => {
      listFailure = true
    },
    deferList: () => {
      deferList = true
    },
    finishList: () => {
      resolveList?.({
        items: [theater()],
        limit: 20,
        offset: 0,
        total: 41,
        imports_enabled: true,
      })
    },
    defer: () => {
      deferred = true
    },
    finish: () => {
      resolveWrite?.({ image_revision: 1, image: null })
    },
    leave,
    stop: () => {
      unmount()
      scope.stop()
    },
  }
}

test('page preserves candidate on conflict, refreshes current revision and never automatically replays upload', async () => {
  const f = await pageFixture()
  try {
    const item = f.model.result.value!.items[0]!
    const draft = f.model.editor(item)
    draft.file = file()
    draft.candidate = 'blob:candidate'
    f.fail('cinema_image_conflict')
    await f.model.mutate(item)
    assert.equal(f.uploads(), 1)
    assert.equal(f.reads(), 2)
    assert.equal(draft.pending, null)
    assert.equal(draft.candidate, 'blob:candidate')
    assert.ok(draft.file)
    assert.equal(f.model.result.value!.items[0]!.image_revision, 2)
    assert.match(draft.error, /actualisée/)
    assert.match(draft.error, /conservée/)
    f.listFail()
    await f.model.mutate(f.model.result.value!.items[0]!)
    assert.match(draft.error, /Actualisation impossible/)
    assert.ok(f.model.loadError.value)
  } finally {
    f.stop()
  }
})

test('page serializes each row, updates returned revision only, clears successful candidate and announces result', async () => {
  const f = await pageFixture()
  try {
    const item = f.model.result.value!.items[0]!
    const draft = f.model.editor(item)
    draft.file = file()
    draft.candidate = 'blob:candidate'
    f.defer()
    const saving = f.model.mutate(item)
    await f.model.mutate(item)
    f.model.switchSource(item, 'url')
    assert.equal(draft.source, 'file')
    assert.equal(f.uploads(), 1)
    f.finish()
    await saving
    assert.equal(draft.pending, null)
    assert.equal(draft.file, null)
    assert.deepEqual(f.revoked, ['blob:candidate'])
    assert.equal(item.image_revision, 1)
    assert.match(f.model.successMessage.value, /Image enregistrée/)
  } finally {
    f.stop()
  }
})

test('page import renders only committed server preview; remove requires inline confirmation and revokes stored preview', async () => {
  const f = await pageFixture()
  try {
    const item = f.model.result.value!.items[0]!
    const draft = f.model.editor(item)
    f.model.switchSource(item, 'url')
    draft.url = 'https://photos.example.test/cinema.png'
    assert.equal(f.previewReads(), 0)
    await f.model.mutate(item)
    await nextTick()
    assert.equal(f.imports(), 1)
    assert.equal(f.previewReads(), 1)
    assert.equal(draft.url, '')
    assert.equal(draft.previewState, 'ready')
    assert.equal(draft.preview, 'blob:stored-1')
    assert.equal(item.image_revision, 1)
    await f.model.mutate(item, true)
    assert.equal(f.removals(), 0)
    draft.confirmation = true
    await f.model.mutate(item, true)
    assert.equal(f.removals(), 1)
    assert.equal(item.image_revision, 2)
    assert.equal(item.image, null)
    assert.equal(draft.preview, '')
    assert.equal(draft.previewState, 'empty')
    assert.deepEqual(f.revoked, ['blob:stored-1'])
    assert.match(f.model.successMessage.value, /Image supprimée/)
  } finally {
    f.stop()
  }
})

test('validation/network/decode failures keep selected candidate and prior stored image intact', async () => {
  for (const [status, code] of [
    [0, ''],
    [422, 'cinema_image_invalid'],
    [415, 'cinema_image_unsupported'],
    [503, 'cinema_images_unavailable'],
  ] as const) {
    const f = await pageFixture()
    try {
      f.image()
      await f.model.loadInventory(true)
      await nextTick()
      const item = f.model.result.value!.items[0]!
      const state = f.model.editor(item)
      state.file = file()
      state.candidate = 'blob:candidate'
      const prior = item.image
      const preview = state.preview
      f.fail(code, status)
      // Empty error code models a network error without arbitrary server copy.
      if (!code) f.fail('network_error', status)
      await f.model.mutate(item)
      assert.equal(state.pending, null)
      assert.ok(state.file)
      assert.equal(state.candidate, 'blob:candidate')
      assert.equal(state.preview, preview)
      assert.equal(item.image, prior)
      assert.equal(item.image_revision, 1)
      assert.ok(state.error)
      assert.equal(f.reads(), 2)
      assert.deepEqual(f.revoked, [])
    } finally {
      f.stop()
    }
  }
})

test('imports unavailable disables only URL source; list errors preserve inventory and candidates, empty inventory is explicit', async () => {
  const f = await pageFixture()
  try {
    f.importsDisabled()
    await f.model.loadInventory(true)
    const item = f.model.result.value!.items[0]!
    const state = f.model.editor(item)
    state.file = file()
    state.candidate = 'blob:candidate'
    f.model.switchSource(item, 'url')
    assert.equal(state.source, 'file')
    f.listFail()
    assert.equal(await f.model.loadInventory(true), false)
    assert.equal(f.model.pending.value, false)
    assert.equal(f.model.result.value!.items[0]!, item)
    assert.ok(f.model.loadError.value)
    assert.equal(state.candidate, 'blob:candidate')
    await f.model.mutate(item)
    assert.equal(f.uploads(), 1)
    assert.equal(state.candidate, '')
  } finally {
    f.stop()
  }
  const empty = await pageFixture()
  try {
    empty.empty()
    assert.equal(await empty.model.loadInventory(true), true)
    assert.equal(empty.model.result.value!.items.length, 0)
    assert.equal(empty.model.result.value!.total, 0)
    assert.equal(empty.model.pending.value, false)
    assert.equal(empty.model.loadError.value, '')
  } finally {
    empty.stop()
  }
})

test('pending preview reads abort on page/filter/unmount and cannot allocate late stored Blobs', async () => {
  for (const reason of ['page', 'q', 'provider', 'unmount'] as const) {
    const f = await pageFixture()
    try {
      f.image()
      f.deferPreview()
      await f.model.loadInventory(true)
      const item = f.model.result.value!.items[0]!
      const state = f.model.editor(item)
      assert.equal(state.previewState, 'loading')
      const signal = f.previewSignal()
      if (reason !== 'unmount') {
        f.query.value =
          reason === 'page'
            ? { page: '2' }
            : reason === 'q'
              ? { q: 'Lille' }
              : { provider: 'ugc' }
        await settle()
      } else f.stop()
      assert.equal(signal?.aborted, true)
      f.finishPreview()
      await settle()
      assert.equal(state.preview, '')
      assert.equal(f.previewCreated(), reason !== 'unmount' ? 1 : 0)
      assert.deepEqual(f.revoked, [])
    } finally {
      f.stop()
    }
  }
})

test('background inventory cannot roll back a newer successful row edit or overwrite a changed page', async () => {
  const f = await pageFixture()
  try {
    const item = f.model.result.value!.items[0]!
    const state = f.model.editor(item)
    state.file = file()
    f.deferList()
    const staleRead = f.model.loadInventory(true)
    await f.model.mutate(item)
    assert.equal(item.image_revision, 1)
    f.finishList()
    assert.equal(await staleRead, true)
    assert.equal(f.model.result.value!.items[0]!.image_revision, 1)
    f.deferList()
    const oldPageRead = f.model.loadInventory(true)
    f.query.value = { page: '2' }
    await settle()
    const newPageResult = f.model.result.value
    f.finishList()
    assert.equal(await oldPageRead, false)
    assert.equal(f.model.result.value, newPageResult)
    assert.equal(f.model.page.value, 2)
  } finally {
    f.stop()
  }
})

test('page source switch clears stale candidate; route paging/filters and unmount abort writes and ignore late results', async () => {
  for (const reason of ['page', 'q', 'provider', 'leave', 'unmount'] as const) {
    const f = await pageFixture()
    try {
      const item = f.model.result.value!.items[0]!
      const draft = f.model.editor(item)
      draft.file = file()
      draft.candidate = 'blob:first'
      f.model.switchSource(item, 'url')
      assert.equal(draft.file, null)
      assert.equal(draft.candidate, '')
      assert.equal(draft.source, 'url')
      f.model.switchSource(item, 'file')
      draft.file = file()
      draft.candidate = 'blob:second'
      f.defer()
      const saving = f.model.mutate(item)
      if (reason === 'page' || reason === 'q' || reason === 'provider') {
        f.query.value =
          reason === 'page'
            ? { page: '2' }
            : reason === 'q'
              ? { q: 'Lille' }
              : { provider: 'ugc' }
        await nextTick()
        await nextTick()
        assert.equal(f.model.page.value, reason === 'page' ? 2 : 1)
      } else if (reason === 'leave') f.leave()
      else f.stop()
      assert.equal(f.signal()?.aborted, true)
      f.finish()
      await saving
      assert.equal(f.model.successMessage.value, '')
      assert.equal(item.image_revision, 0)
      assert.deepEqual(f.revoked, ['blob:first', 'blob:second'])
    } finally {
      f.stop()
    }
  }
})

test('cinema route filters normalize bounded Unicode, reject repeated/unsupported values and preserve unrelated query keys', () => {
  assert.equal(
    cinemaFilters.normalizeCinemaSearch('  Lille\n\u0000\u0085\ud800  '),
    'Lille',
  )
  assert.equal(
    Array.from(cinemaFilters.normalizeCinemaSearch('🎬'.repeat(1025))).length,
    1024,
  )
  const state = cinemaFilters.parseCinemaRoute({
    page: '0002',
    q: '  Lille  ',
    provider: 'ugc',
  })
  assert.deepEqual(state, { page: 2, q: 'Lille', provider: 'ugc' })
  assert.deepEqual(cinemaFilters.cinemaApiQuery(state), {
    limit: 20,
    offset: 20,
    q: 'Lille',
    provider: 'ugc',
  })
  assert.deepEqual(
    cinemaFilters.cinemaRouteQuery(state, { keep: ['one', 'two'], q: 'old' }),
    {
      keep: ['one', 'two'],
      page: '2',
      q: 'Lille',
      provider: 'ugc',
    },
  )
  for (const page of [
    '0',
    '-1',
    '1.5',
    '107374184',
    String(Number.MAX_SAFE_INTEGER),
  ]) {
    assert.equal(cinemaFilters.parseCinemaRoute({ page }).page, 1)
  }
  assert.equal(
    cinemaFilters.parseCinemaRoute({ page: '107374183' }).page,
    107374183,
  )
  assert.deepEqual(
    cinemaFilters.parseCinemaRoute({
      page: ['1', '2'],
      q: ['Lille', 'Paris'],
      provider: ['ugc', 'cgr'],
    }),
    { page: 1, q: '', provider: '' },
  )
  assert.equal(
    cinemaFilters.parseCinemaRoute({ provider: 'unknown' }).provider,
    '',
  )
  assert.equal(
    cinemaFilters.parseCinemaRoute({ provider: 'toString' }).provider,
    '',
  )
  for (const provider of Object.keys(cinemaFilters.cinemaProviderLabels)) {
    assert.equal(cinemaFilters.parseCinemaProvider(provider), provider)
  }
  assert.deepEqual(
    cinemaFilters.cinemaApiQuery({ page: 1, q: '', provider: '' }),
    {
      limit: 20,
      offset: 0,
    },
  )
})

test('page transmits combined route filters, debounces 350ms, resets pagination and applies provider with pending search', async () => {
  const f = await pageFixture({
    page: '2',
    q: 'Paris',
    provider: 'ugc',
    keep: 'yes',
  })
  try {
    assert.deepEqual(f.listCalls[0]!.query, {
      limit: 20,
      offset: 20,
      q: 'Paris',
      provider: 'ugc',
    })
    assert.equal(f.model.search.value, 'Paris')
    assert.equal(f.model.provider.value, 'ugc')
    f.model.search.value = 'Li'
    f.model.changeSearch()
    await f.advance(200)
    f.model.search.value = '  Lille  '
    f.model.changeSearch()
    await f.advance(349)
    assert.equal(f.reads(), 1)
    assert.equal(f.model.pending.value, true)
    await f.advance(1)
    assert.equal(f.reads(), 2)
    assert.deepEqual(
      { ...f.query.value },
      { keep: 'yes', q: 'Lille', provider: 'ugc' },
    )
    assert.equal(f.model.page.value, 1)
    assert.deepEqual(f.listCalls.at(-1)!.query, {
      limit: 20,
      offset: 0,
      q: 'Lille',
      provider: 'ugc',
    })
    f.model.search.value = ' Lyon '
    f.model.changeSearch()
    f.selectProvider('pathe')
    await settle()
    assert.equal(f.timerCount(), 0)
    assert.deepEqual(f.listCalls.at(-1)!.query, {
      limit: 20,
      offset: 0,
      q: 'Lyon',
      provider: 'pathe',
    })
    const reads = f.reads()
    await f.advance(350)
    assert.equal(f.reads(), reads)
    f.model.resetFilters()
    await settle()
    assert.deepEqual({ ...f.query.value }, { keep: 'yes' })
    assert.equal(f.model.search.value, '')
    assert.equal(f.model.provider.value, '')
    assert.deepEqual(f.listCalls.at(-1)!.query, { limit: 20, offset: 0 })
  } finally {
    f.stop()
  }
})

test('page preserves filters in paging history, restores route changes and cancels pending debounce on back/forward', async () => {
  const f = await pageFixture({ q: 'Lille', provider: 'ugc', keep: 'yes' })
  try {
    const first = { ...f.query.value }
    f.model.changePage(2)
    await settle()
    const second = { ...f.query.value }
    assert.equal(f.navigations.at(-1)!.method, 'push')
    assert.deepEqual(second, {
      keep: 'yes',
      page: '2',
      q: 'Lille',
      provider: 'ugc',
    })
    f.model.search.value = 'draft'
    f.model.changeSearch()
    f.query.value = first // Browser back restores URL-owned state.
    await settle()
    assert.equal(f.timerCount(), 0)
    assert.equal(f.model.search.value, 'Lille')
    assert.equal(f.model.page.value, 1)
    f.query.value = second // Browser forward.
    await settle()
    assert.equal(f.model.page.value, 2)
    assert.deepEqual(f.listCalls.at(-1)!.query, {
      limit: 20,
      offset: 20,
      q: 'Lille',
      provider: 'ugc',
    })
    const result = f.model.result.value
    const reads = f.reads()
    f.query.value = { ...second, keep: 'changed' }
    await settle()
    assert.equal(f.reads(), reads)
    assert.equal(f.model.result.value, result)
    await f.advance(350)
    assert.equal(f.model.search.value, 'Lille')
  } finally {
    f.stop()
  }
})

test('page canonicalizes malformed filters and clamps out-of-range pages against filtered total', async () => {
  const f = await pageFixture({
    page: '0009',
    q: '  Lille  ',
    provider: 'ugc',
    keep: 'yes',
  })
  try {
    await settle()
    assert.deepEqual(
      { ...f.query.value },
      { keep: 'yes', page: '3', q: 'Lille', provider: 'ugc' },
    )
    assert.equal(f.model.page.value, 3)
    assert.deepEqual(f.listCalls.at(-1)!.query, {
      limit: 20,
      offset: 40,
      q: 'Lille',
      provider: 'ugc',
    })
    f.query.value = {
      page: '107374184',
      provider: 'bad',
      q: ['one', 'two'],
      keep: 'yes',
    }
    await settle()
    assert.deepEqual({ ...f.query.value }, { keep: 'yes' })
    assert.deepEqual(f.listCalls.at(-1)!.query, { limit: 20, offset: 0 })
  } finally {
    f.stop()
  }
})

test('same-page filters and search draft abort stale inventory and clean stored previews before late responses', async () => {
  for (const reason of ['q', 'provider', 'draft'] as const) {
    const f = await pageFixture()
    try {
      f.image()
      await f.model.loadInventory(true)
      await settle()
      const item = f.model.result.value!.items[0]!
      const state = f.model.editor(item)
      state.file = file()
      state.candidate = 'blob:candidate'
      state.url = 'https://photos.example.test/cinema.png'
      state.confirmation = true
      f.deferList()
      const staleRead = f.model.loadInventory(true)
      const signal = f.listCalls.at(-1)!.signal
      if (reason === 'draft') {
        f.model.search.value = 'Lille'
        f.model.changeSearch()
      } else
        f.query.value = reason === 'q' ? { q: 'Lille' } : { provider: 'ugc' }
      await settle()
      assert.equal(signal.aborted, true)
      assert.equal(state.file, null)
      assert.equal(state.url, '')
      assert.equal(state.preview, '')
      assert.deepEqual(f.revoked, ['blob:candidate', 'blob:stored-1'])
      const current = f.model.result.value
      f.finishList()
      assert.equal(await staleRead, false)
      assert.equal(f.model.result.value, current)
      if (reason === 'draft') await f.advance(350)
      assert.equal(f.model.page.value, 1)
      assert.equal(
        f.model.editor(f.model.result.value!.items[0]!).confirmation,
        false,
      )
    } finally {
      f.stop()
    }
  }
})

test('filtered empty state preserves filters and exposes reset; errors retry same combined query; leave cancels debounce', async () => {
  const f = await pageFixture({ q: 'missing', provider: 'cgr', keep: 'yes' })
  try {
    f.empty()
    await f.model.loadInventory(true)
    assert.equal(f.model.result.value!.items.length, 0)
    assert.equal(f.model.result.value!.total, 0)
    assert.equal(f.model.hasFilters.value, true)
    assert.equal(f.model.pending.value, false)
    f.listFail()
    await f.model.loadInventory(true)
    assert.ok(f.model.loadError.value)
    assert.deepEqual(f.listCalls.at(-1)!.query, {
      limit: 20,
      offset: 0,
      q: 'missing',
      provider: 'cgr',
    })
    f.model.search.value = 'draft'
    f.model.changeSearch()
    const reads = f.reads()
    f.leave()
    await f.advance(350)
    assert.equal(f.timerCount(), 0)
    assert.equal(f.reads(), reads)
  } finally {
    f.stop()
  }
  const source = await readFile(
    new URL('../app/pages/admin/cinemas.vue', import.meta.url),
    'utf8',
  )
  assert.match(source, /for="cinema-search"/)
  assert.match(source, /for="cinema-provider"/)
  assert.match(source, /maxlength="1024"/)
  assert.match(source, /Aucun cinéma ne correspond aux filtres/)
  assert.match(source, /Aucun cinéma disponible/)
  assert.match(source, /@click="resetFilters"/)
})

test('page/dashboard expose auth, uncropped previews, progress, confirmation, mobile targets and no hotlink or persistence', async () => {
  const page = await readFile(
    new URL('../app/pages/admin/cinemas.vue', import.meta.url),
    'utf8',
  )
  const dashboard = await readFile(
    new URL('../app/pages/admin/index.vue', import.meta.url),
    'utf8',
  )
  assert.match(dashboard, /to="\/admin\/cinemas"/)
  assert.match(page, /object-contain/)
  assert.match(page, /<progress/)
  assert.match(page, /Supprimer l’image enregistrée/)
  assert.match(page, /@keydown\.esc/)
  assert.match(page, /role="alert"/)
  assert.match(page, /min-h-11/)
  assert.doesNotMatch(
    page,
    /v-html|localStorage|sessionStorage|:src="[^"]*\.url"/,
  )
})
