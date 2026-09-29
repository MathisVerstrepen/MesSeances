import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import {
  sortWatchlistTags,
  tagNameError,
  watchlistTagPalette,
  watchlistTagStyle,
} from '../app/utils/watchlistTags.ts'
import type { WatchlistTag, WatchlistTagColor } from '../app/types/watchlist.ts'

function luminance(hex: string) {
  const channels = [1, 3, 5].map((start) => {
    const channel = Number.parseInt(hex.slice(start, start + 2), 16) / 255
    return channel <= 0.04045
      ? channel / 12.92
      : ((channel + 0.055) / 1.055) ** 2.4
  })
  return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722
}

function contrast(a: string, b: string) {
  const pair = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (pair[0]! + 0.05) / (pair[1]! + 0.05)
}

test('fixed palette keys, labels and opaque tokens satisfy text and boundary contrast', () => {
  assert.deepEqual(Object.keys(watchlistTagPalette), [
    'neutral',
    'red',
    'amber',
    'green',
    'teal',
    'blue',
    'violet',
    'rose',
  ])
  assert.deepEqual(
    Object.values(watchlistTagPalette).map((token) => token.label),
    ['Neutre', 'Rouge', 'Ambre', 'Vert', 'Sarcelle', 'Bleu', 'Violet', 'Rose'],
  )
  // SAFETY: The static palette has exactly the WatchlistTagColor keys asserted above.
  for (const key of Object.keys(watchlistTagPalette) as WatchlistTagColor[]) {
    const token = watchlistTagPalette[key]
    assert.deepEqual(watchlistTagStyle(key), {
      backgroundColor: token.backgroundColor,
      color: token.color,
      borderColor: token.borderColor,
    })
    for (const color of [token.backgroundColor, token.color, token.borderColor])
      assert.match(color, /^#[0-9a-f]{6}$/)
    assert.ok(
      contrast(token.color, token.backgroundColor) >= 4.5,
      `${key} text`,
    )
    assert.ok(
      contrast(token.borderColor, token.backgroundColor) >= 3,
      `${key} fill boundary`,
    )
    assert.ok(
      contrast(token.borderColor, '#ffffff') >= 3,
      `${key} white boundary`,
    )
  }
  assert.ok(contrast('#27272a', '#ffffff') >= 3)
})

test('picker preserves native radio keyboard and forced color behavior without private persistence', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistTagColorPicker.vue', import.meta.url),
    'utf8',
  )
  assert.match(source, /<fieldset/)
  assert.match(source, /<legend[^>]*>Couleur<\/legend>/)
  assert.match(source, /type="radio"/)
  assert.match(source, /useId\(\)/)
  assert.match(source, /min-h-11/)
  assert.match(source, /focus-visible/)
  assert.match(source, /:name="groupName"/)
  assert.match(source, /has-\[:checked\]:font-semibold/)
  assert.match(source, /grid-cols-2.*sm:grid-cols-4/)
  assert.doesNotMatch(
    source,
    /appearance-none|forced-color-adjust|keydown|localStorage|sessionStorage|useState|useRoute/,
  )
})

test('tag display sorts French names with numeric IDs without floating point loss', () => {
  const tags: WatchlistTag[] = [
    { id: '9007199254740993', name: 'Tag 2', color: 'rose' },
    { id: '2', name: 'Tag 10', color: 'neutral' },
    { id: '9007199254740992', name: 'tag 2', color: 'blue' },
    { id: '1', name: 'Été', color: 'amber' },
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
  contextTagId?: string
  tags: WatchlistTag[]
  open: boolean
  blocked: boolean
  error: string
}

interface CheckboxHandlers {
  assignedTags: ReturnType<typeof computed<WatchlistTag[]>>
  change: (event: { target: unknown }, id: string) => void
  close: (restore?: boolean) => void
  positionPanel: () => void
  position: ReturnType<typeof ref<Record<string, string>>>
}

interface Manager {
  open: ReturnType<typeof ref<boolean>>
  creating: ReturnType<typeof ref<boolean>>
  openCreate: () => void
  cancelCreate: () => void
  cancelAndFocus: () => void
  openModal: () => Promise<void>
  closeModal: () => void
  name: ReturnType<typeof ref<string>>
  color: ReturnType<typeof ref<WatchlistTagColor>>
  editDraft: ReturnType<typeof ref<string>>
  editColor: ReturnType<typeof ref<WatchlistTagColor>>
  target: ReturnType<
    typeof ref<{
      id: string
      name: string
      color: WatchlistTagColor
      action: string
    } | null>
  >
  pending: ReturnType<typeof ref<string | null>>
  create: () => Promise<void>
  edit: (
    tag: WatchlistTag,
    action: 'update' | 'delete',
    event?: { currentTarget: unknown },
  ) => void
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
  const props = reactive<{
    tags: WatchlistTag[]
    ready: boolean
    blocked: boolean
  }>({
    tags: [{ id: '1', name: 'Amis', color: 'neutral' }],
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
  class Button {
    isConnected = true
    disabled = false
    readonly name: string
    constructor(name: string) {
      this.name = name
    }
    matches() {
      return this.disabled
    }
    focus() {
      focusCalls.push(this.name)
    }
  }
  const rowAction = new Button('row')
  const templateRefs = {
    dialog: ref({
      showModal: () => {
        shown = true
      },
      close: () => {
        shown = false
      },
      querySelector: (selector: string) => new Button(selector),
    }),
    closeButton: ref(new Button('close')),
    createButton: ref(new Button('create')),
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
        `${script}\nexport { name, color, editDraft, editColor, target, pending, create, edit, submitTarget, cancel, open, openModal, closeModal, creating, openCreate, cancelCreate, cancelAndFocus }`,
        {
          compilerOptions: {
            module: ts.ModuleKind.CommonJS,
            target: ts.ScriptTarget.ES2022,
          },
        },
      ).outputText,
      {
        exports,
        HTMLButtonElement: Button,
        ref,
        computed,
        watch,
        nextTick,
        defineProps: () => props,
        require: () => ({ sortWatchlistTags, tagNameError, watchlistTagStyle }),
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
          createTag: (name: string, color: string) => {
            writes.push(['create', name, color])
            return write()
          },
          updateTag: (id: string, name: string, color: string) => {
            writes.push(['update', id, name, color])
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
    rowAction,
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
    assert.equal(f.manager.creating.value, false)
    assert.equal(f.manager.target.value, null)
    assert.deepEqual(f.focusCalls, ['close'])
    f.manager.name.value = 'Brouillon'
    f.manager.color.value = 'blue'
    f.manager.closeModal()
    await nextTick()
    assert.equal(f.shown(), false)
    assert.equal(f.manager.open.value, false)
    assert.equal(f.manager.name.value, 'Brouillon')
    assert.equal(f.manager.color.value, 'blue')
    assert.deepEqual(f.focusCalls, ['close', 'trigger'])
  } finally {
    f.stop()
  }
})

test('manager expands one region, focuses it and restores connected openers on cancellation', async () => {
  const f = await managerFixture()
  try {
    await f.manager.openModal()
    f.manager.openCreate()
    await nextTick()
    assert.equal(f.manager.creating.value, true)
    assert.equal(f.manager.target.value, null)
    assert.equal(f.focusCalls.at(-1), '#watchlist-tag-name')
    f.manager.name.value = 'Brouillon'
    f.manager.color.value = 'blue'
    f.manager.cancelCreate()
    await nextTick()
    assert.equal(f.manager.creating.value, false)
    assert.equal(f.focusCalls.at(-1), 'create')
    f.manager.openCreate()
    assert.equal(f.manager.name.value, 'Brouillon')
    assert.equal(f.manager.color.value, 'blue')
    for (const action of ['update', 'delete'] as const) {
      f.manager.edit(f.props.tags[0]!, action, { currentTarget: f.rowAction })
      await nextTick()
      assert.equal(f.manager.creating.value, false)
      assert.equal(f.manager.target.value?.action, action)
      assert.equal(
        f.focusCalls.at(-1),
        action === 'update'
          ? '#watchlist-tag-edit'
          : '#watchlist-tag-delete-cancel',
      )
      f.manager.cancelAndFocus()
      await nextTick()
      assert.equal(f.manager.target.value, null)
      assert.equal(f.focusCalls.at(-1), 'row')
      f.manager.edit(f.props.tags[0]!, action)
      f.manager.openCreate()
      assert.equal(f.manager.target.value, null)
      assert.equal(f.manager.creating.value, true)
    }
    f.manager.closeModal()
    await f.manager.openModal()
    assert.equal(f.manager.creating.value, false)
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.name.value, 'Brouillon')
    assert.equal(f.writes.length, 0)
  } finally {
    f.stop()
  }
})

test('scheduled region focus cannot cross newer action or privacy boundary', async () => {
  const f = await managerFixture()
  try {
    await f.manager.openModal()
    f.manager.openCreate()
    f.manager.edit(f.props.tags[0]!, 'update')
    await nextTick()
    assert.deepEqual(f.focusCalls, ['close', '#watchlist-tag-edit'])
    f.manager.openCreate()
    f.scopeKey.value++
    await nextTick()
    assert.deepEqual(f.focusCalls, ['close', '#watchlist-tag-edit'])
    assert.equal(f.manager.creating.value, false)
  } finally {
    f.stop()
  }
})

test('cancel during create fences late success and reopening remains list-first', async () => {
  const f = await managerFixture()
  try {
    await f.manager.openModal()
    f.manager.openCreate()
    f.manager.name.value = 'À garder'
    const held = deferred()
    f.setWrite(() => held.promise)
    const create = f.manager.create()
    f.manager.cancelCreate()
    held.resolve(true)
    await create
    assert.equal(f.manager.creating.value, false)
    assert.equal(f.manager.name.value, 'À garder')
    assert.equal(f.writes.length, 1)
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
    f.manager.openCreate()
    f.manager.name.value = 'Amis'
    f.manager.color.value = 'rose'
    f.setWrite(async () => false)
    await f.manager.create()
    assert.equal(f.manager.name.value, 'Amis')
    assert.equal(f.manager.color.value, 'rose')
    assert.equal(f.manager.creating.value, true)
    assert.deepEqual(f.writes[0], ['create', 'Amis', 'rose'])
    const held = deferred()
    f.setWrite(() => held.promise)
    const create = f.manager.create()
    f.manager.name.value = 'Nouvelle idée'
    held.resolve(true)
    await create
    assert.equal(f.manager.name.value, 'Nouvelle idée')
    assert.equal(f.manager.creating.value, true)
    f.setWrite(async () => true)
    await f.manager.create()
    assert.equal(f.manager.name.value, '')
    assert.equal(f.manager.color.value, 'neutral')
    assert.equal(f.manager.creating.value, false)
  } finally {
    f.stop()
  }
})

test('manager cancels externally renamed/deleted targets and handles own update separately', async () => {
  const f = await managerFixture()
  try {
    f.manager.edit(f.props.tags[0]!, 'update')
    f.manager.editDraft.value = 'Brouillon'
    f.props.tags = [{ id: '1', name: 'Autre appareil', color: 'neutral' }]
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.editDraft.value, 'Brouillon')
    f.manager.edit(f.props.tags[0]!, 'delete')
    f.props.tags = []
    assert.equal(f.manager.target.value, null)
    f.props.tags = [{ id: '1', name: 'Amis', color: 'neutral' }]
    f.manager.edit(f.props.tags[0]!, 'update')
    f.manager.editDraft.value = 'Ensemble'
    const held = deferred()
    f.setWrite(() => held.promise)
    const rename = f.manager.submitTarget()
    f.props.tags = [{ id: '1', name: 'Ensemble', color: 'neutral' }]
    assert.ok(f.manager.target.value)
    held.resolve(true)
    await rename
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.editDraft.value, '')
    f.manager.edit(f.props.tags[0]!, 'update')
    f.manager.editDraft.value = 'Incertain'
    f.setWrite(async () => {
      f.props.tags = [{ id: '1', name: 'Incertain', color: 'neutral' }]
      return false
    })
    await f.manager.submitTarget()
    assert.equal(f.manager.target.value, null)
    assert.equal(f.manager.editDraft.value, 'Incertain')
  } finally {
    f.stop()
  }
})

test('newer color-only create draft survives deferred success even after changing back', async () => {
  const f = await managerFixture()
  try {
    f.manager.openCreate()
    f.manager.name.value = 'Amis'
    f.manager.color.value = 'blue'
    const held = deferred()
    f.setWrite(() => held.promise)
    const create = f.manager.create()
    f.manager.color.value = 'rose'
    f.manager.color.value = 'blue'
    held.resolve(true)
    await create
    assert.equal(f.manager.name.value, 'Amis')
    assert.equal(f.manager.color.value, 'blue')
    assert.equal(f.manager.creating.value, true)
  } finally {
    f.stop()
  }
})

test('own atomic update rebases newer color draft; remote recolor cancels stale target only', async () => {
  const f = await managerFixture()
  try {
    f.manager.edit(f.props.tags[0]!, 'update')
    f.manager.editDraft.value = 'Ensemble'
    f.manager.editColor.value = 'blue'
    const held = deferred()
    f.setWrite(() => held.promise)
    const update = f.manager.submitTarget()
    f.manager.editColor.value = 'rose'
    f.props.tags = [{ id: '1', name: 'Ensemble', color: 'blue' }]
    held.resolve(true)
    await update
    assert.deepEqual(f.writes[0], ['update', '1', 'Ensemble', 'blue'])
    assert.equal(f.manager.target.value?.color, 'blue')
    assert.equal(f.manager.editDraft.value, 'Ensemble')
    assert.equal(f.manager.editColor.value, 'rose')
    f.props.tags = [...f.props.tags, { id: '2', name: 'Autre', color: 'green' }]
    assert.ok(f.manager.target.value)
    f.props.tags = [{ id: '1', name: 'Ensemble', color: 'teal' }]
    assert.equal(f.manager.target.value, null)
    f.manager.edit(f.props.tags[0]!, 'delete')
    f.props.tags = [{ id: '1', name: 'Ensemble', color: 'amber' }]
    assert.equal(f.manager.target.value, null)
  } finally {
    f.stop()
  }
})

test('uncertain update retains pair only while committed baseline stays unchanged and never replays', async () => {
  const f = await managerFixture()
  try {
    f.manager.edit(f.props.tags[0]!, 'update')
    f.manager.editDraft.value = 'Ensemble'
    f.manager.editColor.value = 'green'
    f.setWrite(async () => false)
    await f.manager.submitTarget()
    assert.equal(f.manager.target.value?.color, 'neutral')
    assert.equal(f.manager.editDraft.value, 'Ensemble')
    assert.equal(f.manager.editColor.value, 'green')
    f.setWrite(async () => {
      f.props.tags = [{ id: '1', name: 'Amis', color: 'green' }]
      return false
    })
    await f.manager.submitTarget()
    assert.equal(f.manager.target.value, null)
    assert.equal(f.writes.length, 2)
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
      f.manager.color.value = 'violet'
      f.manager.edit(f.props.tags[0]!, 'update')
      f.manager.editDraft.value = 'Brouillon privé'
      f.manager.editColor.value = 'rose'
      const held = deferred()
      f.setWrite(() => held.promise)
      const save = f.manager.submitTarget()
      if (boundary === 'scope') f.scopeKey.value++
      else f.pagehide()
      assert.equal(f.manager.name.value, '')
      assert.equal(f.manager.editDraft.value, '')
      assert.equal(f.manager.color.value, 'neutral')
      assert.equal(f.manager.editColor.value, 'neutral')
      assert.equal(f.manager.target.value, null)
      assert.equal(f.manager.creating.value, false)
      assert.equal(f.manager.pending.value, null)
      f.manager.editDraft.value = 'Nouvelle saisie'
      held.resolve(true)
      await save
      assert.equal(f.manager.editDraft.value, 'Nouvelle saisie')
    } finally {
      f.stop()
    }
  })
}

async function itemFixture() {
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
  const props = reactive<CheckboxProps>({
    tagIds: [],
    tags: [],
    open: false,
    blocked: false,
    error: '',
  })
  const calls: unknown[][] = []
  const input = new Input()
  const focused: string[] = []
  const scope = effectScope()
  let unmount = () => {}
  const viewport = {
    offsetLeft: 0,
    offsetTop: 0,
    width: 1440,
    height: 900,
    addEventListener() {},
    removeEventListener() {},
  }
  const anchor = { left: 400, top: 600, bottom: 644 }
  const templateRefs = {
    trigger: ref({
      isConnected: true,
      focus: () => focused.push('trigger'),
      getBoundingClientRect: () => anchor,
    }),
    panel: ref({
      style: { width: '' },
      querySelector: (selector: string) => ({
        focus: () => focused.push(selector),
      }),
    }),
    options: ref({ scrollHeight: 400 }),
    heading: ref({ offsetHeight: 56 }),
    root: ref(null),
  }
  // SAFETY: Only these exact component exports are appended below.
  const exports = {} as CheckboxHandlers
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${script}\nexport { change, close, positionPanel, position, assignedTags }`,
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
        ref,
        watch,
        nextTick,
        onBeforeUnmount: (callback: () => void) => {
          unmount = callback
        },
        HTMLInputElement: Input,
        defineProps: () => props,
        defineEmits:
          () =>
          (...args: unknown[]) => {
            calls.push(args)
            if (args[0] === 'assign')
              assert.equal(
                input.checked,
                props.tagIds.includes(String(args[1])),
              )
            if (args[0] === 'close') props.open = false
          },
        useId: () => 'editor',
        useTemplateRef: (name: keyof typeof templateRefs) => templateRefs[name],
        document: { addEventListener() {}, removeEventListener() {} },
        window: {
          visualViewport: viewport,
          addEventListener() {},
          removeEventListener() {},
        },
      },
    ),
  )
  return {
    source,
    props,
    calls,
    input,
    handlers: exports,
    focused,
    viewport,
    anchor,
    templateRefs,
    stop: () => {
      unmount()
      scope.stop()
    },
  }
}

test('section context hides only its assigned chip without altering picker membership or list summaries', async () => {
  const f = await itemFixture()
  try {
    f.props.tags = [
      { id: '1', name: 'Amis', color: 'blue' },
      { id: '2', name: 'Cinéma', color: 'red' },
      { id: '3', name: 'Vide', color: 'neutral' },
    ]
    f.props.tagIds = ['1', '2']
    const visibleIds = () => f.handlers.assignedTags.value.map((tag) => tag.id)
    assert.deepEqual(visibleIds(), ['1', '2'])
    f.props.contextTagId = '1'
    assert.deepEqual(visibleIds(), ['2'])
    f.props.open = true
    f.input.checked = false
    f.handlers.change({ target: f.input }, '1')
    assert.equal(f.input.checked, true)
    assert.deepEqual(f.calls[0]?.slice(0, 3), ['assign', '1', false])
    assert.deepEqual(f.props.tagIds, ['1', '2'])
    assert.match(f.source, /:checked="tagIds.includes\(tag.id\)"/)
    f.props.contextTagId = '2'
    assert.deepEqual(visibleIds(), ['1'])
    f.props.tagIds = ['2']
    assert.deepEqual(visibleIds(), [])
    f.props.contextTagId = undefined
    assert.deepEqual(visibleIds(), ['2'])
    f.props.tagIds = []
    assert.deepEqual(visibleIds(), [])
  } finally {
    f.stop()
  }
})

test('native tag checkbox restores committed value synchronously before emitting one assignment and rejects pending or closed actions', async () => {
  const f = await itemFixture()
  try {
    const { props, input, calls, source, handlers } = f
    props.open = true
    handlers.change({ target: input }, '9007199254740993')
    assert.equal(input.checked, false)
    assert.equal(calls.length, 1)
    assert.deepEqual(calls[0]?.slice(0, 3), [
      'assign',
      '9007199254740993',
      true,
    ])
    props.tagIds = ['9007199254740993']
    input.checked = false
    handlers.change({ target: input }, '9007199254740993')
    assert.equal(input.checked, true)
    assert.deepEqual(calls[1]?.slice(0, 3), [
      'assign',
      '9007199254740993',
      false,
    ])
    props.blocked = true
    handlers.change({ target: input }, '9007199254740993')
    props.blocked = false
    props.open = false
    handlers.change({ target: input }, '9007199254740993')
    assert.equal(calls.length, 2)
    assert.match(source, /v-if="open"/)
    assert.doesNotMatch(
      source,
      /v-html|localStorage|sessionStorage|useState|useRoute/,
    )
  } finally {
    f.stop()
  }
})

test('compact picker focuses first choice, flips above bottom rows and clamps narrow visual viewports', async () => {
  const f = await itemFixture()
  try {
    f.props.open = true
    await new Promise<void>((resolve) => setImmediate(resolve))
    assert.deepEqual(f.focused, ['input:not(:disabled)'])
    assert.equal(f.handlers.position.value.top, '272px')
    assert.equal(f.handlers.position.value.width, '320px')
    f.viewport.width = 320
    f.viewport.height = 250
    f.viewport.offsetTop = 40
    f.viewport.offsetLeft = 10
    f.anchor.left = 300
    f.handlers.positionPanel()
    assert.deepEqual(
      { ...f.handlers.position.value },
      { left: '18px', top: '48px', width: '304px', maxHeight: '234px' },
    )
    f.handlers.close(true)
    assert.equal(f.props.open, false)
    assert.equal(f.focused.at(-1), 'trigger')
  } finally {
    f.stop()
  }
})

test('closed or unmounted picker fences queued opening focus; outside close does not restore focus', async () => {
  const f = await itemFixture()
  f.props.open = true
  await nextTick()
  f.handlers.close()
  await nextTick()
  assert.deepEqual(f.focused, [])
  f.props.open = true
  await nextTick()
  f.stop()
  await nextTick()
  assert.deepEqual(f.focused, [])
})

test('picker retains committed rows during readback, local errors refresh without replay, and page scope clears picker', async () => {
  const page = await readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  )
  assert.match(page, /v-else-if="ready \|\| openTagEditor"/)
  assert.match(page, /@retry="watchlist.retry"/)
  assert.match(
    page,
    /watch\(watchlist.scopeKey, clearPageSearch, \{ flush: 'sync' \}\)/,
  )
  assert.match(page, /onBeforeRouteLeave\(clearPageSearch\)/)
  assert.match(page, /window.addEventListener\('pagehide', clearPageSearch\)/)
  assert.match(page, /scope !== tagScope.value/)
  assert.match(page, /interaction !== tagInteraction/)
})
