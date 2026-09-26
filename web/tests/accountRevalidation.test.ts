import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, ref, watch } from 'vue'
import type { useAccountSession } from '../app/composables/useAccountSession.ts'
import type { useAccountDetails } from '../app/composables/useAccountDetails.ts'
import type { useAccountPasswordAction } from '../app/composables/useAccountPasswordAction.ts'
import type { useAccountGoogle } from '../app/composables/useAccountGoogle.ts'
import type { useAccountFlowDraft } from '../app/composables/useAccountFlowDraft.ts'
import type { AccountDetails, AccountSession } from '../app/types/account.ts'
import * as errors from '../app/utils/accountState.ts'
import { lifetimeFixture } from './helpers/accountLifetime.ts'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const owner: AccountSession = {
  enabled: true,
  state: 'complete',
  account: {
    email: 'owner@example.test',
    username: 'owner',
    has_password: true,
    google_linked: false,
  },
}
const initial: AccountDetails = {
  ...owner.account!,
  google_email: null,
  pending_email: null,
  avatar_url: null,
  allowed_methods: ['password'],
}

async function fixture(logoutResponse = Promise.resolve()) {
  const scope = effectScope()
  const app = {}
  const states = new Map()
  const mounted: (() => void)[] = []
  const unmounted: (() => void)[] = []
  let sessions = 0
  let detailsCalls = 0
  let sessionResponse = deferred<AccountSession>()
  let detailsResponse = deferred<AccountDetails>()
  const context = {
    ref,
    computed,
    watch,
    useState: <T>(key: string, init: () => T) => {
      if (!states.has(key)) states.set(key, ref(init()))
      return states.get(key)
    },
    useNuxtApp: () => app,
    useAccountApi: () => ({
      session: () => {
        sessions++
        return sessionResponse.promise
      },
      details: () => {
        detailsCalls++
        return detailsResponse.promise
      },
      logout: () => logoutResponse,
    }),
    onMounted: (fn: () => void) => mounted.push(fn),
    onBeforeUnmount: (fn: () => void) => unmounted.push(fn),
    require: () => errors,
  }
  async function compile<T>(name: string, extra = {}): Promise<T> {
    const source = await readFile(
      new URL(`../app/composables/${name}.ts`, import.meta.url),
      'utf8',
    )
    const exports = {}
    runInNewContext(
      ts.transpileModule(source.replaceAll('import.meta.client', 'false'), {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      }).outputText,
      { ...context, ...extra, exports },
    )
    // SAFETY: callers specify the exact exported composable from the compiled source file.
    return exports as T
  }
  const module = await compile<{ useAccountSession: typeof useAccountSession }>(
    'useAccountSession',
  )
  const account = scope.run(() => module.useAccountSession())!
  Object.assign(context, lifetimeFixture(account.revision))
  account.accept(owner)
  const detailModule = await compile<{
    useAccountDetails: typeof useAccountDetails
  }>('useAccountDetails', { useAccountSession: () => account })
  const details = scope.run(() => detailModule.useAccountDetails())!
  for (const mount of mounted) mount()
  detailsResponse.resolve(initial)
  await detailsResponse.promise
  await Promise.resolve()
  detailsResponse = deferred<AccountDetails>()
  return {
    account,
    details,
    compile,
    scope,
    another: () => module.useAccountSession(),
    get sessions() {
      return sessions
    },
    get detailsCalls() {
      return detailsCalls
    },
    get sessionResponse() {
      return sessionResponse
    },
    get detailsResponse() {
      return detailsResponse
    },
    next() {
      sessionResponse = deferred<AccountSession>()
      detailsResponse = deferred<AccountDetails>()
    },
    stop() {
      for (const unmount of unmounted) unmount()
      scope.stop()
    },
  }
}

test('focus holds visible session/details and one shared flight until both responses finish', async () => {
  const f = await fixture()
  try {
    const snapshot = f.account.session.value
    const details = f.details.details.value
    const pending = f.account.revalidate()
    assert.equal(f.account.status.value, 'ready')
    assert.equal(f.account.session.value, snapshot)
    assert.equal(f.account.revalidating.value, true)
    assert.equal(f.account.writesBlocked.value, true)
    assert.equal(f.another().revalidate(), pending)
    assert.equal(f.sessions, 1)
    f.sessionResponse.resolve(structuredClone(owner))
    await f.sessionResponse.promise
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(f.details.details.value, details)
    assert.equal(f.details.loading.value, false)
    assert.equal(f.account.writesBlocked.value, true)
    assert.equal(f.detailsCalls, 2)
    f.detailsResponse.resolve({
      ...initial,
      pending_email: 'next@example.test',
      google_linked: true,
      allowed_methods: ['google'],
    })
    await pending
    assert.equal(f.account.revalidating.value, false)
    assert.equal(f.account.writesBlocked.value, false)
    assert.equal(f.details.details.value?.pending_email, 'next@example.test')
    assert.deepEqual([...f.details.details.value!.allowed_methods], ['google'])
  } finally {
    f.stop()
  }
})

for (const outcome of [
  'revoked',
  'different',
  'network',
  'details-error',
  'clear',
  'newer',
] as const) {
  test(`focus fails closed without late restoration: ${outcome}`, async () => {
    const f = await fixture()
    try {
      const pending = f.account.revalidate()
      if (outcome === 'network') f.sessionResponse.reject(new Error('offline'))
      else if (outcome === 'revoked')
        f.sessionResponse.resolve({
          enabled: true,
          state: 'anonymous',
          account: null,
        })
      else if (outcome === 'different')
        f.sessionResponse.resolve({
          ...owner,
          account: { ...owner.account!, username: 'other' },
        })
      else {
        f.sessionResponse.resolve(owner)
        await f.sessionResponse.promise
        await new Promise<void>((resolve) => setImmediate(resolve))
        if (outcome === 'details-error')
          f.detailsResponse.reject(new Error('offline'))
        else {
          f.account.clear()
          if (outcome === 'newer')
            f.account.accept({
              enabled: true,
              state: 'anonymous',
              account: null,
            })
          f.detailsResponse.resolve(initial)
        }
      }
      await pending
      assert.equal(f.details.details.value, null)
      assert.notEqual(f.account.session.value?.state, 'complete')
      assert.equal(f.account.revalidating.value, false)
    } finally {
      f.stop()
    }
  })
}

test('pending password and Google proofs are invalidated by focus even with identical session DTO', async () => {
  const f = await fixture()
  try {
    let writes = 0
    const passwordProof = deferred<{ grant: string }>()
    const googleProof = deferred<{ authorization_url: string }>()
    const globals = {
      useAccountSession: () => f.account,
      useAccountApi: () => ({
        reauthPassword: () => passwordProof.promise,
        googleStart: () => googleProof.promise,
      }),
      require: () => ({ googleAuthorizationUrl: (url: string) => url }),
      window: {
        location: {
          assign: () => {
            writes++
          },
        },
      },
    }
    const passwordModule = await f.compile<{
      useAccountPasswordAction: typeof useAccountPasswordAction
    }>('useAccountPasswordAction', globals)
    const googleModule = await f.compile<{
      useAccountGoogle: typeof useAccountGoogle
    }>('useAccountGoogle', globals)
    const password = f.scope.run(() =>
      passwordModule.useAccountPasswordAction(),
    )!
    const google = f.scope.run(() => googleModule.useAccountGoogle())!
    const action = password(
      'secret',
      'password_change',
      undefined,
      async () => {
        writes++
      },
    )
    const redirect = google({ mode: 'reauth', action: 'password_add' })
    const rejected = assert.rejects(redirect, /Account state changed/)
    const refresh = f.account.revalidate()
    assert.equal(
      await password('secret', 'password_change', undefined, async () => {
        writes++
      }),
      false,
    )
    await assert.rejects(google(), /Account state changed/)
    f.sessionResponse.resolve(owner)
    f.detailsResponse.resolve(initial)
    await refresh
    const proof = { grant: 'late-proof' }
    passwordProof.resolve(proof)
    googleProof.resolve({
      authorization_url: 'https://accounts.google.com/o/oauth2/v2/auth',
    })
    assert.equal(await action, false)
    await rejected
    assert.equal(writes, 0)
    assert.equal(proof.grant, '')
  } finally {
    f.stop()
  }
})

test('revalidation runtime and requests are isolated between Nuxt applications', async () => {
  const a = await fixture()
  const b = await fixture()
  try {
    const pending = a.account.revalidate()
    assert.equal(b.account.revalidating.value, false)
    assert.equal(b.sessions, 0)
    a.account.clear()
    a.sessionResponse.resolve(owner)
    await pending
    assert.equal(b.account.session.value?.account?.username, 'owner')
  } finally {
    a.stop()
    b.stop()
  }
})

test('logout fences a preceding focus response before its own session recovery', async () => {
  const f = await fixture()
  try {
    const pending = f.account.revalidate()
    await assert.rejects(f.account.logout())
    f.account.clear()
    f.account.accept(owner)
    const late = f.sessionResponse
    f.next()
    const logout = f.account.logout()
    late.resolve(owner)
    await pending
    assert.equal(f.details.details.value, null)
    f.sessionResponse.resolve({
      enabled: true,
      state: 'anonymous',
      account: null,
    })
    await logout
    assert.equal(f.account.session.value?.state, 'anonymous')
  } finally {
    f.stop()
  }
})

test('logout completion cannot resurrect state after pagehide/clear or a newer accepted session', async () => {
  for (const newer of [false, true]) {
    const response = deferred<void>()
    const f = await fixture(response.promise)
    try {
      const logout = f.account.logout()
      f.account.clear()
      if (newer)
        f.account.accept({ enabled: true, state: 'anonymous', account: null })
      response.resolve()
      await logout
      assert.equal(
        f.sessions,
        0,
        'late logout never launches recovery over newer state',
      )
      assert.equal(f.details.details.value, null)
      assert.equal(
        f.account.session.value?.state ?? null,
        newer ? 'anonymous' : null,
      )
    } finally {
      f.stop()
    }
  }
})

const flowSessions: AccountSession[] = [
  { enabled: true, state: 'anonymous', account: null },
  ...(['pending_email', 'pending_username'] as const).map((state) => ({
    enabled: true,
    state,
    account: { ...owner.account!, username: null },
  })),
]

for (const session of flowSessions) {
  test(`${session.state} drafts survive only identical enabled identity, not invalidation`, async () => {
    const f = await fixture()
    try {
      f.account.accept(session)
      const module = await f.compile<{
        useAccountFlowDraft: typeof useAccountFlowDraft
      }>('useAccountFlowDraft', {
        useAccountSession: () => f.account,
        useAccountSecrets:
          (...values: ReturnType<typeof ref<string>>[]) =>
          () => {
            for (const value of values) value.value = ''
          },
      })
      const draft = ref('draft')
      const token = ref('synthetic-token')
      f.scope.run(() => module.useAccountFlowDraft(draft, token))
      const pending = f.account.revalidate()
      assert.equal(draft.value, 'draft')
      assert.equal(token.value, 'synthetic-token')
      f.sessionResponse.resolve(structuredClone(session))
      await pending
      assert.equal(draft.value, 'draft')
      assert.equal(token.value, 'synthetic-token')
      f.account.clear()
      assert.equal(draft.value, '')
      assert.equal(token.value, '')
      f.account.accept(session)
      assert.equal(token.value, '', 'recovery must not restore a cleared token')
      draft.value = 'new draft'
      f.account.accept({
        ...session,
        state:
          session.state === 'pending_email'
            ? 'pending_username'
            : 'pending_email',
      })
      assert.equal(
        draft.value,
        '',
        'state change with identical account clears synchronously',
      )
    } finally {
      f.stop()
    }
  })
  test(`${session.state} focus preserves display and deduplicates until same identity resolves`, async () => {
    const f = await fixture()
    try {
      f.account.accept(session)
      const snapshot = f.account.session.value
      const pending = f.account.revalidate()
      assert.equal(f.account.status.value, 'ready')
      assert.equal(f.account.session.value, snapshot)
      assert.equal(f.account.revalidating.value, true)
      assert.equal(f.account.writesBlocked.value, true)
      assert.equal(f.another().revalidate(), pending)
      assert.equal(f.sessions, 1)
      f.sessionResponse.resolve(structuredClone(session))
      await pending
      assert.equal(f.account.status.value, 'ready')
      assert.equal(f.account.writesBlocked.value, false)
      assert.equal(
        f.detailsCalls,
        1,
        'no complete-account details for auth flow',
      )
    } finally {
      f.stop()
    }
  })
  for (const outcome of [
    'state',
    'identity',
    'disabled',
    'network',
    'clear',
    'newer',
  ] as const) {
    test(`${session.state} focus rejects ${outcome} without adopting another identity`, async () => {
      const f = await fixture()
      try {
        f.account.accept(session)
        const pending = f.account.revalidate()
        if (outcome === 'network')
          f.sessionResponse.reject(new Error('offline'))
        else {
          if (outcome === 'clear' || outcome === 'newer') f.account.clear()
          if (outcome === 'newer') f.account.accept(owner)
          f.sessionResponse.resolve(
            outcome === 'state'
              ? {
                  ...session,
                  state:
                    session.state === 'pending_email'
                      ? 'pending_username'
                      : 'pending_email',
                }
              : outcome === 'identity'
                ? {
                    ...session,
                    account: { ...owner.account!, email: 'other@example.test' },
                  }
                : outcome === 'disabled'
                  ? { ...session, enabled: false }
                  : session,
          )
        }
        await pending
        assert.equal(
          f.account.session.value?.state ?? null,
          outcome === 'newer' ? 'complete' : null,
        )
        assert.equal(
          f.account.status.value,
          outcome === 'newer'
            ? 'ready'
            : outcome === 'clear'
              ? 'idle'
              : 'error',
        )
        assert.equal(f.account.revalidating.value, false)
      } finally {
        f.stop()
      }
    })
  }
}

test('initial/recovery focus uses a deduplicated skeleton path and permits retry after failure', async () => {
  const f = await fixture()
  try {
    f.account.clear()
    const pending = f.account.revalidate()
    assert.equal(f.account.status.value, 'loading')
    assert.equal(f.account.session.value, null)
    assert.equal(f.account.revalidating.value, false)
    assert.equal(f.account.revalidate(), pending)
    f.sessionResponse.reject(new Error('offline'))
    await pending
    assert.equal(f.account.status.value, 'error')
    f.next()
    const recovery = f.account.revalidate()
    assert.equal(f.sessions, 2)
    f.sessionResponse.resolve(owner)
    f.detailsResponse.resolve(initial)
    await recovery
    assert.equal(f.account.status.value, 'ready')
  } finally {
    f.stop()
  }
})
