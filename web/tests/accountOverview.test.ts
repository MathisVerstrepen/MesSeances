import assert from 'node:assert/strict'
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
  const focused: string[] = []
  const calls: string[] = []
  const initialDetails = { ...owner.account, allowed_methods: ['password'] }
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
  const result: ScriptExports = {}
  const script = source
    .split('<script setup lang="ts">')[1]!
    .split('</script>')[0]!
  const compiled = ts.transpileModule(
    `${script.replaceAll('import.meta.client', 'true')}\nexports.state = { ${names.join(',')}, toggleEditor, googleProof, changePassword, clearSecrets, logout }`,
    {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    },
  ).outputText
  scope.run(() =>
    runInNewContext(compiled, {
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
        notify: () => calls.push('notify'),
        refresh: async () => calls.push('refresh'),
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
    owner,
    calls,
    focused,
    details,
    document,
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

test('transient same-account refresh preserves input, changed/revoked/error sessions clear it', async () => {
  for (const outcome of ['same', 'changed', 'revoked', 'error']) {
    const f = fixture()
    try {
      await f.state.toggleEditor('delete')
      f.state.deletionPassword.value = 'secret'
      f.state.confirmation.value = 'SUPPRIMER'
      f.status.value = 'loading'
      f.session.value = null
      await nextTick()
      assert.equal(f.state.deletionPassword.value, 'secret')
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

test('uncertain write revalidates once without replay and keeps owning error visible', async () => {
  const f = fixture()
  try {
    let writes = 0
    f.setWrite(async () => {
      writes++
      throw new accountState.AccountApiError(0, 'network_error')
    })
    await f.state.toggleEditor('password')
    f.state.newPassword.value = 'LongPassword123!'
    await f.state.changePassword()
    assert.equal(writes, 1)
    assert.equal(f.state.editor.value, 'password')
    assert.ok(f.state.passwordError.value)
    assert.equal(f.state.newPassword.value, '')
    assert.deepEqual(f.calls, ['notify', 'refresh'])
  } finally {
    f.stop()
  }
})

test('local logout reuses account.logout and full document navigation', async () => {
  const f = fixture()
  try {
    await f.state.logout()
    assert.deepEqual(f.calls, ['logout', '/connexion'])
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
