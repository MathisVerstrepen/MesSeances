import assert from 'node:assert/strict'
import { lifetimeFixture } from './helpers/accountLifetime.ts'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed, nextTick, ref, watch, type Ref } from 'vue'
import type {
  AccountAction,
  AccountContinuation,
  AccountSession,
} from '../app/types/account.ts'
import * as accountState from '../app/utils/accountState.ts'
import * as accountGoogle from '../app/utils/accountGoogle.ts'
import type { useAccountApi } from '../app/composables/useAccountApi.ts'
import {
  accountEmailConfirmationToken,
  googleAuthorizationUrl,
  validAccountContinuation,
} from '../app/utils/accountGoogle.ts'

const token = 'a'.repeat(43)
const origin = 'https://messeances.fr'
const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

test('email proof accepts only opaque token or exact own-origin confirmation link', () => {
  assert.equal(accountEmailConfirmationToken(token, origin), token)
  assert.equal(
    accountEmailConfirmationToken(
      ` ${origin}/compte/confirmer-email#token=${token} `,
      origin,
    ),
    token,
  )
  for (const input of [
    `https://attacker.test/compte/confirmer-email#token=${token}`,
    `https://messeances.fr.attacker.test/compte/confirmer-email#token=${token}`,
    `${origin}/compte/confirmer-identite#token=${token}`,
    `${origin}/compte/confirmer-email?token=${token}`,
    `${origin}/compte/confirmer-email#token=${token}&token=${token}`,
    `${origin}/compte/confirmer-email#token=${token}&other=1`,
    `https://user@messeances.fr/compte/confirmer-email#token=${token}`,
    `/compte/confirmer-email#token=${token}`,
    `javascript:alert(1)`,
    token.slice(1),
  ])
    assert.equal(accountEmailConfirmationToken(input, origin), '')
})

test('Google redirect only permits trusted HTTPS origin without credentials', () => {
  assert.equal(
    googleAuthorizationUrl(
      'https://accounts.google.com/o/oauth2/v2/auth?state=fixture',
    ),
    'https://accounts.google.com/o/oauth2/v2/auth?state=fixture',
  )
  for (const url of [
    'http://accounts.google.com',
    'https://accounts.google.com.attacker.test',
    'https://user@accounts.google.com',
    'javascript:alert(1)',
    '/connexion',
  ])
    assert.throws(() => googleAuthorizationUrl(url))
})

test('continuation rejects expired, unknown and unbound actions', () => {
  const now = Date.parse('2026-09-22T12:00:00Z')
  const base = {
    action: 'password_add' as const,
    target: null,
    expires_at: '2026-09-22T12:01:00Z',
  }
  assert.equal(validAccountContinuation(base, now), true)
  assert.equal(
    validAccountContinuation(
      { ...base, action: 'email_change', target: 'new@example.test' },
      now,
    ),
    true,
  )
  assert.equal(
    validAccountContinuation({ ...base, action: 'email_change' }, now),
    false,
  )
  assert.equal(
    validAccountContinuation({ ...base, action: 'password_change' }, now),
    false,
  )
  assert.equal(
    validAccountContinuation({ ...base, target: 'wrong@example.test' }, now),
    false,
  )
  assert.equal(
    validAccountContinuation(
      { ...base, expires_at: '2026-09-22T12:00:00Z' },
      now,
    ),
    false,
  )
  assert.equal(
    validAccountContinuation({ ...base, expires_at: 'invalid' }, now),
    false,
  )
})

test('identity page separates explicit email operations and keeps all bearers in memory', async () => {
  const source = await read('../app/pages/compte/confirmer-identite.vue')
  assert.match(source, /api\.continuation\(\)/)
  assert.match(source, /@submit\.prevent="confirmChallenge"/)
  assert.match(source, /@submit\.prevent="applyAction"/)
  assert.match(source, /value="request"/)
  assert.match(source, /value="confirm"/)
  assert.match(
    source,
    /accountEmailConfirmationToken\(\s*originalLink\.value,\s*window\.location\.origin,?\s*\)/,
  )
  assert.match(
    source,
    /useAccountSecrets\(\s*grant,\s*password,\s*confirmation,\s*originalLink,?\s*\)/,
  )
  assert.match(source, /confirmation\.value !== 'SUPPRIMER'/)
  assert.doesNotMatch(
    source,
    /localStorage|sessionStorage|route\.(query|hash)|onMounted\((confirmChallenge|applyAction)/,
  )
  const warning = await read('../app/components/AccountDeletionWarning.vue')
  assert.match(warning, /sans possibilité d’annulation/)
  assert.match(warning, /réservé pour toujours/)
})

interface IdentityModel {
  grant: Ref<string>
  password: Ref<string>
  confirmation: Ref<string>
  originalLink: Ref<string>
  emailOperation: Ref<string>
  restartRequired: Ref<boolean>
  done: Ref<boolean>
  complete: Ref<boolean>
  errorMessage: Ref<string>
  load: () => Promise<void>
  confirmChallenge: () => Promise<void>
  applyAction: () => Promise<void>
  invalidate: () => void
}

interface CompiledIdentity {
  model?: IdentityModel
}

type IdentityApiOverrides = Partial<
  Pick<
    ReturnType<typeof useAccountApi>,
    'confirmIdentityEmail' | 'changePassword'
  >
>

async function identityFixture(
  action: AccountAction,
  overrides: IdentityApiOverrides = {},
) {
  const calls: { method: string; args: unknown[] }[] = []
  const owner: AccountSession = {
    enabled: true,
    state: 'complete',
    account: {
      email: 'owner@example.test',
      username: 'owner',
      google_linked: true,
      has_password: false,
    },
  }
  const session = ref<AccountSession | null>(owner)
  const status = ref('ready')
  const writesBlocked = ref(false)
  const revision = ref(0)
  const continuation: AccountContinuation = {
    action,
    target: action === 'email_change' ? 'next@example.test' : null,
    expires_at: new Date(Date.now() + 600000).toISOString(),
  }
  const proof = { grant: 'synthetic-proof' }
  const api = {
    continuation: async () => continuation,
    confirmIdentityEmail: async (...args: unknown[]) => {
      calls.push({ method: 'proof', args })
      return proof
    },
    changePassword: async (...args: unknown[]) => {
      calls.push({ method: 'password', args })
    },
    requestEmailChange: async (...args: unknown[]) => {
      calls.push({ method: 'emailRequest', args })
    },
    confirmEmailChange: async (...args: unknown[]) => {
      calls.push({ method: 'emailConfirm', args })
    },
    deleteAccount: async (...args: unknown[]) => {
      calls.push({ method: 'delete', args })
    },
    ...overrides,
  }
  const page = await read('../app/pages/compte/confirmer-identite.vue')
  const script = page.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)?.[1]
  assert.ok(script)
  const output = ts.transpileModule(
    `${script.replaceAll('import.meta.client', 'true').replaceAll('import.meta.server', 'false')}\nexports.model = { grant, password, confirmation, originalLink, emailOperation, restartRequired, done, complete, errorMessage, load, confirmChallenge, applyAction, invalidate };`,
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText
  const exports: CompiledIdentity = {}
  runInNewContext(output, {
    ...lifetimeFixture(revision),
    exports,
    ref,
    computed,
    watch,
    Date,
    JSON,
    setInterval,
    clearInterval,
    require: (name: string) =>
      name.includes('accountGoogle') ? accountGoogle : accountState,
    definePageMeta: () => {},
    useHead: () => {},
    onMounted: () => {},
    onBeforeUnmount: () => {},
    window: { location: { origin } },
    useAccountApi: () => api,
    useAccountSession: () => ({
      session,
      status,
      writesBlocked,
      revision,
      notify: () => {},
      refresh: async () => {},
    }),
    useAccountToken: () => {
      const value = ref(token)
      return {
        token: value,
        ready: ref(true),
        clear: () => {
          value.value = ''
        },
      }
    },
    useAccountSecrets:
      (...values: Ref<string>[]) =>
      () => {
        for (const value of values) value.value = ''
      },
  })
  assert.ok(exports.model)
  return {
    model: exports.model,
    calls,
    session,
    status,
    owner,
    proof,
    writesBlocked,
    revision,
  }
}

test('identity page requires separate explicit proof and action, then adds password', async () => {
  const { model, calls, proof } = await identityFixture('password_add')
  await model.load()
  assert.equal(calls.length, 0, 'GET continuation cannot consume challenge')
  await model.confirmChallenge()
  assert.deepEqual(calls, [
    { method: 'proof', args: [token, 'password_add', undefined] },
  ])
  assert.equal(
    proof.grant,
    '',
    'response proof cleared after copying into component memory',
  )
  assert.equal(model.grant.value, 'synthetic-proof')
  model.password.value = 'A long synthetic password'
  await model.applyAction()
  assert.deepEqual(calls[1], {
    method: 'password',
    args: ['A long synthetic password', 'synthetic-proof'],
  })
  assert.equal(model.done.value, true)
  assert.equal(model.password.value, '')
  assert.equal(model.grant.value, '')
})

test('adding a password displays the shared common-password error and clears submitted secrets', async () => {
  let attempts = 0
  const { model } = await identityFixture('password_add', {
    changePassword: async () => {
      attempts++
      throw new accountState.AccountApiError(400, 'common_password')
    },
  })
  await model.load()
  await model.confirmChallenge()
  model.password.value = 'synthetic-password'
  await model.applyAction()
  assert.equal(attempts, 1)
  assert.equal(
    model.errorMessage.value,
    'Ce mot de passe est trop courant. Choisissez un mot de passe plus difficile à deviner.',
  )
  assert.equal(model.done.value, false)
  assert.equal(model.password.value, '')
  assert.equal(model.grant.value, '')
})

test('Google email confirmation allows same-identity focus revalidation before pasting original link', async () => {
  const { model, calls, session, owner, writesBlocked, revision } =
    await identityFixture('email_change')
  await model.load()
  await model.confirmChallenge()
  writesBlocked.value = true
  revision.value++
  assert.equal(model.complete.value, true)
  await model.applyAction()
  assert.equal(calls.length, 1, 'no writes during revalidation')
  session.value = { ...owner }
  writesBlocked.value = false
  assert.equal(model.grant.value, 'synthetic-proof')
  model.emailOperation.value = 'confirm'
  await nextTick()
  model.originalLink.value = `${origin}/compte/confirmer-identite#token=${token}`
  await model.applyAction()
  assert.equal(
    calls.length,
    1,
    'identity challenge cannot stand in for target confirmation',
  )
  model.originalLink.value = `${origin}/compte/confirmer-email#token=${token}`
  await model.applyAction()
  assert.deepEqual(calls[1], {
    method: 'emailConfirm',
    args: [token, 'synthetic-proof'],
  })
  assert.equal(model.originalLink.value, '')
})

test('Google email request stays distinct from confirmation without URL/storage intent', async () => {
  const { model, calls } = await identityFixture('email_change')
  await model.load()
  await model.confirmChallenge()
  await model.applyAction()
  assert.equal(calls.length, 1)
  model.emailOperation.value = 'request'
  await model.applyAction()
  assert.deepEqual(calls[1], {
    method: 'emailRequest',
    args: ['next@example.test', 'synthetic-proof'],
  })
})

test('deletion requires exact typed confirmation after proof, with no last-method workaround', async () => {
  const { model, calls } = await identityFixture('delete_account')
  await model.load()
  await model.confirmChallenge()
  model.confirmation.value = 'supprimer'
  await model.applyAction()
  assert.equal(calls.length, 1)
  model.confirmation.value = 'SUPPRIMER'
  await model.applyAction()
  assert.deepEqual(calls[1], {
    method: 'delete',
    args: ['synthetic-proof', 'SUPPRIMER'],
  })
  assert.equal(model.confirmation.value, '')
})

test('session change discards late proofs, explicit invalidation clears entered secrets', async () => {
  let resolve: (value: { grant: string }) => void = () => {}
  const pending = new Promise<{ grant: string }>((done) => {
    resolve = done
  })
  const { model, session } = await identityFixture('password_add', {
    confirmIdentityEmail: () => pending,
  })
  await model.load()
  const confirming = model.confirmChallenge()
  session.value = { enabled: true, state: 'anonymous', account: null }
  const proof = { grant: 'late-proof' }
  resolve(proof)
  await confirming
  assert.equal(proof.grant, '')
  assert.equal(model.grant.value, '')
  model.password.value = 'entered-password'
  model.originalLink.value = token
  model.invalidate()
  assert.equal(model.password.value, '')
  assert.equal(model.originalLink.value, '')
})

test('ordinary focus rejects a pending identity proof without clearing visible drafts', async () => {
  let resolve!: (value: { grant: string }) => void
  const pending = new Promise<{ grant: string }>((done) => {
    resolve = done
  })
  const { model, revision, writesBlocked } = await identityFixture(
    'password_add',
    {
      confirmIdentityEmail: () => pending,
    },
  )
  await model.load()
  model.password.value = 'draft-kept-in-memory'
  const confirming = model.confirmChallenge()
  writesBlocked.value = true
  revision.value++
  const proof = { grant: 'stale-proof' }
  resolve(proof)
  await confirming
  assert.equal(proof.grant, '')
  assert.equal(model.grant.value, '')
  assert.equal(model.password.value, 'draft-kept-in-memory')
  assert.equal(model.complete.value, true)
  writesBlocked.value = false
  await model.applyAction()
  assert.equal(model.done.value, false)
})

test('lost sensitive write response cannot auto-replay or reuse proof', async () => {
  let attempts = 0
  const { model } = await identityFixture('password_add', {
    changePassword: async () => {
      attempts++
      throw new accountState.AccountApiError(0)
    },
  })
  await model.load()
  await model.confirmChallenge()
  model.password.value = 'A long synthetic password'
  await model.applyAction()
  await model.applyAction()
  assert.equal(attempts, 1)
  assert.equal(model.restartRequired.value, true)
  assert.equal(model.done.value, false)
  assert.equal(model.grant.value, '')
})
