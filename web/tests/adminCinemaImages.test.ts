import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext, type Context } from 'node:vm'
import ts from 'typescript'
import { computed, ref, watch, effectScope, nextTick, type Ref } from 'vue'
import * as queryUtils from '../app/utils/routeQuery.ts'
import * as images from '../app/utils/adminCinemaImages.ts'
import type { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import type {
  AdminTheater,
  AdminTheatersResponse,
  AdminTheaterImageResult,
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
      query?: { limit: number; offset: number }
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
  await api.adminTheaters({ limit: 20, offset: 40 }, signal)
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

async function pageFixture() {
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
  const query = ref<Record<string, string>>({})
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
          : name.includes('routeQuery')
            ? queryUtils
            : {},
      ref,
      computed,
      watch: (...args: Parameters<typeof watch>) =>
        scope.run(() => watch(...args)),
      useRoute: () => ({
        get query() {
          return query.value
        },
      }),
      useRouter: () => ({
        replace: async ({ query: next }: { query: Record<string, string> }) => {
          query.value = next
        },
        push: async ({ query: next }: { query: Record<string, string> }) => {
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
        adminTheaters: async () => {
          reads++
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
    '\nexports.model = { result, editor, switchSource, mutate, loadInventory, applyRoute, page, pending, loadError, successMessage };',
  )
  mount()
  await nextTick()
  await nextTick()
  const model = exports.model
  return {
    model,
    query,
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

test('pending preview reads abort on page/unmount and cannot allocate late stored Blobs', async () => {
  for (const reason of ['page', 'unmount'] as const) {
    const f = await pageFixture()
    try {
      f.image()
      f.deferPreview()
      await f.model.loadInventory(true)
      const item = f.model.result.value!.items[0]!
      const state = f.model.editor(item)
      assert.equal(state.previewState, 'loading')
      const signal = f.previewSignal()
      if (reason === 'page') {
        f.query.value = { page: '2' }
        await settle()
      } else f.stop()
      assert.equal(signal?.aborted, true)
      f.finishPreview()
      await settle()
      assert.equal(state.preview, '')
      assert.equal(f.previewCreated(), reason === 'page' ? 1 : 0)
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

test('page source switch clears stale candidate; route paging and unmount abort writes and ignore late results', async () => {
  for (const reason of ['page', 'leave', 'unmount'] as const) {
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
      if (reason === 'page') {
        f.query.value = { page: '2' }
        await nextTick()
        await nextTick()
        assert.equal(f.model.page.value, 2)
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
