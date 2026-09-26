import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { type Context, runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { compile, computed, createSSRApp, ref, type Ref } from 'vue'
import * as accountState from '../app/utils/accountState.ts'
import type { AccountSession } from '../app/types/account.ts'

const read = (path: string) => readFile(new URL(path, import.meta.url), 'utf8')
const home = await read('../app/pages/compte/index.vue')
const navigation = await read('../app/components/AccountAreaNavigation.vue')
const shell = await read('../app/components/AccountShell.vue')
const settings = await read('../app/pages/compte/parametres.vue')
const middleware = await read('../app/middleware/account-auth.ts')

type Guard = (to: {
  path: string
  query: Record<string, string>
}) => Promise<string | undefined>
interface ScriptExports {
  state?: {
    account: { session: Ref<AccountSession | null> }
    complete: Ref<boolean>
    accountDestination: typeof accountState.accountDestination
  }
  active?: Ref<boolean>
  default?: Guard
}

function evaluate(source: string, globals: Context) {
  const exports: ScriptExports = {}
  runInNewContext(
    ts.transpileModule(source, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    { exports, ...globals },
  )
  return exports
}

test('home renders one available section without initializing settings data or secrets', async () => {
  const session = ref<AccountSession | null>({
    enabled: true,
    state: 'complete',
    account: null,
  })
  const { descriptor } = parse(home)
  let title = ''
  const state = evaluate(
    `${descriptor.scriptSetup!.content}\nexports.state = { account, complete, accountDestination };`,
    {
      computed,
      require: (name: string) => (name === '@lucide/vue' ? {} : accountState),
      definePageMeta: (meta: { middleware: string }) =>
        assert.equal(meta.middleware, 'account-auth'),
      useHead: (head: { title: string }) => {
        title = head.title
      },
      useAccountSession: () => ({ session }),
    },
  ).state
  assert.ok(state)
  const render = async () => {
    const app = createSSRApp({
      render: compile(descriptor.template!.content),
      setup: () => state,
    })
    app.component('AccountShell', {
      props: ['title'],
      template: '<main><h1>{{ title }}</h1><slot /></main>',
    })
    app.component('NuxtLink', {
      props: ['to', 'prefetch'],
      template: '<a :href="to"><slot /></a>',
    })
    for (const icon of ['Settings', 'ChevronRight'])
      app.component(icon, { template: '<svg />' })
    return renderToString(app)
  }
  const html = await render()
  assert.equal(title, 'Mon compte - MesSeances')
  assert.match(html, /<h1>Mon compte<\/h1>/)
  assert.equal([...html.matchAll(/<li>/g)].length, 1)
  assert.match(html, /href="\/compte\/parametres"/)
  assert.match(home, /:prefetch="false"/)
  assert.match(home, /min-h-12/)
  assert.doesNotMatch(
    home,
    /useAccount(?:Api|Details|PasswordAction|Google|Secrets)|AccountAvatar|Watchlist|Amis/,
  )
  assert.doesNotMatch(html, /<input|<form|Reprendre la connexion/)
  for (const [next, destination] of [
    ['anonymous', '/connexion'],
    ['pending_email', '/verification'],
    ['pending_username', '/finaliser'],
  ] as const) {
    session.value = { enabled: true, state: next, account: null }
    const lost = await render()
    assert.doesNotMatch(lost, /Rubriques du compte|href="\/compte\/parametres"/)
    assert.match(lost, /Reprendre la connexion/)
    assert.ok(lost.includes(`href="${destination}"`))
  }
  session.value = null
  assert.match(await render(), /href="\/connexion"/)
})

test('desktop settings selection follows exact normalized route, not account home', async () => {
  const { descriptor } = parse(navigation)
  for (const path of [
    '/compte',
    '/compte/',
    '/compte/parametres',
    '/COMPTE/PARAMETRES/',
    '/compte/confirmer-email',
  ]) {
    const exports = evaluate(
      `${descriptor.scriptSetup!.content}\nexports.active = settingsActive;`,
      {
        computed,
        useRoute: () => ({ path }),
        require: () => ({}),
      },
    )
    const active = exports.active
    assert.ok(active)
    assert.equal(active.value, path.toLowerCase().includes('/parametres'))
    const app = createSSRApp({
      render: compile(descriptor.template!.content),
      setup: () => ({ settingsActive: active, upcomingEntries: [] }),
    })
    app.component('NuxtLink', {
      props: ['to', 'prefetch'],
      template: '<a :href="to"><slot /></a>',
    })
    app.component('Settings', { template: '<svg />' })
    const html = await renderToString(app)
    assert.equal(html.includes('aria-current="page"'), active.value)
    assert.match(html, /href="\/compte\/parametres"/)
  }
  assert.match(navigation, /account-area-navigation hidden[^"\n]*lg:block/)
})

test('mobile section return precedes sole heading and leaves desktop geometry intact', () => {
  assert.match(settings, /back-to-account/)
  assert.match(settings, /title: 'Paramètres - MesSeances'/)
  assert.doesNotMatch(home, /back-to-account/)
  assert.equal([...shell.matchAll(/<h1\b/g)].length, 1)
  assert.match(
    shell,
    /v-if="backToAccount"\s+to="\/compte"\s+:prefetch="false"\s+class="[^"]*min-h-12[^"]*lg:hidden"/,
  )
  const returnLinkClasses = shell
    .match(/v-if="backToAccount"[\s\S]*?class="([^"]+)"/)![1]!
    .split(/\s+/)
  for (const className of ['-mt-4', 'mb-2', 'flex', 'w-fit'])
    assert.ok(returnLinkClasses.includes(className))
  assert.match(shell, /px-4 py-8 sm:px-8 lg:px-12 lg:py-10/)
  assert.ok(shell.indexOf('v-if="backToAccount"') < shell.indexOf('<h1'))
  assert.match(shell, /aria-hidden="true">‹<\/span>\s+Mon compte/)
  assert.doesNotMatch(shell, /grid-rows-\[auto_1fr\]/)
  assert.match(shell, /lg:grid-cols-\[15rem_minmax\(0,1fr\)\]/)
  assert.match(shell, /max-w-\[60rem\]/)
  for (const state of [
    "status === 'idle'",
    "status === 'loading'",
    "status === 'error'",
    '!session.enabled',
  ])
    assert.ok(shell.includes(state))
})

test('home and settings require complete sessions while confirmation routes stay exempt', async () => {
  for (const path of [
    '/compte',
    '/compte/',
    '/COMPTE',
    '/compte/parametres',
    '/compte/parametres/',
    '/COMPTE/PARAMETRES/',
    '/compte/confirmer-email',
    '/compte/confirmer-identite',
  ]) {
    for (const [state, destination] of [
      ['anonymous', '/connexion'],
      ['pending_email', '/verification'],
      ['pending_username', '/finaliser'],
      ['complete', undefined],
    ] as const) {
      for (const mode of [
        'ready',
        'disabled',
        'unavailable',
        'hydrate',
        'later',
      ] as const) {
        const session = ref<AccountSession | null>(
          mode === 'unavailable'
            ? null
            : { enabled: mode !== 'disabled', state, account: null },
        )
        let checks = 0
        const exports = evaluate(
          middleware.replaceAll('import.meta.client', 'true'),
          {
            require: () => accountState,
            defineNuxtRouteMiddleware: (handler: Guard) => handler,
            useNuxtApp: () => ({ isHydrating: mode === 'hydrate' }),
            useAccountSession: () => ({
              session,
              status: ref(mode === 'unavailable' ? 'error' : 'ready'),
              revalidate: async () => {
                checks++
                if (mode === 'later')
                  session.value = {
                    enabled: true,
                    state: 'anonymous',
                    account: null,
                  }
              },
            }),
            navigateTo: (to: string) => to,
          },
        )
        const guard = exports.default
        assert.ok(guard)
        const exempt =
          path.includes('/confirmer-') ||
          mode === 'disabled' ||
          mode === 'unavailable'
        assert.equal(
          await guard({ path, query: {} }),
          exempt ? undefined : mode === 'later' ? '/connexion' : destination,
          `${path} ${state} ${mode}`,
        )
        assert.equal(checks, mode === 'hydrate' ? 0 : 1)
      }
    }
  }
})
