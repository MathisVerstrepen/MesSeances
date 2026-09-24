import assert from 'node:assert/strict'
import { lifetimeFixture } from './helpers/accountLifetime.ts'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { type Context, runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import * as icons from '@lucide/vue'
import ts from 'typescript'
import * as vue from 'vue'
import type { LocationQueryValue } from 'vue-router'
import * as accountState from '../app/utils/accountState.ts'
import type { AccountSession } from '../app/types/account.ts'

const guidance =
  'Un compte utilise déjà cette adresse email. Connectez-vous avec votre moyen habituel, puis associez Google depuis votre compte.'
const generic =
  'La confirmation Google n’a pas abouti ou a expiré. Recommencez depuis votre compte ou utilisez votre moyen de connexion habituel.'
type CallbackQuery = {
  error: LocationQueryValue | LocationQueryValue[] | undefined
}
interface CallbackRoute {
  query: CallbackQuery
}
type Middleware = (to: {
  path: string
  query: CallbackQuery
}) => Promise<string | undefined>
interface CompiledModule<T> {
  default?: T
}

const cases: [CallbackQuery['error'], string][] = [
  ['google_email_in_use', guidance],
  ['google_failed', generic],
  [undefined, ''],
  [null, ''],
  ['', ''],
  ['unknown', ''],
  ['GOOGLE_EMAIL_IN_USE', ''],
  ['google_email_in_use ', ''],
  ['<script>alert(1)</script>', ''],
  ['https://untrusted.invalid', ''],
  [[], ''],
  [['google_email_in_use'], ''],
  [['google_failed'], ''],
  [['google_email_in_use', 'google_failed'], ''],
  [['google_email_in_use', 'google_email_in_use'], ''],
]
const states = [
  'anonymous',
  'pending_email',
  'pending_username',
  'complete',
] as const

function sessionFor(state: AccountSession['state']): AccountSession {
  return {
    enabled: true,
    state,
    account:
      state === 'anonymous'
        ? null
        : {
            email: 'synthetic@example.test',
            username: state === 'complete' ? 'synthetic' : null,
            has_password: false,
            google_linked: true,
          },
  }
}

const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')

function compile<T>(source: string, globals: Context): T {
  const compiled = ts.transpileModule(
    source.replaceAll('import.meta.client', 'false'),
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText
  const exports: CompiledModule<T> = {}
  runInNewContext(compiled, {
    exports,
    require: (name: string) => {
      if (name === 'vue') return vue
      if (name === '@lucide/vue') return icons
      if (name === '~/utils/accountState') return accountState
      throw new Error(`Unexpected import: ${name}`)
    },
    ...vue,
    ...globals,
  })
  assert.ok(exports.default)
  return exports.default
}

async function component(path: string, globals: Context) {
  const { descriptor } = parse(await read(path), { filename: path })
  return compile<vue.Component>(
    compileScript(descriptor, { id: path, inlineTemplate: true }).content,
    globals,
  )
}

test('Google callback copy accepts only fixed scalar codes without assuming a password', () => {
  for (const [error, message] of cases)
    assert.equal(accountState.accountGoogleCallbackMessage(error), message)
  assert.doesNotMatch(guidance, /mot de passe/i)
})

test('callback middleware preserves guidance and existing sessions without redirects or loops', async () => {
  const source = await read('../app/middleware/account-auth.ts')
  for (const state of states) {
    const session = vue.ref(sessionFor(state))
    const original = session.value
    let refreshes = 0
    const redirects: string[] = []
    const middleware = compile<Middleware>(source, {
      defineNuxtRouteMiddleware: (handler: Middleware) => handler,
      useNuxtApp: () => ({ isHydrating: false }),
      useAccountSession: () => ({
        session,
        revalidate: async () => {
          refreshes++
        },
      }),
      navigateTo: (path: string) => {
        redirects.push(path)
        return path
      },
    })
    for (const [error, message] of cases) {
      redirects.length = 0
      const destination =
        message !== '' || state === 'anonymous'
          ? undefined
          : accountState.accountDestination(original)
      for (let visit = 0; visit < 2; visit++)
        assert.equal(
          await middleware({ path: '/connexion', query: { error } }),
          destination,
        )
      assert.deepEqual(redirects, destination ? [destination, destination] : [])
      assert.equal(session.value, original)
    }
    assert.equal(refreshes, cases.length * 2)
    assert.equal(
      await middleware({
        path: '/inscription',
        query: { error: 'google_email_in_use' },
      }),
      state === 'anonymous'
        ? undefined
        : accountState.accountDestination(original),
    )
  }
})

test('connexion renders safe callback alerts with login recovery or return-account action', async () => {
  for (const state of states) {
    const session = vue.ref(sessionFor(state))
    const route: CallbackRoute = { query: { error: undefined } }
    const globals = {
      ...lifetimeFixture(),
      definePageMeta: () => {},
      useHead: () => {},
      useRoute: () => route,
      useAccountSession: () => ({ session, writesBlocked: vue.ref(false) }),
      useAccountApi: () => ({}),
      useAccountGoogle: () => () => {},
      useAccountFlowDraft: () => () => {},
    }
    const page = await component('../app/pages/connexion.vue', globals)
    const credentials = await component(
      '../app/components/AccountCredentialsForm.vue',
      globals,
    )
    const password = await component(
      '../app/components/AccountPasswordField.vue',
      globals,
    )
    for (const [error, message] of cases) {
      route.query.error = error
      const app = vue.createSSRApp(page)
      app.component('NuxtLink', {
        props: ['to', 'prefetch'],
        setup:
          (props, { slots }) =>
          () =>
            vue.h('a', { href: props.to }, slots.default?.()),
      })
      app.component('AccountShell', {
        setup:
          (_, { slots }) =>
          () =>
            vue.h('main', slots.default?.()),
      })
      app.component('AccountCredentialsForm', credentials)
      app.component('AccountPasswordField', password)
      const html = await renderToString(app)
      if (message) {
        assert.match(html, /role="alert"/)
        assert.ok(html.includes(message))
      } else {
        assert.doesNotMatch(html, /role="alert"/)
        assert.ok(!html.includes(guidance))
        assert.ok(!html.includes(generic))
      }
      assert.doesNotMatch(html, /<script>|untrusted\.invalid/)
      if (state === 'anonymous') {
        assert.match(html, /autocomplete="email"/)
        assert.match(html, /autocomplete="current-password"/)
        assert.match(html, /href="\/mot-de-passe-oublie"/)
        assert.match(html, /Se connecter/)
        assert.match(html, /Continuer avec Google/)
        assert.doesNotMatch(html, /Revenir à mon compte/)
      } else {
        assert.match(html, /href="\/compte"[^>]*>Revenir à mon compte<\/a>/)
        assert.doesNotMatch(html, /<form/)
      }
    }
  }
})
