import assert from 'node:assert/strict'
import { lifetimeFixture } from './helpers/accountLifetime.ts'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, ref, watch, type Ref } from 'vue'
import type { AccountSession } from '../app/types/account.ts'
import * as accountState from '../app/utils/accountState.ts'

const source = await readFile(
  new URL('../app/pages/compte/index.vue', import.meta.url),
  'utf8',
)
const names = [
  'editor',
  'busy',
  'currentPassword',
  'newPassword',
  'email',
  'emailPassword',
  'googlePassword',
  'deletionPassword',
  'confirmation',
  'passwordError',
  'emailError',
  'googleError',
  'deletionError',
  'notice',
] as const
type State = Record<(typeof names)[number], Ref<string | null>> & {
  toggleEditor: (next: string) => Promise<void>
  googleProof: (action: string) => Promise<void>
  changePassword: () => Promise<void>
  clearSecrets: () => void
  logout: () => Promise<void>
  requestEmail: () => Promise<void>
  changeGoogle: () => Promise<void>
  deleteAccount: () => Promise<void>
  cancelEmail: () => Promise<void>
  logoutAll: () => Promise<void>
}
interface ScriptExports {
  state?: State
}

function fixture() {
  const scope = effectScope()
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
  const session = ref<AccountSession | null>(owner)
  const status = ref('ready')
  const writesBlocked = ref(false)
  const focused: string[] = []
  const calls: string[] = []
  const initialDetails = {
    ...owner.account,
    avatar_url: null,
    allowed_methods: ['password'],
  }
  const details = ref<typeof initialDetails | null>(initialDetails)
  class Button {
    id: string
    disabled = false
    constructor(id: string) {
      this.id = id
    }
    focus() {
      focused.push(this.id)
      document.activeElement = this
    }
  }
  const elements = new Map<string, Button>()
  const document = {
    // SAFETY: the mock assigns only Button instances or null to activeElement.
    activeElement: null as Button | null,
    getElementById(id: string) {
      if (!elements.has(id)) elements.set(id, new Button(id))
      return elements.get(id)!
    },
  }
  let write: () => Promise<boolean> = async () => true
  let refresh: () => Promise<void> = async () => {
    calls.push('refresh')
  }
  const result: ScriptExports = {}
  const script = source
    .split('<script setup lang="ts">')[1]!
    .split('</script>')[0]!
  const compiled = ts.transpileModule(
    `${script.replaceAll('import.meta.client', 'true')}\nexports.state = { ${names.join(',')}, toggleEditor, googleProof, changePassword, clearSecrets, logout, requestEmail, changeGoogle, deleteAccount, cancelEmail, logoutAll }`,
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText
  scope.run(() =>
    runInNewContext(compiled, {
      ...lifetimeFixture(),
      exports: result,
      require: () => accountState,
      ref,
      computed,
      watch,
      nextTick,
      definePageMeta: () => {},
      useHead: () => {},
      useAccountApi: () => ({}),
      useAccountSession: () => ({
        session,
        status,
        writesBlocked,
        notify: () => calls.push('notify'),
        refresh: () => refresh(),
        logout: async () => calls.push('logout'),
      }),
      useAccountDetails: () => ({
        details,
        loading: ref(false),
        errorMessage: ref(''),
        refresh: async () => {},
      }),
      useAccountPasswordAction: () => () => write(),
      useAccountGoogle: () => async () => {
        throw new accountState.AccountApiError(503, 'unavailable')
      },
      useAccountSecrets:
        (...values: Ref<string>[]) =>
        () => {
          for (const value of values) value.value = ''
        },
      document,
      HTMLButtonElement: Button,
      navigateTo: async (path: string) => calls.push(path),
    }),
  )
  return {
    state: result.state!,
    session,
    status,
    writesBlocked,
    owner,
    calls,
    focused,
    details,
    document,
    setRefresh: (callback: typeof refresh) => {
      refresh = callback
    },
    stop: () => scope.stop(),
    setWrite: (callback: typeof write) => {
      write = callback
    },
  }
}

test('overview opens one editor, clears secrets/errors on switch/cancel and restores focus', async () => {
  const f = fixture()
  try {
    await f.state.toggleEditor('password')
    assert.equal(f.focused.at(-1), 'current-password')
    f.state.currentPassword.value = 'secret'
    f.state.passwordError.value = 'stale'
    await f.state.toggleEditor('email')
    assert.equal(f.state.editor.value, 'email')
    assert.equal(f.state.currentPassword.value, '')
    assert.equal(f.state.passwordError.value, '')
    assert.equal(f.focused.at(-1), 'new-email')
    f.state.email.value = 'draft@example.test'
    await f.state.toggleEditor('email')
    assert.equal(f.state.editor.value, null)
    assert.equal(f.state.email.value, '')
    assert.equal(f.focused.at(-1), 'trigger-email')
  } finally {
    f.stop()
  }
})

test('password editor shows the shared common-password error and remains open for correction', async () => {
  const f = fixture()
  try {
    let attempts = 0
    f.setWrite(async () => {
      attempts++
      throw new accountState.AccountApiError(400, 'common_password')
    })
    await f.state.toggleEditor('password')
    f.state.currentPassword.value = 'synthetic-current-password'
    f.state.newPassword.value = 'synthetic-new-password'
    await f.state.changePassword()
    assert.equal(attempts, 1)
    assert.equal(
      f.state.passwordError.value,
      'Ce mot de passe est trop courant. Choisissez un mot de passe plus difficile à deviner.',
    )
    assert.equal(f.state.editor.value, 'password')
    assert.equal(f.state.busy.value, '')
    assert.equal(f.state.notice.value, '')
    assert.deepEqual(f.calls, [])
  } finally {
    f.stop()
  }
})

test('transient same-account refresh preserves input, changed/revoked/error sessions clear it', async () => {
  for (const outcome of ['same', 'changed', 'revoked', 'error']) {
    const f = fixture()
    try {
      await f.state.toggleEditor('delete')
      f.state.deletionPassword.value = 'secret'
      f.state.confirmation.value = 'SUPPRIMER'
      f.writesBlocked.value = true
      await nextTick()
      assert.equal(f.state.deletionPassword.value, 'secret')
      f.writesBlocked.value = false
      f.status.value = outcome === 'error' ? 'error' : 'ready'
      f.session.value =
        outcome === 'same'
          ? f.owner
          : outcome === 'changed'
            ? {
                ...f.owner,
                account: { ...f.owner.account!, username: 'someone_else' },
              }
            : null
      await nextTick()
      assert.equal(
        f.state.deletionPassword.value,
        outcome === 'same' ? 'secret' : '',
      )
      assert.equal(
        f.state.confirmation.value,
        outcome === 'same' ? 'SUPPRIMER' : '',
      )
      assert.equal(f.state.editor.value, outcome === 'same' ? 'delete' : null)
    } finally {
      f.stop()
    }
  }
})

test('in-flight writes block cancel/switch and successful write closes editor', async () => {
  const f = fixture()
  try {
    let finish: (value: boolean) => void = () => {}
    f.setWrite(
      () =>
        new Promise((resolve) => {
          finish = resolve
        }),
    )
    await f.state.toggleEditor('password')
    f.state.newPassword.value = 'LongPassword123!'
    const action = f.state.changePassword()
    await f.state.toggleEditor('email')
    await f.state.toggleEditor('password')
    assert.equal(f.state.editor.value, 'password')
    assert.equal(f.state.newPassword.value, 'LongPassword123!')
    finish(true)
    await action
    assert.equal(f.state.editor.value, null)
    assert.equal(f.state.newPassword.value, '')
    assert.deepEqual(f.calls, ['notify', 'refresh'])
  } finally {
    f.stop()
  }
})

test('success focus survives overlapping same-account rechecks without stealing a moved focus', async () => {
  const f = fixture()
  try {
    const snapshot = f.details.value
    await f.state.toggleEditor('password')
    f.state.newPassword.value = 'LongPassword123!'
    await f.state.changePassword()
    await nextTick()
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(f.document.activeElement?.id, 'trigger-password')
    f.details.value = null
    f.document.activeElement = null // Vue unmounts the old overview during revalidation.
    await nextTick()
    f.details.value = snapshot
    await nextTick()
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(f.document.activeElement?.id, 'trigger-password')
    f.document.getElementById('unrelated-link').focus()
    f.details.value = null
    f.document.activeElement = null
    await nextTick()
    f.details.value = snapshot
    await nextTick()
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.equal(f.document.activeElement, null)
  } finally {
    f.stop()
  }
})

test('completed mutation restores its trigger after destructive recovery clears drafts', async () => {
  const f = fixture()
  const detailsValue = f.details.value
  try {
    f.setRefresh(async () => {
      f.status.value = 'loading'
      f.session.value = null
      f.details.value = null
      await nextTick()
      f.session.value = { ...f.owner }
      f.status.value = 'ready'
      f.details.value = detailsValue
    })
    await f.state.toggleEditor('password')
    f.state.currentPassword.value = 'CurrentPassword123!'
    f.state.newPassword.value = 'LongPassword123!'
    await f.state.changePassword()
    await nextTick()
    assert.equal(f.state.editor.value, null)
    assert.equal(f.state.newPassword.value, '')
    assert.equal(f.focused.at(-1), 'trigger-password')
  } finally {
    f.stop()
  }
})

test('a superseding destructive broadcast refresh keeps only the successful action focus target', async () => {
  const f = fixture()
  const details = f.details.value
  try {
    f.setRefresh(async () => {
      f.status.value = 'loading'
      f.session.value = null
      f.details.value = null
      // The initiating refresh resolves stale while the broadcast refresh is held.
      await nextTick()
    })
    await f.state.toggleEditor('password')
    f.state.currentPassword.value = 'CurrentPassword123!'
    f.state.newPassword.value = 'LongPassword123!'
    await f.state.changePassword()
    await nextTick()
    assert.equal(f.state.editor.value, null)
    assert.equal(f.state.currentPassword.value, '')
    f.session.value = { ...f.owner }
    f.status.value = 'ready'
    f.details.value = details
    await nextTick()
    assert.equal(f.focused.at(-1), 'trigger-password')
  } finally {
    f.stop()
  }
})

test('Google proof failures belong to expanded password/deletion action, never hidden Google editor', async () => {
  const f = fixture()
  try {
    for (const [editor, action, error] of [
      ['password', 'password_add', 'passwordError'],
      ['delete', 'delete_account', 'deletionError'],
    ] as const) {
      await f.state.toggleEditor(editor)
      await f.state.googleProof(action)
      assert.ok(f.state[error].value)
      assert.equal(f.state.googleError.value, '')
      assert.equal(f.state.editor.value, editor)
    }
  } finally {
    f.stop()
  }
})

test('uncertain write destructively recovers once without replay and keeps recovery notice visible', async () => {
  const f = fixture()
  try {
    let writes = 0
    f.setRefresh(async () => {
      f.calls.push('refresh')
      f.session.value = null
      f.status.value = 'loading'
      await nextTick()
      f.session.value = { ...f.owner }
      f.status.value = 'ready'
    })
    f.setWrite(async () => {
      writes++
      throw new accountState.AccountApiError(0, 'network_error')
    })
    await f.state.toggleEditor('password')
    f.state.newPassword.value = 'LongPassword123!'
    await f.state.changePassword()
    assert.equal(writes, 1)
    assert.equal(f.state.editor.value, null)
    assert.match(f.state.notice.value!, /sans répéter l’action/)
    assert.equal(f.state.newPassword.value, '')
    assert.deepEqual(f.calls, ['notify', 'refresh'])
  } finally {
    f.stop()
  }
})

test('local logout reuses account.logout and router navigation', async () => {
  const f = fixture()
  try {
    await f.state.logout()
    assert.deepEqual(f.calls, ['logout', '/connexion'])
  } finally {
    f.stop()
  }
})

test('all overview mutation handlers block programmatic calls throughout revalidation', async () => {
  const f = fixture()
  try {
    await f.state.toggleEditor('email')
    f.state.email.value = 'draft@example.test'
    f.writesBlocked.value = true
    for (const handler of [
      'requestEmail',
      'changeGoogle',
      'changePassword',
      'deleteAccount',
      'cancelEmail',
      'logoutAll',
      'logout',
    ] as const)
      await f.state[handler]()
    await f.state.googleProof('password_add')
    assert.deepEqual(f.calls, [])
    assert.equal(f.state.email.value, 'draft@example.test')
    assert.equal(f.state.busy.value, '')
    assert.equal(f.state.emailError.value, '')
  } finally {
    f.stop()
  }
})

test('destructive loading clears drafts immediately, unlike ordinary focus', async () => {
  const f = fixture()
  try {
    await f.state.toggleEditor('email')
    f.state.email.value = 'draft@example.test'
    f.state.emailPassword.value = 'secret'
    f.status.value = 'loading'
    f.session.value = null
    assert.equal(f.state.email.value, '')
    assert.equal(f.state.emailPassword.value, '')
    assert.equal(f.state.editor.value, null)
  } finally {
    f.stop()
  }
})

test('overview preserves conditional forms, last-method guard, deletion warning and shell opt-out', async () => {
  assert.match(source, /hide-logout/)
  assert.match(source, /v-if="editor === 'email'"/)
  assert.match(source, /v-if="editor === 'password'"/)
  assert.match(source, /v-if="editor === 'google' && passwordAvailable"/)
  assert.match(source, /v-if="editor === 'delete'"/)
  assert.match(source, /<AccountDeletionWarning \/>/)
  assert.match(source, /confirmation\.value !== 'SUPPRIMER'/)
  assert.match(source, /useAccountSecrets\(/)
  assert.ok(
    source.indexOf('id="account-sessions"') <
      source.indexOf('id="account-delete"'),
  )
  const shell = await readFile(
    new URL('../app/components/AccountShell.vue', import.meta.url),
    'utf8',
  )
  assert.match(shell, /session\?\.account && !hideLogout/)
})

test('overview summary typography and logout actions omit redundant consequence copy', async () => {
  assert.doesNotMatch(source, /Nom d’utilisateur définitif\./)
  assert.match(
    source,
    /<dd class="mt-1 break-words">\{\{ details\.username \}\}<\/dd>/,
  )
  assert.match(source, /passwordAvailable \? 'Défini' : 'Non défini'/)
  assert.match(
    source,
    /id="account-delete" class="account-heading">Suppression<\/h2>/,
  )
  assert.match(source, /<GoogleIcon\b[^>]*\/>/)
  assert.doesNotMatch(source, /Vos cinémas|Le nom d’utilisateur est définitif/)
  assert.match(source, /grid-cols-\[minmax\(0,1fr\)_auto\] items-baseline/)
  assert.match(source, /<dt class="overview-label">Email du compte<\/dt>/)
  assert.match(source, /<label for="new-email" class="account-label">/)
  const buttons = [...source.matchAll(/<button\b[^>]*>[\s\S]*?<\/button>/g)]
  const local = buttons.find(([button]) =>
    button.includes('@click="logout"'),
  )?.[0]
  const global = buttons.find(([button]) =>
    button.includes('@click="logoutAll"'),
  )?.[0]
  assert.ok(local && global)
  for (const button of [local, global])
    assert.match(button, /class="account-secondary overview-secondary"/)
  assert.doesNotMatch(local, /aria-describedby/)
  assert.doesNotMatch(global, /aria-describedby/)
  assert.match(global, /Déconnecter tous les appareils/)
  assert.doesNotMatch(
    source,
    /logout-all-consequence|Vous serez aussi déconnecté de cet appareil\./,
  )
  const shell = await readFile(
    new URL('../app/components/AccountShell.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    shell,
    /'account-area-inner mx-auto max-w-\[60rem\]': accountArea/,
  )
  assert.match(
    shell,
    /\.account-overview :deep\(\.overview-link\) \{\s*@apply min-w-11 font-sans text-sm font-semibold;/,
  )
})

test('account area is opt-in, with one current route and disabled future categories', async () => {
  const shell = await readFile(
    new URL('../app/components/AccountShell.vue', import.meta.url),
    'utf8',
  )
  const navigation = await readFile(
    new URL('../app/components/AccountAreaNavigation.vue', import.meta.url),
    'utf8',
  )
  assert.match(source, /\saccount-area\s/)
  assert.match(source, /title="Paramètres"/)
  assert.match(source, /<dl class="space-y-2 text-sm">/)
  assert.match(source, /\[id\^="editor-"\] \{\s*@apply max-w-lg;/)
  assert.match(shell, /lg:px-12 lg:py-10/)
  assert.match(navigation, /lg:py-10/)
  assert.match(navigation, /Paramètres\s*<\/span>\s*<\/NuxtLink>/)
  assert.match(navigation, /text-white no-underline/)
  const header = await readFile(
    new URL('../app/components/AppHeader.vue', import.meta.url),
    'utf8',
  )
  assert.match(header, /\? 'Mon compte' : 'Connexion'/)
  assert.match(shell, /accountArea\?: boolean/)
  assert.match(shell, /<AccountAreaNavigation v-if="accountArea"/)
  assert.match(shell, /min-h-svh/)
  assert.match(shell, /lg:grid-cols-\[15rem_minmax\(0,1fr\)\]/)
  const root = await readFile(
    new URL('../app/app.vue', import.meta.url),
    'utf8',
  )
  assert.match(root, /noindex, nofollow/)
  assert.match(root, /no-referrer/)
  assert.match(shell, /status === 'ready' && session\?\.enabled/)
  assert.match(navigation, /aria-label="Espace personnel"/)
  assert.match(
    navigation,
    /to="\/compte"\s+:prefetch="false"\s+aria-current="page"/,
  )
  assert.match(navigation, /\{ label: 'Watchlist', icon: Bookmark \}/)
  assert.match(navigation, /\{ label: 'Amis', icon: Users \}/)
  assert.match(navigation, /v-for="entry in upcomingEntries"/)
  assert.doesNotMatch(navigation, /Films aimés/)
  assert.match(navigation, /<button\s+type="button"\s+disabled/)
  assert.match(navigation, /À venir/)
  assert.equal([...navigation.matchAll(/to=/g)].length, 1)
  assert.doesNotMatch(navigation, /@click|tabindex|href=/)
  for (const page of [
    'connexion',
    'inscription',
    'verification',
    'finaliser',
    'mot-de-passe-oublie',
    'reinitialiser-mot-de-passe',
    'compte/confirmer-email',
    'compte/confirmer-identite',
  ]) {
    const route = await readFile(
      new URL(`../app/pages/${page}.vue`, import.meta.url),
      'utf8',
    )
    assert.doesNotMatch(route, /\saccount-area\s|account-shell-area/)
  }
})

test('account navigation pairs each label with a decorative icon without changing responsive layout', async () => {
  const navigation = await readFile(
    new URL('../app/components/AccountAreaNavigation.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    navigation,
    /import \{ Bookmark, Settings, Users \} from '@lucide\/vue'/,
  )
  assert.match(
    navigation,
    /<span class="inline-flex items-center gap-2">\s*<Settings :size="18" class="shrink-0" aria-hidden="true" \/>\s*Paramètres/,
  )
  assert.match(
    navigation,
    /<span class="inline-flex items-center gap-2">\s*<component\s+:is="entry.icon"\s+:size="18"\s+class="shrink-0"\s+aria-hidden="true"\s*\/>\s*\{\{ entry.label \}\}\s*<\/span>\s*<span class="text-xs">À venir<\/span>/,
  )
  assert.match(navigation, /grid grid-cols-2 gap-2 lg:grid-cols-1/)
  assert.match(navigation, /flex-col items-start justify-between gap-x-2/)
  assert.match(navigation, /lg:flex-row lg:items-center/)
})
