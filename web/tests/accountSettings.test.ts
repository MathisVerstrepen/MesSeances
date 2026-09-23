import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { type Context, runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { createFetch, FetchError } from 'ofetch'
import { type Ref, ref } from 'vue'
import {
  AccountApiError,
  accountDestination,
} from '../app/utils/accountState.ts'
import type { useAccountApi } from '../app/composables/useAccountApi.ts'
import type { useAccountPasswordAction } from '../app/composables/useAccountPasswordAction.ts'
import type { useAccountDetails } from '../app/composables/useAccountDetails.ts'
import type { AccountDetails, AccountSession } from '../app/types/account.ts'
import { isAccountPage } from '../shared/accountPrivacy.ts'

const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

interface CompiledComposables {
  default?: (to: {
    path: string
    query: Record<string, string>
  }) => Promise<string | undefined>
  useAccountApi?: typeof useAccountApi
  useAccountPasswordAction?: typeof useAccountPasswordAction
  useAccountDetails?: typeof useAccountDetails
}

test('pending email password session can restart registration without substituting for browser proof', async () => {
  for (const [state, hasPassword, destination] of [
    ['anonymous', false, undefined],
    ['pending_email', true, undefined],
    ['pending_email', false, '/verification'],
    ['pending_username', true, '/finaliser'],
    ['complete', true, '/compte'],
  ] as const) {
    const session = ref<AccountSession>({
      enabled: true,
      state,
      account:
        state === 'anonymous'
          ? null
          : {
              email: 'synthetic@example.test',
              username: null,
              has_password: hasPassword,
              google_linked: !hasPassword,
            },
    })
    const exports = await compile('../app/middleware/account-auth.ts', {
      defineNuxtRouteMiddleware: (
        handler: NonNullable<CompiledComposables['default']>,
      ) => handler,
      useAccountSession: () => ({ session, refresh: async () => {} }),
      navigateTo: (path: string) => path,
      require: () => ({ accountDestination }),
    })
    assert.ok(exports.default)
    assert.equal(
      await exports.default({ path: '/inscription', query: {} }),
      destination,
    )
  }
})

test('verification errors preserve only allowlisted browser/session recovery codes', async () => {
  for (const [status, code] of [
    [403, 'verification_browser_required'],
    [401, 'authentication_required'],
  ] as const) {
    const transport = createFetch({
      fetch: async () =>
        Response.json(
          { error: { code, message: 'never expose response body' } },
          { status },
        ),
      Headers,
      AbortController,
    })
    const exports = await compile('../app/composables/useAccountApi.ts', {
      useRuntimeConfig: () => ({ public: {} }),
      require: (name: string) =>
        name === 'ofetch'
          ? { ofetch: transport, FetchError }
          : { AccountApiError },
    })
    assert.ok(exports.useAccountApi)
    await assert.rejects(
      exports.useAccountApi().confirmVerification('synthetic-token'),
      (cause: unknown) => {
        assert.ok(cause instanceof AccountApiError)
        assert.equal(cause.status, status)
        assert.equal(cause.code, code)
        assert.doesNotMatch(cause.message, /response body/)
        return true
      },
    )
  }
})

test('private fragment-only navigation reloads once without stopping Nuxt in a redirect loop', async () => {
  type Route = { path: string; fullPath: string }
  let middleware: (to: Route, from: Route) => void = () => {}
  const calls: string[] = []
  await compile('../app/middleware/account-boundary.global.ts', {
    defineNuxtRouteMiddleware: (handler: typeof middleware) => {
      middleware = handler
      return handler
    },
    useState: () => ref(true),
    useNuxtApp: () => ({ isHydrating: false }),
    require: () => ({ isAccountPage }),
    navigateTo: () => {
      calls.push('external')
    },
    URL,
    window: {
      location: {
        origin: 'https://messeances.fr',
        pathname: '/verification',
        search: '',
        reload: () => {
          calls.push('reload')
        },
      },
      history: {
        state: null,
        replaceState: (_state: null, _title: string, path: string) => {
          calls.push(path)
        },
      },
    },
  })
  middleware(
    { path: '/verification', fullPath: '/verification#token=synthetic' },
    { path: '/verification', fullPath: '/verification' },
  )
  assert.deepEqual(calls, ['/verification#token=synthetic', 'reload'])
  calls.length = 0
  middleware(
    { path: '/connexion', fullPath: '/connexion' },
    { path: '/verification', fullPath: '/verification' },
  )
  assert.deepEqual(calls, ['external'])
})

async function compile(path: string, globals: Context) {
  const source = (await read(path))
    .replaceAll('import.meta.server', 'false')
    .replaceAll('import.meta.client', 'true')
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: CompiledComposables = {}
  runInNewContext(compiled, { exports, ...globals })
  return exports
}

test('settings API uses exact actions, same-origin privacy and empty 202/204 bodies', async () => {
  const calls: { path: string; body: Record<string, string> | undefined }[] = []
  const transport = createFetch({
    fetch: async (input, options) => {
      const path = String(input)
      assert.equal(options?.credentials, 'same-origin')
      assert.equal(options?.cache, 'no-store')
      assert.equal(options?.redirect, 'error')
      const headers = new Headers(options?.headers)
      if (options?.method === 'POST') {
        assert.equal(headers.get('X-Messeances-CSRF'), '1')
        assert.match(headers.get('Content-Type')!, /application\/json/)
      }
      calls.push({
        path,
        body: options?.body ? JSON.parse(String(options.body)) : undefined,
      })
      if (path.endsWith('/reauth/password'))
        return Response.json({ grant: 'synthetic-grant' })
      if (path === '/api/v1/account')
        return Response.json({ pending_email: 'new@example.test' })
      return new Response(null, {
        status: path.endsWith('/request') ? 202 : 204,
      })
    },
    Headers,
    AbortController,
  })
  const exports = await compile('../app/composables/useAccountApi.ts', {
    useRuntimeConfig: () => ({
      apiBase: 'http://private.invalid',
      public: { siteUrl: 'https://messeances.fr' },
    }),
    require: (name: string) =>
      name === 'ofetch'
        ? { ofetch: transport, FetchError }
        : { AccountApiError },
  })
  assert.ok(exports.useAccountApi)
  const api = exports.useAccountApi()
  await api.requestPasswordReset('owner@example.test')
  await api.confirmPasswordReset('synthetic-token', 'long synthetic password')
  await api.reauthPassword('current synthetic password', 'password_change')
  await api.changePassword('new synthetic password', 'synthetic-grant')
  await api.reauthPassword(
    'current synthetic password',
    'email_change',
    'new@example.test',
  )
  await api.requestEmailChange('new@example.test', 'synthetic-grant')
  await api.confirmEmailChange('synthetic-token', 'synthetic-grant')
  await api.cancelEmailChange()
  await api.logoutAll()
  await api.details()
  assert.deepEqual(calls, [
    {
      path: '/api/v1/auth/password/reset/request',
      body: { email: 'owner@example.test' },
    },
    {
      path: '/api/v1/auth/password/reset/confirm',
      body: { token: 'synthetic-token', password: 'long synthetic password' },
    },
    {
      path: '/api/v1/account/reauth/password',
      body: {
        password: 'current synthetic password',
        action: 'password_change',
      },
    },
    {
      path: '/api/v1/account/password',
      body: { password: 'new synthetic password', grant: 'synthetic-grant' },
    },
    {
      path: '/api/v1/account/reauth/password',
      body: {
        password: 'current synthetic password',
        action: 'email_change',
        target: 'new@example.test',
      },
    },
    {
      path: '/api/v1/account/email/request',
      body: { email: 'new@example.test', grant: 'synthetic-grant' },
    },
    {
      path: '/api/v1/account/email/confirm',
      body: { token: 'synthetic-token', grant: 'synthetic-grant' },
    },
    { path: '/api/v1/account/email/cancel', body: {} },
    { path: '/api/v1/auth/logout-all', body: {} },
    { path: '/api/v1/account', body: undefined },
  ])
})

test('failed writes never retry or retain credential-bearing request errors', async () => {
  let attempts = 0
  const transport = createFetch({
    fetch: async () => {
      attempts++
      throw new Error('sensitive request body')
    },
    Headers,
    AbortController,
  })
  const exports = await compile('../app/composables/useAccountApi.ts', {
    useRuntimeConfig: () => ({ public: {} }),
    require: (name: string) =>
      name === 'ofetch'
        ? { ofetch: transport, FetchError }
        : { AccountApiError },
  })
  assert.ok(exports.useAccountApi)
  const api = exports.useAccountApi()
  await assert.rejects(
    api.changePassword('synthetic-password', 'synthetic-grant'),
    (error: Error) => {
      assert.ok(error instanceof AccountApiError)
      assert.doesNotMatch(
        JSON.stringify(error),
        /sensitive|synthetic|request body/,
      )
      return true
    },
  )
  assert.equal(attempts, 1)
})

test('Google continuation, scoped proof, linking and deletion use the exact backend contract', async () => {
  const calls: { path: string; method: string; body: unknown }[] = []
  const transport = createFetch({
    fetch: async (input, options) => {
      const method = options?.method ?? 'GET'
      if (method !== 'GET')
        assert.equal(
          new Headers(options?.headers).get('X-Messeances-CSRF'),
          '1',
        )
      calls.push({
        path: String(input),
        method,
        body: options?.body ? JSON.parse(String(options.body)) : undefined,
      })
      return new Response(null, { status: 204 })
    },
    Headers,
    AbortController,
  })
  const exports = await compile('../app/composables/useAccountApi.ts', {
    useRuntimeConfig: () => ({ public: {} }),
    require: (name: string) =>
      name === 'ofetch'
        ? { ofetch: transport, FetchError }
        : { AccountApiError },
  })
  assert.ok(exports.useAccountApi)
  const api = exports.useAccountApi()
  await api.googleStart()
  await api.googleStart({ mode: 'link', grant: 'proof' })
  await api.googleStart({
    mode: 'reauth',
    action: 'email_change',
    target: 'next@example.test',
  })
  await api.continuation()
  await api.requestIdentityEmail()
  await api.confirmIdentityEmail('token', 'email_change', 'next@example.test')
  await api.confirmIdentityEmail('token', 'password_add')
  await api.confirmVerification('token')
  await api.googleUnlink('proof')
  await api.deleteAccount('proof', 'SUPPRIMER')
  assert.deepEqual(calls, [
    {
      path: '/api/v1/auth/google/start',
      method: 'POST',
      body: { mode: 'login' },
    },
    {
      path: '/api/v1/auth/google/start',
      method: 'POST',
      body: { mode: 'link', grant: 'proof' },
    },
    {
      path: '/api/v1/auth/google/start',
      method: 'POST',
      body: {
        mode: 'reauth',
        action: 'email_change',
        target: 'next@example.test',
      },
    },
    {
      path: '/api/v1/account/reauth/continuation',
      method: 'GET',
      body: undefined,
    },
    { path: '/api/v1/account/reauth/email/request', method: 'POST', body: {} },
    {
      path: '/api/v1/account/reauth/email/confirm',
      method: 'POST',
      body: {
        token: 'token',
        action: 'email_change',
        target: 'next@example.test',
      },
    },
    {
      path: '/api/v1/account/reauth/email/confirm',
      method: 'POST',
      body: { token: 'token', action: 'password_add' },
    },
    {
      path: '/api/v1/auth/verification/confirm',
      method: 'POST',
      body: { token: 'token' },
    },
    {
      path: '/api/v1/account/google/unlink',
      method: 'POST',
      body: { grant: 'proof' },
    },
    {
      path: '/api/v1/account',
      method: 'DELETE',
      body: { grant: 'proof', confirmation: 'SUPPRIMER' },
    },
  ])
})

test('password proof clears grant and cannot write after session invalidation or unmount', async () => {
  for (const invalidate of [false, true]) {
    let onInvalidate = () => {}
    let onUnmount = () => {}
    let calls = 0
    const proof = { grant: 'synthetic-grant' }
    const exports = await compile(
      '../app/composables/useAccountPasswordAction.ts',
      {
        useAccountApi: () => ({
          reauthPassword: async (
            password: string,
            action: string,
            target: string,
          ) => {
            assert.equal(password, 'current-password')
            assert.equal(action, 'email_change')
            assert.equal(target, 'new@example.test')
            if (invalidate) {
              onInvalidate()
              onUnmount()
            }
            return proof
          },
        }),
        useAccountSession: () => ({
          session: ref<AccountSession | null>(null),
        }),
        watch: (_: Ref<AccountSession | null>, callback: () => void) => {
          onInvalidate = callback
        },
        onMounted: () => {},
        onBeforeUnmount: (callback: () => void) => {
          onUnmount = callback
        },
        window: { removeEventListener: () => {} },
      },
    )
    assert.ok(exports.useAccountPasswordAction)
    const run = exports.useAccountPasswordAction()
    const applied = await run(
      'current-password',
      'email_change',
      'new@example.test',
      async (grant) => {
        assert.equal(grant, 'synthetic-grant')
        calls++
      },
    )
    assert.equal(applied, !invalidate)
    assert.equal(calls, invalidate ? 0 : 1)
    assert.equal(proof.grant, '')
  }
})

test('recovery and email confirmation remain explicit, memory-only and scanner-safe', async () => {
  for (const path of [
    '../app/pages/reinitialiser-mot-de-passe.vue',
    '../app/pages/compte/confirmer-email.vue',
  ]) {
    const source = await read(path)
    assert.match(source, /useAccountToken\(\)/)
    assert.match(source, /@submit\.prevent="confirm"/)
    assert.match(source, /useAccountSecrets\(password\)/)
    assert.doesNotMatch(
      source,
      /onMounted\(confirm\)|localStorage|sessionStorage|route\.(query|hash)/,
    )
    assert.match(source, /accountWriteUncertain/)
  }
  const settings = await read('../app/pages/compte/index.vue')
  assert.match(settings, /<AccountShell title="Mon compte" hide-explore>/)
  const shell = await read('../app/components/AccountShell.vue')
  assert.match(shell, /hideExplore\?: boolean/)
  assert.match(
    shell,
    /v-if="!hideExplore && route\.path\.toLowerCase\(\)\.startsWith\('\/compte'\)"/,
  )
  assert.match(settings, /Email du compte/)
  assert.match(settings, /Email communiqué par Google/)
  assert.match(settings, /allowed_methods\.includes\('password'\)/)
  assert.match(settings, /googleUnlink/)
  assert.match(settings, /deleteAccount/)
  assert.match(settings, /password_add/)
})

test('settings never retain late private details after session invalidation', async () => {
  const session = ref<AccountSession | null>({
    enabled: true,
    state: 'complete',
    account: null,
  })
  let mounted = () => {}
  let changed = () => {}
  let resolveDetails: (value: AccountDetails) => void = () => {}
  const response = new Promise<AccountDetails>((resolve) => {
    resolveDetails = resolve
  })
  const exports = await compile('../app/composables/useAccountDetails.ts', {
    require: () => ({ accountErrorMessage: () => 'Safe error' }),
    useAccountApi: () => ({ details: () => response }),
    useAccountSession: () => ({ session }),
    ref,
    watch: (_: Ref<AccountSession | null>, callback: () => void) => {
      changed = callback
    },
    onMounted: (callback: () => void) => {
      mounted = callback
    },
    onBeforeUnmount: () => {},
  })
  assert.ok(exports.useAccountDetails)
  const details = exports.useAccountDetails()
  mounted()
  assert.equal(details.loading.value, true)
  session.value = null
  changed()
  resolveDetails({
    email: 'private@example.test',
    username: 'owner',
    has_password: true,
    google_linked: false,
    google_email: null,
    pending_email: null,
    allowed_methods: ['password'],
  })
  await response
  await Promise.resolve()
  assert.equal(details.details.value, null)
  assert.equal(details.loading.value, false)
})
