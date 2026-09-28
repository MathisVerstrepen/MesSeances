import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import { sortWatchlistTags, tagNameError } from '../app/utils/watchlistTags.ts'
import type { WatchlistTag } from '../app/types/watchlist.ts'

test('tag display sorts French names with numeric IDs without floating point loss', () => {
  const tags = [
    { id: '9007199254740993', name: 'Tag 2' },
    { id: '2', name: 'Tag 10' },
    { id: '9007199254740992', name: 'tag 2' },
    { id: '1', name: 'Été' },
  ]
  assert.deepEqual(
    sortWatchlistTags(tags).map((tag) => tag.id),
    ['1', '9007199254740992', '9007199254740993', '2'],
  )
  assert.equal(tags[0]?.id, '9007199254740993')
})

test('tag input validates normalized code points without truncating or interpreting markup', () => {
  for (const name of [
    '',
    '   ',
    'a'.repeat(41),
    'A\nB',
    'A\tB',
    'A\u2028B',
    '\ud800',
  ])
    assert.ok(tagNameError(name))
  for (const name of [
    '🎬'.repeat(40),
    'e\u0301'.repeat(40),
    '  À   revoir  ',
    '<b>Amis</b>',
  ])
    assert.equal(tagNameError(name), '')
})

function deferred() {
  let resolve!: (success: boolean) => void
  const promise = new Promise<boolean>((yes) => {
    resolve = yes
  })
  return { promise, resolve }
}

interface CheckboxProps {
  tagIds: string[]
  tags: WatchlistTag[]
  open: boolean
}

interface CheckboxHandlers {
  change?: (event: { target: unknown }, id: string) => void
}

interface Manager {
  open: ReturnType<typeof ref<boolean>>
  openModal: () => Promise<void>
  closeModal: () => void
  name: ReturnType<typeof ref<string>>
  renameDraft: ReturnType<typeof ref<string>>
  target: ReturnType<
    typeof ref<{ id: string; name: string; action: string } | null>
  >
  pending: ReturnType<typeof ref<string | null>>
  create: () => Promise<void>
  edit: (tag: WatchlistTag, action: 'rename' | 'delete') => void
  submitTarget: () => Promise<void>
  cancel: () => void
}

async function managerFixture() {
  const source = await readFile(
    new URL('../app/components/WatchlistTagManager.vue', import.meta.url),
    'utf8',
  )
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )?.[1]
  assert.ok(script)
  const props = reactive({
    tags: [{ id: '1', name: 'Amis' }],
    ready: true,
    blocked: false,
  })
  const writes: string[][] = []
  let write = async () => true
  let unmount = () => {}
  let pagehide = () => {}
  const scopeKey = ref(0)
  const scope = effectScope()
  const focusCalls: string[] = []
  let shown = false
  const templateRefs = {
    dialog: ref({
      showModal: () => {
        shown = true
      },
      close: () => {
        shown = false
      },
    }),
    closeButton: ref({ focus: () => focusCalls.push('close') }),
    trigger: ref({
      isConnected: true,
      disabled: false,
      focus: () => focusCalls.push('trigger'),
    }),
  }
  // SAFETY: The exact exports are appended to the compiled component script below.
  const exports = {} as Manager
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${script}\nexport { name, renameDraft, target, pending, create, edit, submitTarget, cancel, open, openModal, closeModal }`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        ref,
        computed,
        watch,
        nextTick,
        defineProps: () => props,
        require: () => ({ sortWatchlistTags, tagNameError }),
        useTemplateRef: (name: keyof typeof templateRefs) => templateRefs[name],
        onBeforeUnmount: (callback: () => void) => {
          unmount = callback
        },
        onMounted: (callback: () => void) => callback(),
        window: {
          addEventListener: (_name: string, callback: () => void) => {
            pagehide = callback
          },
          removeEventListener: () => {},
        },
        document: {
          activeElement: null,
          addEventListener: () => {},
          removeEventListener: () => {},
        },
        useWatchlist: () => ({
          scopeKey,
          createTag: (name: string) => {
            writes.push(['create', name])
            return write()
          },
          renameTag: (id: string, name: string) => {
            writes.push(['rename', id, name])
            return write()
          },
          deleteTag: (id: string) => {
            writes.push(['delete', id])
            return write()
          },
        }),
      },
    ),
  )
  return {
    manager: exports,
    props,
    scopeKey,
    focusCalls,
    shown: () => shown,
    pagehide: () => pagehide(),
    writes,
    setWrite: (next: typeof write) => {
      write = next
    },
    stop: () => {
      unmount()
      scope.stop()
    },
  }
}

test('manager opens native modal with close focus and restores same-scope trigger without discarding draft', async () => {
  const f = await managerFixture()
  try {
    await f.manager.openModal()
    assert.equal(f.shown(), true)
    assert.deepEqual(f.focusCalls, ['close'])
    f.manager.name.value = 'Brouillon'
    f.manager.closeModal()
    await nextTick()
    assert.equal(f.shown(), false)
    assert.equal(f.manager.open.value, false)
    assert.equal(f.manager.name.value, 'Brouillon')
    assert.deepEqual(f.focusCalls, ['close', 'trigger'])
  } finally {
    f.stop()
  }
})

test('manager fences scheduled opening and trigger restoration on owner or page boundary', async () => {
  const f = await managerFixture()
  try {
    const opening = f.manager.openModal()
    f.scopeKey.value++
    await opening
    assert.equal(f.shown(), false)
    assert.equal(f.manager.open.value, false)
    assert.deepEqual(f.focusCalls, [])
    await f.manager.openModal()
    f.manager.closeModal()
    f.pagehide()
    await nextTick()
    assert.deepEqual(f.focusCalls, ['close'])
  } finally {
    f.stop()
  }
})

test('manager retains failed create draft, clears only unchanged successful draft and never clears newer input', async () => {
  const f = await managerFixture()
  try {
    f.manager.name.value = 'Amis'
    f.setWrite(async () => false)
    await f.manager.create()
    assert.equal(f.manager.name.value, 'Amis')
    const held = deferred()
    f.setWrite(() => held.promise)
    const create = f.manager.create()
    f.manager.name.value = 'Nouvelle idée'
    held.resolve(true)
    await create
    assert.equal(f.manager.name.value, 'Nouvelle idée')
    f.setWrite(async () => true)
    await f.manager.create()
    assert.equal(f.manager.name.value, '')
  } finally {
    f.stop()
  }
})

test('manager cancels externally renamed/deleted targets and handles own rename separately', async () => {
  const f = await managerFixture()
  try {
    f.manager.edit(f.props.tags[0]!, 'rename')
    f.manager.renameDraft.value = 'Brouillon'
    f.props.tags = [{ id: '1', name: 'Autre appareil' }]
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.renameDraft.value, 'Brouillon')
    f.manager.edit(f.props.tags[0]!, 'delete')
    f.props.tags = []
    assert.equal(f.manager.target.value, null)
    f.props.tags = [{ id: '1', name: 'Amis' }]
    f.manager.edit(f.props.tags[0]!, 'rename')
    f.manager.renameDraft.value = 'Ensemble'
    const held = deferred()
    f.setWrite(() => held.promise)
    const rename = f.manager.submitTarget()
    f.props.tags = [{ id: '1', name: 'Ensemble' }]
    assert.ok(f.manager.target.value)
    held.resolve(true)
    await rename
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.renameDraft.value, '')
    f.manager.edit(f.props.tags[0]!, 'rename')
    f.manager.renameDraft.value = 'Incertain'
    f.setWrite(async () => {
      f.props.tags = [{ id: '1', name: 'Incertain' }]
      return false
    })
    await f.manager.submitTarget()
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.renameDraft.value, 'Incertain')
  } finally {
    f.stop()
  }
})

test('unmount/privacy replacement erases drafts and fences deferred manager completion', async () => {
  const f = await managerFixture()
  f.manager.name.value = 'Ancien compte'
  const held = deferred()
  f.setWrite(() => held.promise)
  const create = f.manager.create()
  f.stop()
  assert.equal(f.manager.name.value, '')
  f.manager.name.value = 'Replacement simulation'
  held.resolve(true)
  await create
  assert.equal(f.manager.name.value, 'Replacement simulation')
})

for (const boundary of ['scope', 'pagehide'] as const) {
  test(`manager ${boundary} clears drafts synchronously and fences late completion`, async () => {
    const f = await managerFixture()
    try {
      f.manager.name.value = 'Privé'
      f.manager.edit(f.props.tags[0]!, 'rename')
      f.manager.renameDraft.value = 'Brouillon privé'
      const held = deferred()
      f.setWrite(() => held.promise)
      const save = f.manager.submitTarget()
      if (boundary === 'scope') f.scopeKey.value++
      else f.pagehide()
      assert.equal(f.manager.name.value, '')
      assert.equal(f.manager.renameDraft.value, '')
      assert.equal(f.manager.target.value, null)
      assert.equal(f.manager.pending.value, null)
      f.manager.renameDraft.value = 'Nouvelle saisie'
      held.resolve(true)
      await save
      assert.equal(f.manager.renameDraft.value, 'Nouvelle saisie')
    } finally {
      f.stop()
    }
  })
}

test('native tag checkbox restores committed value synchronously before emitting one assignment', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistItemTags.vue', import.meta.url),
    'utf8',
  )
  const script = source.match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )?.[1]
  assert.ok(script)
  class Input {
    checked = true
  }
  const props: CheckboxProps = { tagIds: [], tags: [], open: true }
  const calls: unknown[][] = []
  const exports: CheckboxHandlers = {}
  runInNewContext(
    ts.transpileModule(`${script}\nexport { change }`, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText,
    {
      exports,
      computed,
      HTMLInputElement: Input,
      defineProps: () => props,
      defineEmits:
        () =>
        (...args: unknown[]) => {
          calls.push(args)
          assert.equal(input.checked, false)
        },
      useId: () => 'editor',
      useTemplateRef: () => ref(null),
    },
  )
  const input = new Input()
  exports.change?.({ target: input }, '9007199254740993')
  assert.equal(input.checked, false)
  assert.equal(calls.length, 1)
  assert.deepEqual(calls[0]?.slice(0, 3), ['assign', '9007199254740993', true])
  assert.match(source, /v-if="open"/)
  assert.doesNotMatch(
    source,
    /v-html|localStorage|sessionStorage|useState|useRoute/,
  )
})
