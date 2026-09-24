import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { type Context, runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { createFetch, FetchError } from 'ofetch'
import { type Ref, computed, ref } from 'vue'
import {
  AccountApiError,
  accountDestination,
  accountErrorMessage,
} from '../app/utils/accountState.ts'
import * as accountState from '../app/utils/accountState.ts'
import type { useAccountApi } from '../app/composables/useAccountApi.ts'
import type { useAccountPasswordAction } from '../app/composables/useAccountPasswordAction.ts'
import type { useAccountDetails } from '../app/composables/useAccountDetails.ts'
import type { AccountDetails, AccountSession } from '../app/types/account.ts'
import { isAccountPage } from '../shared/accountPrivacy.ts'
import { createAccountNavigation } from '../app/utils/accountNavigation.ts'
import { lifetimeFixture } from './helpers/accountLifetime.ts'

const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

test('registration sent actions keep a wrapping gap and their public link styles', async () => {
  const source = await read('../app/components/AccountCredentialsForm.vue')
  assert.match(
    source,
    /<div v-if="sent" class="space-y-5">\s*<p\b[^>]*>[\s\S]*?<\/p>\s*<div class="flex flex-wrap items-center gap-4">\s*<NuxtLink\s+to="\/verification"\s+:prefetch="false"\s+class="account-primary"\s*>\s*Vérifier mon adresse\s*<\/NuxtLink\s*>\s*<NuxtLink\s+to="\/connexion"\s+:prefetch="false"\s+class="account-link"\s*>\s*Se connecter\s*<\/NuxtLink\s*>\s*<\/div>\s*<\/div>/,
  )
})

test('compact shell is opt-in for credentials pages, not account settings', async () => {
  const shell = await read('../app/components/AccountShell.vue')
  assert.match(shell, /compact\?: boolean/)
  assert.match(shell, /'account-shell-compact': compact/)
  assert.match(shell, /max-w-\[33rem\] sm:p-8/)
  for (const page of ['connexion', 'inscription']) {
    assert.match(
      await read(`../app/pages/${page}.vue`),
      /<AccountShell[^>]+ compact>/,
    )
  }
  const settings = await read('../app/pages/compte/index.vue')
  assert.match(settings, /account-area/)
  assert.doesNotMatch(settings, /<AccountShell[^>]+\bcompact\b/)
})

test('credentials keep recovery beside login password and Google as a separate alternative', async () => {
  const source = await read('../app/components/AccountCredentialsForm.vue')
  assert.match(
    source,
    /<template v-if="!register" #label-action>[\s\S]*?to="\/mot-de-passe-oublie"/,
  )
  assert.doesNotMatch(
    await read('../app/pages/connexion.vue'),
    /mot-de-passe-oublie/,
  )
  assert.match(
    source,
    /<\/form>\s*<div[^>]+>[\s\S]*?<span>ou<\/span>[\s\S]*?<\/div>\s*<button[\s\S]*?Continuer avec Google/,
  )
  assert.match(
    source,
    /:to="register \? '\/connexion' : '\/inscription'"\s+:prefetch="false"\s+class="account-navigation-link"/,
  )
})

test('shared Google login button keeps its label and a local decorative brand icon', async () => {
  const source = await read('../app/components/AccountCredentialsForm.vue')
  const button = source.match(
    /<button\b[^>]*@click="google"[^>]*>([\s\S]*?)<\/button>/,
  )?.[0]
  assert.ok(button)
  assert.match(button, /type="button"/)
  assert.match(button, /:disabled="blocked"/)
  assert.match(button, /class="account-secondary w-full"/)
  assert.match(button, /<GoogleIcon \/>\s*Continuer avec Google\s*<\/button>/)
  const icon = (await read('../app/components/GoogleIcon.vue')).match(
    /<svg\b[\s\S]*?<\/svg>/,
  )?.[0]
  assert.ok(icon)
  assert.match(icon, /viewBox="10 10 20 20"/)
  assert.match(icon, /class="size-5 shrink-0"/)
  assert.match(icon, /aria-hidden="true"/)
  assert.match(icon, /focusable="false"/)
  assert.deepEqual(
    [...icon.matchAll(/fill="(#[A-F0-9]+)"/g)].map((match) => match[1]),
    ['#4285F4', '#34A853', '#FBBC04', '#E94235'],
  )
  assert.doesNotMatch(icon, /<title|<image|<use|(?:href|src)=/)
})

test('password recovery row aligns text baselines while retaining wrapping and its touch target', async () => {
  const field = await read('../app/components/AccountPasswordField.vue')
  const shell = await read('../app/components/AccountShell.vue')
  assert.match(
    field,
    /class="account-password-label-row[^"\n]*flex-wrap items-baseline/,
  )
  assert.match(
    shell,
    /\.account-password-label-row \.account-label\) \{\s*@apply mb-0;/,
  )
  assert.match(
    shell,
    /\.account-navigation-link\) \{\s*@apply inline-flex min-h-11 items-center/,
  )
})

test('shared inset password toggle preserves masking, labels, criteria and autocomplete', async () => {
  const source = await read('../app/components/AccountPasswordField.vue')
  assert.match(source, /import \{ Eye, EyeOff \} from '@lucide\/vue'/)
  assert.match(source, /const visible = ref\(false\)/)
  assert.match(source, /:type="visible \? 'text' : 'password'"/)
  assert.match(
    source,
    /:autocomplete="newPassword \? 'new-password' : 'current-password'"/,
  )
  assert.match(source, /:aria-controls="id"/)
  assert.match(source, /:aria-pressed="visible"/)
  assert.match(
    source,
    /:aria-label="`\$\{visible \? 'Masquer' : 'Afficher'\} le mot de passe`"/,
  )
  assert.match(source, /type="button"\s+class="absolute[^"\n]*size-11/)
  assert.match(source, /<EyeOff[^>]+aria-hidden="true"/)
  assert.match(source, /<Eye v-else[^>]+aria-hidden="true"/)
  assert.match(source, /account-password-label-row[^"\n]*flex-wrap/)
  assert.match(
    source,
    /:aria-describedby="newPassword \? `\$\{id\}-criteria` : undefined"/,
  )
  assert.match(
    await read('../app/components/AccountShell.vue'),
    /\.account-password-input\) \{\s*@apply pr-14;/,
  )
})

interface CompiledComposables {
  default?: (to: {
    path: string
    query: Record<string, string>
  }) => Promise<string | undefined>
  useAccountApi?: typeof useAccountApi
  useAccountPasswordAction?: typeof useAccountPasswordAction
  useAccountDetails?: typeof useAccountDetails
}

interface CompiledPasswordForm {
  model?: {
    password: Ref<string>
    errorMessage: Ref<string>
    busy: Ref<boolean>
    completed: Ref<boolean>
    run: () => Promise<void>
  }
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
      useNuxtApp: () => ({ isHydrating: false }),
      useAccountSession: () => ({ session, revalidate: async () => {} }),
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

test('password errors propagate only safe codes across creation, login and reauthentication APIs', async () => {
  for (const [status, code, expectedCode] of [
    [400, 'common_password', 'common_password'],
    [400, 'invalid_input', ''],
    [400, 'invalid_link', 'invalid_link'],
    [400, 'synthetic-secret-code', ''],
    [401, 'invalid_credentials', ''],
  ] as const) {
    let attempts = 0
    const transport = createFetch({
      fetch: async () => {
        attempts++
        return Response.json(
          { error: { code, message: 'synthetic-secret-response' } },
          { status },
        )
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
    const actions = [
      () => api.register('synthetic@example.test', 'synthetic-password'),
      () => api.confirmPasswordReset('synthetic-token', 'synthetic-password'),
      () => api.changePassword('synthetic-password', 'synthetic-grant'),
      () => api.login('synthetic@example.test', 'synthetic-password'),
      () => api.reauthPassword('synthetic-password', 'password_change'),
    ]
    for (const action of actions) {
      await assert.rejects(action, (cause: unknown) => {
        assert.ok(cause instanceof AccountApiError)
        assert.equal(cause.status, status)
        assert.equal(cause.code, expectedCode)
        assert.equal(cause.message, 'Account request failed')
        assert.deepEqual(Object.keys(cause).sort(), [
          'code',
          'retryAfter',
          'status',
        ])
        for (const field of ['cause', 'request', 'response', 'options', 'data'])
          assert.equal(field in cause, false)
        assert.doesNotMatch(JSON.stringify(cause), /synthetic/)
        assert.doesNotMatch(accountErrorMessage(cause), /synthetic/)
        return true
      })
    }
    assert.equal(attempts, actions.length, 'failed writes are not retried')
  }
})

test('registration and reset show the shared common-password error without completing or invalidating the form', async () => {
  for (const [path, action] of [
    ['../app/components/AccountCredentialsForm.vue', 'submit'],
    ['../app/pages/reinitialiser-mot-de-passe.vue', 'confirm'],
  ] as const) {
    const source = await read(path)
    const script = source.match(
      /<script setup lang="ts">([\s\S]*?)<\/script>/,
    )?.[1]
    assert.ok(script)
    let attempts = 0
    const reject = async () => {
      attempts++
      throw new AccountApiError(400, 'common_password')
    }
    const exports: CompiledPasswordForm = {}
    runInNewContext(
      ts.transpileModule(
        `${script.replaceAll('import.meta.client', 'true')}\nexports.model = { password, errorMessage, busy, completed: ${action === 'submit' ? 'sent' : 'done'}, run: ${action} };`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        ...lifetimeFixture(),
        ref,
        computed,
        require: () => accountState,
        defineProps: () => ({ register: true }),
        definePageMeta: () => {},
        useHead: () => {},
        onMounted: () => {},
        onBeforeUnmount: () => {},
        useAccountApi: () => ({
          register: reject,
          confirmPasswordReset: reject,
        }),
        useAccountSession: () => ({ writesBlocked: ref(false) }),
        useAccountGoogle: () => {},
        useAccountFlowDraft: () => () =>
          assert.fail('validation must not complete reset'),
        useAccountToken: () => ({
          token: ref('synthetic-token'),
          ready: ref(true),
          clear: () => assert.fail('validation must not discard reset token'),
        }),
      },
    )
    const model = exports.model!
    model.password.value = 'synthetic-password'
    await model.run()
    assert.equal(attempts, 1)
    assert.equal(
      model.errorMessage.value,
      'Ce mot de passe est trop courant. Choisissez un mot de passe plus difficile à deviner.',
    )
    assert.equal(model.busy.value, false)
    assert.equal(model.completed.value, false)
    assert.match(source, /role="alert"[\s\S]*?\{\{ errorMessage \}\}/)
  }
})

test('private fragment-only navigation replaces once without reloading', async () => {
  type Route = { path: string; fullPath: string; hash: string; query: object }
  let middleware: (to: Route, from: Route) => Promise<boolean> | void = () => {}
  const calls: string[] = []
  const runtime = createAccountNavigation()
  await compile('../app/middleware/account-boundary.global.ts', {
    defineNuxtRouteMiddleware: (handler: typeof middleware) => {
      middleware = handler
      return handler
    },
    useState: () => ref(true),
    useNuxtApp: () => ({ isHydrating: false }),
    useAccountNavigation: () => runtime,
    useRouter: () => ({
      resolve: ({ path }: { path: string }) => ({ fullPath: path }),
      replace: async (to: {
        path: string
        hash: string
        replace: boolean
        force: boolean
      }) => {
        assert.equal(to.hash, '')
        assert.equal(to.replace, true)
        assert.equal(to.force, true)
        calls.push(to.path)
      },
    }),
    document: { querySelector: () => null },
    require: () => ({ isAccountPage }),
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
  await middleware(
    {
      path: '/verification',
      fullPath: '/verification#token=synthetic',
      hash: '#token=synthetic',
      query: {},
    },
    { path: '/verification', fullPath: '/verification', hash: '', query: {} },
  )
  assert.deepEqual(calls, ['/verification'])
  calls.length = 0
  middleware(
    { path: '/connexion', fullPath: '/connexion', hash: '', query: {} },
    { path: '/verification', fullPath: '/verification', hash: '', query: {} },
  )
  assert.deepEqual(calls, [])
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
  runInNewContext(compiled, {
    exports,
    ...lifetimeFixture(),
    useState: <T>(_: string, init: () => T) => ref(init()),
    ...globals,
  })
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
          writesBlocked: ref(false),
          revision: ref(0),
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
    assert.match(
      source,
      /useAccount(?:Secrets\(password|FlowDraft\(password, token)\)/,
    )
    assert.doesNotMatch(
      source,
      /onMounted\(confirm\)|localStorage|sessionStorage|route\.(query|hash)/,
    )
    assert.match(source, /accountWriteUncertain/)
  }
  const settings = await read('../app/pages/compte/index.vue')
  assert.match(settings, /<AccountShell\s+title="Paramètres"\s+hide-explore\b/)
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
    useAccountSession: () => ({
      session,
      revision: ref(0),
      onRevalidate: () => () => {},
    }),
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
    avatar_url: null,
    pending_email: null,
    allowed_methods: ['password'],
  })
  await response
  await Promise.resolve()
  assert.equal(details.details.value, null)
  assert.equal(details.loading.value, false)
})
