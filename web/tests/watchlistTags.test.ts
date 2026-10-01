import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import {
  type Component,
  computed,
  createSSRApp,
  effectScope,
  nextTick,
  reactive,
  ref,
  useId,
  watch,
} from 'vue'
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

function hue(hex: string) {
  const r = Number.parseInt(hex.slice(1, 3), 16)
  const g = Number.parseInt(hex.slice(3, 5), 16)
  const b = Number.parseInt(hex.slice(5, 7), 16)
  const max = Math.max(r, g, b)
  const range = max - Math.min(r, g, b)
  assert.ok(range > 0)
  const sector =
    max === r
      ? (g - b) / range
      : max === g
        ? (b - r) / range + 2
        : (r - g) / range + 4
  return (sector * 60 + 360) % 360
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

test('red/pink and leaf-green/cyan-teal retain distinct surface, text and border hues', () => {
  for (const [a, b, min] of [
    ['red', 'rose', 20],
    ['green', 'teal', 45],
  ] as const) {
    for (const role of ['backgroundColor', 'color', 'borderColor'] as const) {
      const difference = Math.abs(
        hue(watchlistTagPalette[a][role]) - hue(watchlistTagPalette[b][role]),
      )
      assert.ok(
        Math.min(difference, 360 - difference) >=
          (role === 'backgroundColor' ? Math.max(30, min) : min),
        `${a}/${b} ${role} hue separation`,
      )
    }
  }
  for (const [key, min, max] of [
    ['rose', 310, 345],
    ['green', 80, 125],
    ['teal', 175, 200],
  ] as const) {
    const surfaceHue = hue(watchlistTagPalette[key].backgroundColor)
    assert.ok(
      surfaceHue >= min && surfaceHue <= max,
      `${key} named surface hue`,
    )
  }
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
  assert.match(source, /class="sr-only focus-visible:ring-0"/)
  assert.match(source, /rounded-none border-2/)
  assert.match(source, /font-mono text-center text-xs font-bold/)
  assert.match(source, /grid-cols-\[20px_minmax\(0,1fr\)_20px\]/)
  assert.match(source, /justify-self-center/)
  assert.match(source, /has-\[:focus-visible\]:outline-ink/)
  assert.match(source, /v-if="modelValue === color"/)
  assert.match(source, /@change="emit\('update:modelValue', color\)"/)
  assert.match(source, /grid-cols-2.*sm:grid-cols-4/)
  assert.doesNotMatch(
    source,
    /appearance-none|forced-color-adjust|keydown|localStorage|sessionStorage|useState|useRoute/,
  )
})

test('editorial color tiles retain eight named native radios and one decorative selected check for every color', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistTagColorPicker.vue', import.meta.url),
    'utf8',
  )
  const { descriptor } = parse(source)
  const pickerModule: { default?: Component } = {}
  const require = createRequire(import.meta.url)
  runInNewContext(
    ts.transpileModule(
      compileScript(descriptor, {
        id: 'WatchlistTagColorPicker',
        inlineTemplate: true,
      }).content,
      {
        compilerOptions: {
          module: ts.ModuleKind.CommonJS,
          target: ts.ScriptTarget.ES2022,
        },
      },
    ).outputText,
    {
      exports: pickerModule,
      useId,
      require: (id: string) =>
        id === '~/utils/watchlistTags'
          ? { watchlistTagPalette, watchlistTagStyle }
          : require(id),
    },
  )
  assert.ok(pickerModule.default)
  for (const color of Object.keys(watchlistTagPalette) as WatchlistTagColor[]) {
    const html = await renderToString(
      createSSRApp(pickerModule.default, { modelValue: color }),
    )
    const labels = html.match(/<label\b[\s\S]*?<\/label>/g) ?? []
    assert.equal(labels.length, 8)
    assert.equal((html.match(/<svg\b/g) ?? []).length, 1)
    const names = new Set<string>()
    for (const [index, tile] of labels.entries()) {
      const key = Object.keys(watchlistTagPalette)[index] as WatchlistTagColor
      const token = watchlistTagPalette[key]
      const radio = tile.match(/<input\b[^>]*>/)?.[0]
      assert.ok(radio)
      names.add(radio.match(/name="([^"]+)"/)![1]!)
      assert.match(radio, /type="radio"/)
      assert.match(radio, /class="sr-only focus-visible:ring-0"/)
      assert.match(radio, new RegExp(`value="${key}"`))
      assert.equal(/ checked(?:=""|(?=[\s>]))/.test(radio), key === color)
      assert.equal(tile.includes('<svg'), key === color)
      assert.ok(tile.includes(token.label))
      for (const value of [
        token.backgroundColor,
        token.color,
        token.borderColor,
      ])
        assert.ok(tile.includes(value))
      if (key === color) {
        assert.match(tile, /aria-hidden="true"/)
        assert.match(tile, /focusable="false"/)
      }
    }
    assert.equal(names.size, 1)
  }
})

test('manager, assigned and assignment-option chips share square editorial typography without changing palette bindings', async () => {
  const [css, manager, item] = await Promise.all([
    readFile(new URL('../app/assets/css/main.css', import.meta.url), 'utf8'),
    readFile(
      new URL('../app/components/WatchlistTagManager.vue', import.meta.url),
      'utf8',
    ),
    readFile(
      new URL('../app/components/WatchlistItemTags.vue', import.meta.url),
      'utf8',
    ),
  ])
  const chip = css.match(/\.watchlist-tag-chip\s*\{([^}]+)\}/)?.[1]
  assert.ok(chip)
  assert.match(chip, /max-w-full min-w-0 rounded-none border-2/)
  assert.match(chip, /font-mono text-xs font-bold/)
  assert.match(chip, /\[overflow-wrap:anywhere\]/)
  assert.equal((manager.match(/class="watchlist-tag-chip"/g) ?? []).length, 1)
  assert.equal((item.match(/class="watchlist-tag-chip"/g) ?? []).length, 2)
  assert.equal(
    (manager.match(/:style="watchlistTagStyle\(tag.color\)"/g) ?? []).length,
    1,
  )
  assert.equal(
    (item.match(/:style="watchlistTagStyle\(tag.color\)"/g) ?? []).length,
    2,
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
  panelHeight: ReturnType<typeof ref<number>>
  panelTop: ReturnType<typeof ref<number>>
  positionPanel: () => void
  backdrop: (event: {
    currentTarget: unknown
    target: unknown
    clientX: number
    clientY: number
  }) => void
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
  const body = { style: { overflow: 'auto' } }
  const listeners = new Map<string, () => void>()
  const viewportListeners = new Map<string, () => void>()
  const viewport = {
    height: 844,
    offsetTop: 0,
    addEventListener: (name: string, callback: () => void) =>
      viewportListeners.set(name, callback),
    removeEventListener: (name: string) => viewportListeners.delete(name),
  }
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
  class Dialog {
    showModal() {
      shown = true
    }
    close() {
      shown = false
    }
    querySelector(selector: string) {
      return new Button(selector)
    }
    getBoundingClientRect() {
      return { left: 16, right: 304, top: 16, bottom: 304 }
    }
  }
  const modal = new Dialog()
  const templateRefs = {
    dialog: ref(modal),
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
        `${script}\nexport { panelHeight, panelTop, positionPanel, backdrop, name, color, editDraft, editColor, target, pending, create, edit, submitTarget, cancel, open, openModal, closeModal, creating, openCreate, cancelCreate, cancelAndFocus }`,
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
        HTMLDialogElement: Dialog,
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
          innerHeight: 844,
          visualViewport: viewport,
          addEventListener: (name: string, callback: () => void) => {
            listeners.set(name, callback)
            if (name === 'pagehide') pagehide = callback
          },
          removeEventListener: (name: string) => listeners.delete(name),
        },
        document: {
          body,
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
    body,
    modal: templateRefs.dialog.value,
    viewport,
    listeners,
    viewportListeners,
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
    assert.equal(f.body.style.overflow, 'hidden')
    assert.equal(f.manager.creating.value, false)
    assert.equal(f.manager.target.value, null)
    assert.deepEqual(f.focusCalls, ['close'])
    f.manager.name.value = 'Brouillon'
    f.manager.color.value = 'blue'
    f.manager.closeModal()
    await nextTick()
    assert.equal(f.shown(), false)
    assert.equal(f.body.style.overflow, 'auto')
    assert.equal(f.manager.open.value, false)
    assert.equal(f.manager.name.value, 'Brouillon')
    assert.equal(f.manager.color.value, 'blue')
    assert.deepEqual(f.focusCalls, ['close', 'trigger'])
  } finally {
    f.stop()
  }
})

test('tag-manager panel matches add-dialog surface, heading, close and scroll styling', async () => {
  const [manager, page] = await Promise.all([
    readFile(
      new URL('../app/components/WatchlistTagManager.vue', import.meta.url),
      'utf8',
    ),
    readFile(
      new URL('../app/pages/compte/watchlist.vue', import.meta.url),
      'utf8',
    ),
  ])
  const addDialog = page.match(/<dialog\s[^>]*id="watchlist-add"[\s\S]*?>/)?.[0]
  const tagDialog = manager.match(
    /<dialog\s[^>]*id="watchlist-tag-manager"[\s\S]*?>/,
  )?.[0]
  assert.ok(addDialog)
  assert.ok(tagDialog)
  assert.equal(
    tagDialog.match(/class="([^"]*)"/)?.[1],
    addDialog.match(/class="([^"]*)"/)?.[1],
  )
  assert.match(tagDialog, /panelHeight - 32/)
  assert.match(tagDialog, /panelTop \+ panelHeight \/ 2/)
  assert.match(
    manager,
    /<header\s+class="flex shrink-0 items-center justify-between gap-3 border-b border-ink\/20 p-4"/,
  )
  assert.match(
    manager,
    /id="watchlist-tag-manager-title" class="account-heading"/,
  )
  assert.match(
    manager,
    /class="flex size-11 shrink-0 items-center justify-center hover:bg-subtle"/,
  )
  assert.match(manager, /<X :size="20" aria-hidden="true"/)
  assert.match(
    manager,
    /class="min-h-0 overflow-y-auto overscroll-contain p-4"/,
  )
  assert.doesNotMatch(
    manager,
    /rounded-lg|backdrop:bg-transparent|w-screen|@click\.self/,
  )
})

test('manager follows visual viewport, ignores panel clicks and closes true backdrop with scroll restoration', async () => {
  const f = await managerFixture()
  try {
    await f.manager.openModal()
    assert.equal(f.manager.panelHeight.value, 844)
    f.viewport.height = 320
    f.viewport.offsetTop = 60
    f.viewportListeners.get('resize')?.()
    assert.equal(f.manager.panelHeight.value, 320)
    assert.equal(f.manager.panelTop.value, 60)
    for (const event of [
      { currentTarget: f.modal, target: f.modal, clientX: 20, clientY: 20 },
      { currentTarget: f.modal, target: f.rowAction, clientX: 8, clientY: 8 },
    ]) {
      f.manager.backdrop(event)
      assert.equal(f.manager.open.value, true)
      assert.equal(f.body.style.overflow, 'hidden')
    }
    f.manager.backdrop({
      currentTarget: f.modal,
      target: f.modal,
      clientX: 8,
      clientY: 8,
    })
    await nextTick()
    assert.equal(f.manager.open.value, false)
    assert.equal(f.body.style.overflow, 'auto')
    assert.equal(f.focusCalls.at(-1), 'trigger')
  } finally {
    f.stop()
    assert.equal(f.listeners.size, 0)
    assert.equal(f.viewportListeners.size, 0)
  }
})

for (const boundary of ['scope', 'pagehide', 'unmount']) {
  test(`manager releases modal scroll lock at ${boundary} boundary without restoring focus`, async () => {
    const f = await managerFixture()
    try {
      await f.manager.openModal()
      if (boundary === 'scope') f.scopeKey.value++
      else if (boundary === 'pagehide') f.pagehide()
      else f.stop()
      await nextTick()
      assert.equal(f.manager.open.value, false)
      assert.equal(f.body.style.overflow, 'auto')
      assert.deepEqual(f.focusCalls, ['close'])
    } finally {
      f.stop()
    }
  })
}

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

test('compact tag trigger uses 28px minimum height while preserving width, focus and picker semantics', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistItemTags.vue', import.meta.url),
    'utf8',
  )
  const trigger = source.match(/<button\s+ref="trigger"[\s\S]*?<\/button>/)?.[0]
  assert.ok(trigger)
  assert.match(trigger, /inline-flex min-h-7 min-w-11/)
  assert.doesNotMatch(trigger, /min-h-11/)
  assert.match(
    trigger,
    /focus-visible:outline-2 focus-visible:outline-offset-2/,
  )
  assert.match(trigger, /:aria-label="`Modifier les tags de \$\{title\}`"/)
  assert.match(trigger, /:disabled="blocked && !open"/)
  assert.match(trigger, /:aria-expanded="open"/)
  assert.match(trigger, /:aria-controls="open \? regionId : undefined"/)
  assert.match(trigger, /@click="emit\('toggle'\)"/)
  assert.match(trigger, /<Plus :size="16" aria-hidden="true"\s*\/>\s*Tag/)
})

test('floating picker matches square modal styling without changing anchored group or dismissal semantics', async () => {
  const f = await itemFixture()
  try {
    const panel = f.source.slice(f.source.indexOf('ref="panel"'))
    assert.match(
      panel,
      /fixed z-40 flex flex-col overflow-hidden rounded-none border-2 border-ink bg-surface p-0 text-ink shadow-lg/,
    )
    assert.match(panel, /:style="position"/)
    assert.match(panel, /role="group"/)
    assert.match(panel, /items-start gap-3 border-b border-ink\/20 p-4/)
    assert.match(
      panel,
      /<h3\s+:id="`\$\{regionId\}-title`"\s+class="min-w-0 flex-1"/,
    )
    assert.match(
      panel,
      /flex size-11 shrink-0 items-center justify-center hover:bg-subtle focus-visible:outline-2/,
    )
    assert.match(panel, /aria-label="Fermer les tags"\s+@click="close\(true\)"/)
    assert.match(panel, /<X :size="20" aria-hidden="true"/)
    assert.match(panel, /min-h-0 overflow-y-auto overscroll-contain p-4/)
    assert.match(
      panel,
      /flex min-h-11 cursor-pointer items-center gap-3 px-2 py-2/,
    )
    assert.doesNotMatch(panel, /rounded-(?:lg|md)|<dialog|aria-modal|backdrop/)
    assert.doesNotMatch(f.source, /showModal|lockScroll|body\.style\.overflow/)
    assert.match(f.source, /document\[method\]\('pointerdown', outside\)/)
    assert.match(f.source, /document\[method\]\('focusin', outside\)/)
    assert.match(f.source, /document\[method\]\('keydown', keydown\)/)
  } finally {
    f.stop()
  }
})

test('floating heading keeps Tags above a smaller single-line original movie title with accessible ellipsis', async () => {
  const source = await readFile(
    new URL('../app/components/WatchlistItemTags.vue', import.meta.url),
    'utf8',
  )
  const heading = source.match(/<h3\b[\s\S]*?<\/h3>/)?.[0]
  assert.ok(heading)
  assert.match(source, /:aria-labelledby="`\$\{regionId\}-title`"/)
  assert.match(heading, /:id="`\$\{regionId\}-title`"/)
  assert.match(heading, /class="min-w-0 flex-1"/)
  assert.match(
    heading,
    /<span class="account-heading block">Tags<\/span>\s*<span class="block truncate text-sm font-normal">\{\{ title \}\}<\/span>/,
  )
  assert.doesNotMatch(
    heading,
    /line-clamp|aria-hidden|aria-label|slice|substring/,
  )
})

test('fixed two-line floating header retains 8px above/below gap and 320px cap', async () => {
  const f = await itemFixture()
  try {
    f.props.open = true
    f.templateRefs.heading.value.offsetHeight = 120
    f.anchor.top = 100
    f.anchor.bottom = 128
    f.handlers.positionPanel()
    assert.equal(f.handlers.position.value.top, '136px')
    assert.equal(f.handlers.position.value.maxHeight, '320px')
    f.anchor.top = 600
    f.anchor.bottom = 628
    f.handlers.positionPanel()
    assert.equal(f.handlers.position.value.top, '272px')
    f.viewport.height = 250
    f.handlers.positionPanel()
    assert.equal(f.handlers.position.value.top, '8px')
    assert.equal(f.handlers.position.value.maxHeight, '234px')
  } finally {
    f.stop()
  }
})

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
