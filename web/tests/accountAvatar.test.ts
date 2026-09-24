import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext, type Context } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import {
  computed,
  effectScope,
  ref,
  shallowRef,
  watch,
  toRef,
  nextTick,
  reactive,
  type Ref,
  type EffectScope,
} from 'vue'
import * as state from '../app/utils/accountState.ts'
import type * as avatar from '../app/utils/accountAvatar.ts'
import type { useAccountAvatarPreview } from '../app/composables/useAccountAvatarPreview.ts'
import type {
  AccountSession,
  AccountAvatarResult,
} from '../app/types/account.ts'
import { lifetimeFixture } from './helpers/accountLifetime.ts'

async function compile<T>(
  path: string,
  globals: Context,
  suffix = '',
  scope?: EffectScope,
): Promise<T> {
  const raw = await readFile(new URL(path, import.meta.url), 'utf8')
  const source = raw.includes('<script')
    ? raw.split('<script setup lang="ts">')[1]!.split('</script>')[0]!
    : raw
  const exports = {}
  const run = () =>
    runInNewContext(
      ts.transpileModule(source + suffix, {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      }).outputText,
      {
        exports,
        require: () => state,
        Blob,
        File,
        FormData,
        URL,
        AbortController,
        Uint8Array,
        setTimeout,
        clearTimeout,
        ...globals,
      },
    )
  if (scope) scope.run(run)
  else run()
  // SAFETY: each call names the exports declared by its compiled fixture source.
  return exports as T
}

const utility = await compile<typeof avatar>(
  '../app/utils/accountAvatar.ts',
  {},
)
const png = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 0])
const webp = new Uint8Array(
  Buffer.from(
    'UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA',
    'base64',
  ),
)

test('avatar route accepts only canonical positive int64 own routes', () => {
  for (const revision of ['1', '9223372036854775807'])
    assert.ok(utility.validAvatarURL(`/api/v1/account/avatar/${revision}`))
  for (const value of [
    null,
    '',
    '/api/v1/account/avatar/0',
    '/api/v1/account/avatar/01',
    '/api/v1/account/avatar/-1',
    '/api/v1/account/avatar/9223372036854775808',
    '/api/v1/account/avatar/1?x=1',
    '/api/v1/account/avatar/1#x',
    '/api/v1/account/avatar/1/',
    'https://messeances.fr/api/v1/account/avatar/1',
    '//evil.test/api/v1/account/avatar/1',
    '/api/v1/account/avatar/%31',
  ])
    assert.equal(utility.validAvatarURL(value), false)
})

test('client file checks use only metadata, never decode originals', () => {
  for (const [name, type] of [
    ['a.png', 'image/png'],
    ['a.JPEG', 'image/jpeg'],
    ['a.webp', 'image/webp'],
    ['a.jpg', ''],
  ])
    assert.equal(utility.avatarFileError(new File(['x'], name!, { type })), '')
  for (const file of [
    new File(['x'], 'a.svg', { type: 'image/svg+xml' }),
    new File(['x'], 'a.png', { type: 'text/html' }),
    new File([], 'a.png'),
    new File([new Uint8Array(5242881)], 'a.png'),
  ])
    assert.ok(utility.avatarFileError(file))
  assert.equal(
    utility.avatarFileError(new File([new Uint8Array(5242880)], 'a.png')),
    '',
  )
})

test('private blob fetch is no-store, same-origin, no redirect/retry, bounded and WebP only', async () => {
  let calls = 0
  let response = () =>
    new Response(webp, { headers: { 'Content-Type': 'image/webp' } })
  const transport = await compile<typeof avatar>(
    '../app/utils/accountAvatar.ts',
    {
      fetch: async (url: string, options: RequestInit) => {
        calls++
        assert.equal(url, '/api/v1/account/avatar/1')
        assert.equal(options.credentials, 'same-origin')
        assert.equal(options.cache, 'no-store')
        assert.equal(options.redirect, 'error')
        assert.ok(options.signal)
        return response()
      },
    },
  )
  const signal = new AbortController().signal
  assert.equal(
    (await transport.fetchAccountAvatar('/api/v1/account/avatar/1', signal))
      .type,
    'image/webp',
  )
  await assert.rejects(
    transport.fetchAccountAvatar('https://evil.test/avatar', signal),
  )
  assert.equal(calls, 1)
  for (const make of [
    () => new Response(png, { headers: { 'Content-Type': 'image/png' } }),
    () => new Response(webp, { headers: { 'Content-Type': 'image/jpeg' } }),
    () =>
      new Response(webp, {
        headers: { 'Content-Type': 'image/webp; charset=utf-8' },
      }),
    () => new Response(png, { headers: { 'Content-Type': 'image/webp' } }),
    () =>
      new Response('invalid', { headers: { 'Content-Type': 'image/webp' } }),
    () =>
      new Response(new Uint8Array(524289), {
        headers: { 'Content-Type': 'image/webp' },
      }),
    () =>
      new Response(webp, {
        headers: { 'Content-Type': 'image/webp', 'Content-Length': '524289' },
      }),
    () =>
      Response.json(
        { error: { code: 'avatar_not_found', message: 'private secret' } },
        { status: 404 },
      ),
    () => new Response('secret'.repeat(1000), { status: 503 }),
  ]) {
    response = make
    const before = calls
    await assert.rejects(
      transport.fetchAccountAvatar('/api/v1/account/avatar/1', signal),
      (cause: unknown) => {
        assert.ok(cause instanceof state.AccountApiError)
        assert.doesNotMatch(JSON.stringify(cause), /secret|request|response/)
        return true
      },
    )
    assert.equal(calls, before + 1)
  }
})

test('WebP transport rejects short, mislabeled, truncated and trailing RIFF bodies', async () => {
  const wrongRIFF = webp.slice()
  wrongRIFF[0] = 0
  const wrongWEBP = webp.slice()
  wrongWEBP[8] = 0
  const wrongSize = webp.slice()
  new DataView(wrongSize.buffer).setUint32(4, 0xffffffff, true)
  for (const bytes of [
    webp.slice(0, 11),
    webp.slice(0, 19),
    webp.slice(0, -1),
    new Uint8Array([...webp, 0]),
    wrongRIFF,
    wrongWEBP,
    wrongSize,
  ]) {
    const transport = await compile<typeof avatar>(
      '../app/utils/accountAvatar.ts',
      {
        fetch: async () =>
          new Response(bytes, { headers: { 'Content-Type': 'image/webp' } }),
      },
    )
    await assert.rejects(
      transport.fetchAccountAvatar(
        '/api/v1/account/avatar/1',
        new AbortController().signal,
      ),
      state.AccountApiError,
    )
  }
})

test('late valid WebP response cannot escape cancellation', async () => {
  const controller = new AbortController()
  const transport = await compile<typeof avatar>(
    '../app/utils/accountAvatar.ts',
    {
      fetch: async () => {
        controller.abort()
        return new Response(webp, { headers: { 'Content-Type': 'image/webp' } })
      },
    },
  )
  await assert.rejects(
    transport.fetchAccountAvatar('/api/v1/account/avatar/1', controller.signal),
    state.AccountApiError,
  )
})

test('upload uses one avatar part, browser multipart boundary, CSRF, progress, timeout and abort without retries', async () => {
  let xhr!: FakeXHR
  interface UploadEvents {
    onprogress?: (event: {
      lengthComputable: boolean
      loaded: number
      total: number
    }) => void
    onload?: () => void
  }
  class FakeXHR {
    upload: UploadEvents = {}
    headers: Record<string, string> = {}
    timeout = 0
    status = 200
    responseURL = 'https://messeances.fr/api/v1/account/avatar'
    responseText = '{"avatar_url":"/api/v1/account/avatar/1"}'
    onload?: () => void
    onabort?: () => void
    constructor() {
      // oxlint-disable-next-line typescript/no-this-alias -- The test drives the transport-created XHR instance, never a production global.
      xhr = this
    }
    open(method: string, path: string) {
      assert.equal(method, 'POST')
      assert.equal(path, '/api/v1/account/avatar')
    }
    setRequestHeader(key: string, value: string) {
      this.headers[key] = value
    }
    getResponseHeader() {
      return '999999'
    }
    send(body: FormData) {
      assert.deepEqual([...body.keys()], ['avatar'])
      assert.ok(body.get('avatar') instanceof File)
    }
    abort() {
      this.onabort?.()
    }
  }
  const transport = await compile<typeof avatar>(
    '../app/utils/accountAvatar.ts',
    {
      XMLHttpRequest: FakeXHR,
      window: { location: { origin: 'https://messeances.fr' } },
    },
  )
  const progress: (number | null)[] = []
  const file = new File(['x'], 'avatar.png')
  const controller = new AbortController()
  const pending = transport.uploadAccountAvatar(
    file,
    controller.signal,
    (value) => progress.push(value),
  )
  assert.equal(xhr.timeout, 15000)
  assert.equal(xhr.headers['X-Messeances-CSRF'], '1')
  assert.equal(xhr.headers['Cache-Control'], 'no-store')
  assert.equal(xhr.headers['Content-Type'], undefined)
  xhr.upload.onprogress?.({ lengthComputable: true, loaded: 5, total: 10 })
  xhr.upload.onload?.()
  xhr.onload?.()
  assert.equal((await pending).avatar_url, '/api/v1/account/avatar/1')
  assert.deepEqual(progress, [50, 100])
  const aborted = transport.uploadAccountAvatar(
    file,
    controller.signal,
    () => {},
  )
  controller.abort()
  await assert.rejects(aborted, state.AccountApiError)
  const failed = transport.uploadAccountAvatar(
    file,
    new AbortController().signal,
    () => {},
  )
  xhr.status = 409
  xhr.responseText = '{"error":{"code":"avatar_changed","message":"private"}}'
  xhr.onload?.()
  await assert.rejects(failed, (cause: unknown) => {
    assert.ok(cause instanceof state.AccountApiError)
    assert.equal(cause.code, 'avatar_changed')
    assert.equal(cause.retryAfter, 86400)
    assert.doesNotMatch(state.accountErrorMessage(cause), /utilisateur|private/)
    return true
  })
})

test('preview revokes and fences owner/version replacement, removal, logout, departure and unmount', async () => {
  const scope = effectScope()
  const lifetime = lifetimeFixture()
  const owner = (name: string): AccountSession => ({
    enabled: true,
    state: 'complete',
    account: {
      username: name,
      email: `${name}@example.test`,
      has_password: true,
      google_linked: false,
    },
  })
  const session = ref<AccountSession | null>(owner('first'))
  const path = ref<string | null>('/api/v1/account/avatar/1')
  const reads: { signal: AbortSignal; resolve: (blob: Blob) => void }[] = []
  const revoked: string[] = []
  let created = 0
  const revalidating = ref(false)
  let mount = () => {}
  const exports = await compile<{
    useAccountAvatarPreview: typeof useAccountAvatarPreview
  }>('../app/composables/useAccountAvatarPreview.ts', {
    ...lifetime,
    ref,
    computed,
    watch,
    useAccountSession: () => ({
      session,
      revalidating,
      clear: () => {
        session.value = null
      },
    }),
    onMounted: (fn: () => void) => {
      mount = fn
    },
    require: (name: string) =>
      name.includes('accountAvatar')
        ? {
            fetchAccountAvatar: (_: string, signal: AbortSignal) =>
              new Promise<Blob>((resolve) => reads.push({ signal, resolve })),
          }
        : state,
    URL: {
      createObjectURL: () => `blob:${++created}`,
      revokeObjectURL: (url: string) => revoked.push(url),
    },
  })
  const preview = scope.run(() => exports.useAccountAvatarPreview(path))!
  const finish = async (index: number) => {
    reads[index]!.resolve(new Blob([webp], { type: 'image/webp' }))
    await nextTick()
    await nextTick()
  }
  mount()
  await finish(0)
  assert.equal(preview.image.value, 'blob:1')
  session.value = owner('second') // Same revision URL, different owner.
  assert.equal(preview.image.value, '')
  assert.deepEqual(revoked, ['blob:1'])
  path.value = '/api/v1/account/avatar/2'
  assert.equal(reads[1]!.signal.aborted, true)
  await finish(1)
  assert.equal(created, 1)
  await finish(2)
  assert.equal(preview.image.value, 'blob:2')
  path.value = null
  assert.deepEqual(revoked, ['blob:1', 'blob:2'])
  path.value = '/api/v1/account/avatar/3'
  session.value = null
  await finish(3)
  assert.equal(created, 2)
  session.value = owner('third')
  await finish(4)
  preview.clear()
  revalidating.value = true
  await nextTick()
  revalidating.value = false
  await nextTick()
  await finish(5)
  assert.equal(
    preview.image.value,
    'blob:4',
    'unchanged revision reloads after ambiguous write recheck',
  )
  for (const start of lifetime.runtime.starts) start()
  assert.equal(preview.image.value, '')
  assert.deepEqual(revoked, ['blob:1', 'blob:2', 'blob:3', 'blob:4'])
  await preview.refresh()
  assert.equal(reads.length, 6, 'departure cannot start another read')
  lifetime.unmount()
  scope.stop()
})

test('avatar component serializes writes, retains known failures, rereads ambiguous writes and clears lost-session selection', async () => {
  for (const [status, code, uncertain] of [
    [422, 'avatar_invalid', false],
    [429, '', false],
    [409, 'avatar_changed', true],
    [0, '', true],
    [503, 'avatar_busy', true],
    [401, '', false],
  ] as const) {
    const scope = effectScope()
    const lifetime = lifetimeFixture()
    const events: unknown[][] = []
    let writes = 0
    let cleared = 0
    const blocked = ref(false)
    const exports = await compile<{
      model: {
        selected: { value: File | null }
        error: { value: string }
        pending: { value: boolean }
        save: () => Promise<void>
      }
    }>(
      '../app/components/AccountAvatar.vue',
      {
        ...lifetime,
        ref,
        shallowRef,
        computed,
        watch,
        toRef,
        defineProps: () => ({ url: null, blocked: false }),
        defineEmits:
          () =>
          (...args: unknown[]) =>
            events.push(args),
        require: (name: string) =>
          name.includes('accountAvatar') ? utility : state,
        useAccountSession: () => ({
          session: ref(null),
          writesBlocked: blocked,
          clear: () => {
            cleared++
          },
        }),
        useAccountAvatarPreview: () => ({ clear: () => {} }),
        useAccountApi: () => ({
          uploadAvatar: async () => {
            writes++
            throw new state.AccountApiError(status, code)
          },
        }),
      },
      '\nexports.model = { selected, error, pending, save };',
    )
    const model = exports.model
    model.selected.value = new File(['x'], 'a.png')
    blocked.value = true
    await model.save()
    assert.equal(writes, 0)
    blocked.value = false
    await model.save()
    assert.equal(writes, 1)
    assert.equal(model.pending.value, false)
    assert.equal(
      events.some((event) => event[0] === 'changed' && event[2] === true),
      uncertain,
    )
    if (!uncertain && status !== 401) assert.ok(model.selected.value)
    if (status === 401) assert.equal(cleared, 1)
    lifetime.unmount()
    assert.equal(model.selected.value, null)
    scope.stop()
  }
})

test('avatar result belongs to details only and source never becomes a public URL', async () => {
  const types = await readFile(
    new URL('../app/types/account.ts', import.meta.url),
    'utf8',
  )
  assert.doesNotMatch(
    types.split('export interface AccountView')[1]!.split('}')[0]!,
    /avatar/,
  )
  const component = await readFile(
    new URL('../app/components/AccountAvatar.vue', import.meta.url),
    'utf8',
  )
  assert.doesNotMatch(
    component,
    /FileReader|createImageBitmap|localStorage|sessionStorage|data:image/,
  )
  assert.match(component, /alt="Photo de profil"/)
  assert.match(component, /min-h-11/)
  const result: AccountAvatarResult = { avatar_url: null }
  assert.equal(result.avatar_url, null)
})

class PickerInput {
  value = ''
  files: File[] = []
  click() {}
}
interface PickerModel {
  selected: Ref<File | null>
  input: Ref<PickerInput | null>
  inputDisabled: Ref<boolean>
  disabled: Ref<boolean>
  pending: Ref<boolean>
  openPicker: () => void
  choose: (event: { target: PickerInput }) => void
  save: (remove?: boolean) => Promise<void>
}
async function pickerFixture() {
  const scope = effectScope()
  const revision = ref(0)
  const lifetime = lifetimeFixture(revision)
  const owner: AccountSession = {
    enabled: true,
    state: 'complete',
    account: {
      username: 'picker',
      email: 'picker@example.test',
      has_password: true,
      google_linked: false,
    },
  }
  const session = ref<AccountSession | null>(owner)
  const status = ref('ready')
  const revalidating = ref(false)
  const props = reactive({ url: null, blocked: false })
  const writes: boolean[] = []
  let finish = () => {}
  const transport = () =>
    new Promise<AccountAvatarResult>((resolve) => {
      finish = () => resolve({ avatar_url: '/api/v1/account/avatar/2' })
    })
  const source = await readFile(
    new URL('../app/components/AccountAvatar.vue', import.meta.url),
    'utf8',
  )
  const inputBinding = source.match(
    /<input\s[\s\S]*?id="avatar-file"[\s\S]*?:disabled="([^"]+)"/,
  )![1]
  const exports = await compile<{ model: PickerModel }>(
    '../app/components/AccountAvatar.vue',
    {
      ...lifetime,
      ref,
      shallowRef,
      computed,
      watch,
      toRef,
      HTMLInputElement: PickerInput,
      defineProps: () => props,
      defineEmits: () => () => {},
      require: (name: string) =>
        name.includes('accountAvatar') ? utility : state,
      useAccountSession: () => ({
        session,
        status,
        revalidating,
        writesBlocked: computed(
          () => revalidating.value || status.value !== 'ready',
        ),
      }),
      useAccountAvatarPreview: () => ({ clear: () => {} }),
      useAccountApi: () => ({
        uploadAvatar: () => {
          writes.push(false)
          return transport()
        },
        removeAvatar: () => {
          writes.push(true)
          return transport()
        },
      }),
    },
    `\nexports.model = { selected, input, disabled, pending, choose, save, inputDisabled: computed(() => ${inputBinding}.value), openPicker };`,
    scope,
  )
  const model = exports.model
  const input = new PickerInput()
  model.input.value = input
  const change = () => {
    input.files = [new File(['synthetic'], 'picker.png', { type: 'image/png' })]
    input.value = 'C:\\fakepath\\picker.png'
    model.choose({ target: input })
  }
  return {
    model,
    input,
    change,
    session,
    owner,
    status,
    revalidating,
    props,
    revision,
    lifetime,
    writes,
    finish: () => finish(),
    stop: () => {
      lifetime.unmount()
      scope.stop()
    },
  }
}

for (const focusFirst of [true, false]) {
  test(`picker keeps local selection with ${focusFirst ? 'focus-before-change' : 'change-before-focus'}, never writes until explicit save`, async () => {
    const f = await pickerFixture()
    try {
      f.model.openPicker()
      if (!focusFirst) f.change()
      f.revision.value++
      f.revalidating.value = true
      f.props.blocked = true
      await nextTick()
      assert.equal(
        f.model.inputDisabled.value,
        false,
        'native input must remain enabled while already-open picker returns',
      )
      if (focusFirst) f.change()
      assert.equal(f.model.selected.value?.name, 'picker.png')
      assert.equal(f.model.disabled.value, true)
      await f.model.save()
      await f.model.save(true)
      assert.deepEqual(f.writes, [])
      // Session response settled, details still pending: the selection stays local.
      f.session.value = structuredClone(f.owner)
      await nextTick()
      assert.equal(f.model.selected.value?.name, 'picker.png')
      assert.equal(f.model.disabled.value, true)
      f.revalidating.value = false
      f.props.blocked = false
      await nextTick()
      assert.deepEqual(f.writes, [], 'validation never auto-uploads')
      const saving = f.model.save()
      assert.deepEqual(f.writes, [false])
      assert.equal(
        f.model.inputDisabled.value,
        true,
        'pending upload blocks native input',
      )
      const selected = f.model.selected.value
      f.model.openPicker()
      f.change()
      assert.equal(
        f.model.selected.value,
        selected,
        'pending upload rejects replacement',
      )
      f.finish()
      await saving
      assert.equal(f.model.selected.value, null)
    } finally {
      f.stop()
    }
  })
}

test('picker change handler independently accepts an already-open selection during soft revalidation', async () => {
  const f = await pickerFixture()
  try {
    f.model.openPicker()
    f.revalidating.value = true
    f.props.blocked = true
    f.change()
    assert.equal(f.model.selected.value?.name, 'picker.png')
    assert.deepEqual(f.writes, [])
  } finally {
    f.stop()
  }
})

for (const reason of [
  'failure',
  'logout',
  'identity',
  'route',
  'unmount',
  'destructive',
] as const) {
  test(`picker rejects stale result and clears draft after ${reason}`, async () => {
    const f = await pickerFixture()
    try {
      f.model.openPicker()
      f.change()
      f.model.openPicker()
      if (reason === 'route')
        for (const start of f.lifetime.runtime.starts) start()
      else if (reason === 'unmount') f.lifetime.unmount()
      else if (reason === 'identity')
        f.session.value = {
          ...f.owner,
          account: { ...f.owner.account!, username: 'other' },
        }
      else {
        f.session.value = null
        f.status.value =
          reason === 'failure'
            ? 'error'
            : reason === 'destructive'
              ? 'loading'
              : 'idle'
      }
      assert.equal(f.model.selected.value, null)
      assert.equal(f.input.value, '')
      // Even restoration of the original owner cannot resurrect that open picker.
      f.session.value = structuredClone(f.owner)
      f.status.value = 'ready'
      f.change()
      assert.equal(f.model.selected.value, null)
      assert.deepEqual(f.writes, [])
    } finally {
      f.stop()
    }
  })
}

test('picker rejects unsolicited change and opening while writes are blocked', async () => {
  const f = await pickerFixture()
  try {
    f.change()
    assert.equal(f.model.selected.value, null)
    f.revalidating.value = true
    f.model.openPicker()
    f.change()
    assert.equal(f.model.selected.value, null)
    assert.deepEqual(f.writes, [])
  } finally {
    f.stop()
  }
})
