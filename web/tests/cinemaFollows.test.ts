import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { runInNewContext } from 'node:vm'
import ts from 'typescript'
import { computed, nextTick, reactive, ref } from 'vue'
import { parse } from '@vue/compiler-sfc'
import { accountDestination } from '../app/utils/accountState.ts'
import { AccountApiError } from '../app/utils/accountState.ts'
import {
  deferred,
  fixture,
  session,
  settle,
  snapshot,
} from './helpers/cinemaFollowsHarness.ts'

interface FollowClickEvent {
  detail: number
  currentTarget: {
    isConnected: boolean
    disabled: boolean
    getClientRects: () => number[]
    focus: () => void
  }
}
interface FollowButtonExports {
  toggle?: (event: FollowClickEvent) => Promise<void>
}

test('follow snapshot is singleton client memory, no SSR read, storage or selection fallback', async () => {
  const f = await fixture(false)
  try {
    f.admit()
    await settle()
    assert.equal(f.gets, 0)
    assert.equal(f.another(), f.follows)
    assert.equal(await f.follows.save('cinema-a', true), '')
    assert.equal(f.posts.length, 0)
    assert.ok([...f.states.keys()].every((key) => !key.includes('follow')))
    const source = await readFile(
      new URL('../app/composables/useCinemaFollows.ts', import.meta.url),
      'utf8',
    )
    assert.doesNotMatch(
      source,
      /useState|useAsyncData|localStorage|sessionStorage|useCinemaPreferences|saveTheaterPreferences/,
    )
  } finally {
    f.stop()
  }
})

test('only complete enabled owners read; shared startup reads once and committed toggles serialize', async () => {
  const f = await fixture()
  try {
    for (const next of [
      { enabled: false, state: 'anonymous', account: null },
      { enabled: true, state: 'anonymous', account: null },
      { ...session(), state: 'pending_username' },
    ] as const) {
      f.admit(next)
      await settle()
      assert.equal(f.gets, 0)
    }
    f.admit()
    f.another().startSynchronization()
    await settle()
    assert.equal(f.gets, 1)
    const held = deferred<ReturnType<typeof snapshot>>()
    f.setWrite(() => held.promise)
    const save = f.follows.save('cinema-a', true)
    assert.equal(f.follows.ids.value.has('cinema-a'), false)
    assert.equal(f.follows.saving.value, true)
    await f.follows.save('cinema-b', true)
    assert.equal(f.posts.length, 1)
    held.resolve(snapshot('1', ['cinema-a']))
    await save
    assert.equal(f.follows.ids.value.has('cinema-a'), true)
    assert.deepEqual(f.messages, ['theater-follows-changed'])
    assert.deepEqual(
      { ...f.posts[0] },
      {
        expected_username: 'alice',
        expected_revision: '0',
        theater_id: 'cinema-a',
        followed: 'true',
      },
    )
    f.setWrite(async () => snapshot('2'))
    await f.follows.save('cinema-a', false)
    assert.equal(f.follows.ids.value.size, 0)
  } finally {
    f.stop()
  }
})

for (const status of [0, 409, 503]) {
  test(`follow ${status} reconciles GET without replay; failed reconciliation gates until explicit recovery`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      f.setWrite(async () => {
        f.setSnapshot(snapshot('2', ['cinema-b']))
        throw new AccountApiError(
          status,
          status === 409 ? 'theater_follows_changed' : '',
        )
      })
      assert.ok(await f.follows.save('cinema-a', true))
      assert.equal(f.follows.ready.value, true)
      assert.deepEqual([...f.follows.ids.value], ['cinema-b'])
      assert.equal(f.posts.length, 1)
      f.setRead(async () => {
        throw new AccountApiError(503)
      })
      await f.follows.save('cinema-a', true)
      assert.equal(f.follows.ready.value, false)
      await f.follows.save('cinema-a', true)
      assert.equal(f.posts.length, 2)
      f.setRead(async () => snapshot('3', ['cinema-a']))
      await f.follows.retry()
      assert.equal(f.follows.ready.value, true)
      assert.equal(f.posts.length, 2)
    } finally {
      f.stop()
    }
  })
}

for (const transition of [
  'logout',
  'owner',
  'same-owner',
  'offline',
  'pagehide',
]) {
  test(`late follow completion fenced after ${transition}`, async () => {
    const f = await fixture()
    try {
      f.admit()
      await settle()
      const held = deferred<ReturnType<typeof snapshot>>()
      f.setWrite(() => held.promise)
      const pending = f.follows.save('private-cinema', true)
      if (transition === 'owner' || transition === 'same-owner') {
        f.setSnapshot(
          snapshot('0', [], transition === 'owner' ? 'bob' : 'alice'),
        )
        f.admit(session(transition === 'owner' ? 'bob' : 'alice'))
      } else f.account.clear()
      await settle()
      held.resolve(snapshot('99', ['private-cinema']))
      await pending
      assert.equal(f.follows.ids.value.has('private-cinema'), false)
      assert.deepEqual(f.messages, [])
    } finally {
      f.stop()
    }
  })
}

test('same-owner revalidation holds writes and replaces snapshot in two phases without POST', async () => {
  const f = await fixture()
  try {
    f.admit()
    await settle()
    const held = deferred<ReturnType<typeof snapshot>>()
    f.setRead(() => held.promise)
    const refresh = f.account.revalidate()
    await settle()
    assert.equal(f.follows.writesBlocked.value, true)
    await f.follows.save('cinema-a', true)
    assert.equal(f.posts.length, 0)
    held.resolve(snapshot('8', ['cinema-b']))
    await refresh
    assert.equal(f.follows.revision.value, '8')
    assert.equal(f.follows.writesBlocked.value, false)
  } finally {
    f.stop()
  }
})

test('actual follow button does not show late feedback or focus on changed theater, route or owner', async () => {
  const source = await readFile(
    new URL('../app/components/CinemaFollowButton.vue', import.meta.url),
    'utf8',
  )
  for (const transition of ['theater', 'route', 'owner']) {
    const props = reactive({ theaterId: 'cinema-a' })
    const saving = deferred<string>()
    const owner = ref('alice')
    const feedback: string[] = []
    let valid = true
    let focuses = 0
    const exports: FollowButtonExports = {}
    runInNewContext(
      ts.transpileModule(
        `${parse(source).descriptor.scriptSetup!.content}\nexports.toggle = toggle`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        computed,
        nextTick,
        defineProps: () => props,
        defineEmits: () => (_event: string, message: string) =>
          feedback.push(message),
        useAccountSession: () => ({
          status: ref('ready'),
          session: ref(session()),
          writesBlocked: ref(false),
          revalidating: ref(false),
        }),
        useCinemaFollows: () => ({
          owner,
          ready: ref(true),
          ids: ref(new Set()),
          saving: ref(false),
          save: () => saving.promise,
        }),
        useAccountLifetime: () => ({
          capture: () => {
            const expected = owner.value
            return () => valid && owner.value === expected
          },
        }),
        require: () => ({ accountDestination }),
      },
    )
    const pending = exports.toggle!({
      detail: 0,
      currentTarget: {
        isConnected: true,
        disabled: false,
        getClientRects: () => [1],
        focus: () => focuses++,
      },
    })
    if (transition === 'theater') props.theaterId = 'cinema-b'
    else if (transition === 'owner') owner.value = 'bob'
    else valid = false
    saving.resolve('Old private failure')
    await pending
    assert.deepEqual(feedback, [''])
    assert.equal(focuses, 0)
  }
  assert.match(source, /:aria-pressed="known \? followed : undefined"/)
  assert.match(source, /session\.value\?\.enabled !== false/)
})
