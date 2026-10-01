import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, reactive, ref, watch } from 'vue'
import { isAccountPage } from '../shared/accountPrivacy.ts'

const source = await readFile(
  new URL('../app/components/CinemaPreferencePrompt.vue', import.meta.url),
  'utf8',
)
const parsed = ts.createSourceFile(
  'prompt.ts',
  source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!,
  ts.ScriptTarget.Latest,
  true,
)
const compiled = ts.transpileModule(
  parsed.statements
    .filter((node) => !ts.isImportDeclaration(node))
    .map((node) => node.getFullText(parsed))
    .join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

type PointerFixture = Pick<
  PointerEvent,
  'pointerId' | 'pointerType' | 'isPrimary' | 'clientX' | 'clientY' | 'target'
>
type ClickFixture = Pick<
  MouseEvent,
  'detail' | 'preventDefault' | 'stopPropagation'
>

class ElementFixture extends EventTarget {
  readonly tag: string
  readonly parent: ElementFixture | null
  readonly captures = new Set<number>()

  constructor(tag = 'div', parent: ElementFixture | null = null) {
    super()
    this.tag = tag
    this.parent = parent
  }

  closest(selector: string): ElementFixture | null {
    if (selector === this.tag) return this
    return this.parent?.closest(selector) ?? null
  }

  hasPointerCapture(id: number) {
    return this.captures.has(id)
  }

  setPointerCapture(id: number) {
    this.captures.add(id)
  }

  releasePointerCapture(id: number) {
    this.captures.delete(id)
  }
}

function harness(
  storage = new Map<string, string>(),
  fails = false,
  coarse = true,
) {
  const route = reactive({ path: '/' })
  const preferences = {
    hasSavedTheaterSelection: ref<boolean | null>(null),
    error: ref<string | null>(null),
    syncError: ref<string | null>(null),
    writesBlocked: ref(false),
  }
  const update = reactive({ available: false })
  let mount = () => {}
  const bindings = {
    ref,
    computed,
    watch,
    isAccountPage,
    Element: ElementFixture,
    useRoute: () => route,
    useNuxtApp: () => ({ $appUpdate: { state: update } }),
    useCinemaPreferences: () => preferences,
    onMounted: (callback: () => void) => {
      mount = callback
    },
    onBeforeUnmount: () => {},
    window: { matchMedia: () => ({ matches: coarse }) },
    localStorage: {
      getItem: (key: string) => {
        if (fails) throw new Error('blocked')
        return storage.get(key) ?? null
      },
      setItem: (key: string, value: string) => {
        if (fails) throw new Error('blocked')
        storage.set(key, value)
      },
    },
  }
  // SAFETY: The appended return names actual setup refs and handlers. Fixtures cover every
  // accessed event/element member; the injected Element constructor also validates target identity.
  const page = new Function(
    ...Object.keys(bindings),
    `${compiled}\nreturn { visible, dismiss, popup, dragX, dragY, onPointerDown, onPointerMove, onPointerUp, onPointerCancel, onLostPointerCapture, onClick }`,
  )(...Object.values(bindings)) as {
    visible: { value: boolean }
    dismiss: () => void
    popup: { value: ElementFixture | null }
    dragX: { value: number }
    dragY: { value: number }
    onPointerDown: (event: PointerFixture) => void
    onPointerMove: (event: PointerFixture) => void
    onPointerUp: (event: PointerFixture) => void
    onPointerCancel: (event: PointerFixture) => void
    onLostPointerCapture: (event: PointerFixture) => void
    onClick: (event: ClickFixture) => void
  }
  page.popup.value = new ElementFixture()
  return { page, route, preferences, update, mount: () => mount() }
}

function pointer(
  x = 0,
  y = 0,
  overrides: Partial<PointerFixture> = {},
): PointerFixture {
  return {
    pointerId: 1,
    pointerType: 'touch',
    isPrimary: true,
    clientX: x,
    clientY: y,
    target: new ElementFixture(),
    ...overrides,
  }
}

function readyPrompt(storage = new Map<string, string>(), fails = false) {
  const h = harness(storage, fails)
  h.mount()
  h.preferences.hasSavedTheaterSelection.value = false
  return h
}

function click(detail = 1) {
  let prevented = false
  let stopped = false
  const event: ClickFixture = {
    detail,
    preventDefault: () => {
      prevented = true
    },
    stopPropagation: () => {
      stopped = true
    },
  }
  return {
    event,
    prevented: () => prevented,
    stopped: () => stopped,
  }
}

test('prompt waits for presence and storage, suppresses saved/inactive/private/error/write/update states', () => {
  const h = harness()
  h.preferences.hasSavedTheaterSelection.value = false
  assert.equal(h.page.visible.value, false)
  h.mount()
  assert.equal(h.page.visible.value, true)
  for (const path of [
    '/cinemas',
    '/connexion',
    '/compte',
    '/compte/preferences',
    '/admin',
    '/admin/sync',
  ]) {
    h.route.path = path
    assert.equal(h.page.visible.value, false, path)
  }
  h.route.path = '/film/film-1'
  for (const present of [null, true]) {
    h.preferences.hasSavedTheaterSelection.value = present
    assert.equal(h.page.visible.value, false)
  }
  h.preferences.hasSavedTheaterSelection.value = false
  h.preferences.error.value = 'Erreur'
  assert.equal(h.page.visible.value, false)
  h.preferences.error.value = null
  h.preferences.syncError.value = 'Erreur'
  assert.equal(h.page.visible.value, false)
  h.preferences.syncError.value = null
  h.preferences.writesBlocked.value = true
  assert.equal(h.page.visible.value, false)
  h.preferences.writesBlocked.value = false
  h.update.available = true
  assert.equal(h.page.visible.value, false)
  h.update.available = false
  assert.equal(h.page.visible.value, true)
})

test('dismissal persists across remounts, does not reset on scope changes, and tolerates unavailable storage', () => {
  const storage = new Map<string, string>()
  for (const fails of [false, true]) {
    const h = harness(storage, fails)
    h.mount()
    h.preferences.hasSavedTheaterSelection.value = false
    h.page.dismiss()
    h.route.path = '/planning'
    h.preferences.hasSavedTheaterSelection.value = true
    h.preferences.hasSavedTheaterSelection.value = false
    assert.equal(h.page.visible.value, false)
  }
  const remount = harness(storage)
  remount.mount()
  remount.preferences.hasSavedTheaterSelection.value = false
  assert.equal(remount.page.visible.value, false)
  assert.equal(storage.get('messeances.cinemaSelectionPromptDismissed.v1'), '1')
})

test('prompt is compact, client-only, keyboard dismissible, labelled, and navigation does not dismiss', async () => {
  assert.match(source, /@keydown\.esc\.stop="dismiss"/)
  assert.match(source, /aria-label="Ne plus afficher"/)
  assert.match(source, /role="status"\s+aria-live="polite"/)
  assert.match(source, /min-h-11/)
  assert.match(source, /size-11/)
  assert.match(source, /safe-area-inset-bottom/)
  assert.doesNotMatch(source, /initialize\(|useState|autofocus/)
  const app = await readFile(new URL('../app/app.vue', import.meta.url), 'utf8')
  assert.match(app, /<ClientOnly><CinemaPreferencePrompt \/><\/ClientOnly>/)
  assert.doesNotMatch(
    source.match(/<NuxtLink[\s\S]*?<\/NuxtLink\s*>/)![0],
    /dismiss|@click/,
  )
})

test('prompt keeps one compact row with contextual icons and a yellow navigation action', () => {
  assert.match(source, /<Clapperboard[\s\S]*?aria-hidden="true"/)
  assert.match(source, /<ArrowRight :size="14" aria-hidden="true"/)
  assert.match(source, /gap-3 rounded-\[3px\]/)
  assert.match(source, /shadow-\[2px_2px_0_#27272a\]/)
  assert.doesNotMatch(source, /flex-wrap|bg-highlight/)
  const action = source.match(/<NuxtLink[\s\S]*?<\/NuxtLink\s*>/)![0]
  assert.match(action, /to="\/cinemas"/)
  assert.match(action, /min-h-11 shrink-0/)
  assert.match(action, /bg-\[#ffcf3f\]/)
  assert.match(action, />\s+Choisir\s/)
  assert.match(source, /absolute -top-11 right-0 grid size-11/)
  assert.match(source, /text-muted opacity-0 group-hover:pointer-events-auto/)
  assert.match(source, /group-hover:opacity-100/)
  assert.match(source, /group-has-\[:focus-visible\]:opacity-100/)
  assert.match(source, /motion-reduce:translate-none/)
  assert.match(source, /\[@media\(any-pointer:coarse\)\]:touch-none/)
  assert.match(source, /@click\.capture="onClick"/)
})

test('deliberate left, right and down swipes persist browser dismissal, including CTA-origin drags', () => {
  for (const [x, y] of [
    [-64, 0],
    [64, 0],
    [0, 40],
  ]) {
    const storage = new Map<string, string>()
    const h = readyPrompt(storage)
    h.page.onPointerDown(pointer())
    h.page.onPointerMove(pointer(x, y))
    assert.equal(h.page.dragX.value, x)
    assert.equal(h.page.dragY.value, y)
    h.page.onPointerUp(pointer(x, y))
    assert.equal(h.page.visible.value, false)
    assert.equal(h.page.dragX.value, 0)
    assert.equal(h.page.dragY.value, 0)
    assert.equal(readyPrompt(storage).page.visible.value, false)
    const postDragClick = click()
    h.page.onClick(postDragClick.event)
    assert.equal(postDragClick.prevented(), true)
    assert.equal(postDragClick.stopped(), true)
  }
  const blockedStorage = readyPrompt(new Map(), true)
  blockedStorage.page.onPointerDown(pointer())
  blockedStorage.page.onPointerUp(pointer(80, 0))
  assert.equal(blockedStorage.page.visible.value, false)
})

test('short, upward, ambiguous, cancelled and returned drags reset without dismissing or activating CTA', () => {
  for (const [x, y] of [
    [63, 0],
    [0, 39],
    [0, -90],
    [70, -100],
    [70, 70],
  ]) {
    const h = readyPrompt()
    h.page.onPointerDown(pointer())
    h.page.onPointerMove(pointer(x, y))
    h.page.onPointerUp(pointer(x, y))
    assert.equal(h.page.visible.value, true, `${x},${y}`)
    assert.equal(h.page.dragX.value, 0)
    assert.equal(h.page.dragY.value, 0)
    const postDragClick = click()
    h.page.onClick(postDragClick.event)
    assert.equal(postDragClick.prevented(), true)
  }
  for (const end of ['cancel', 'return'] as const) {
    const h = readyPrompt()
    h.page.onPointerDown(pointer())
    h.page.onPointerMove(pointer(90, 0))
    if (end === 'cancel') h.page.onPointerCancel(pointer(90, 0))
    else h.page.onPointerUp(pointer())
    assert.equal(h.page.visible.value, true)
    assert.equal(h.page.dragX.value, 0)
    const postDragClick = click()
    h.page.onClick(postDragClick.event)
    assert.equal(postDragClick.prevented(), true)
  }
})

test('ordinary CTA taps and keyboard activation never persist dismissal; later taps work after drag', () => {
  const storage = new Map<string, string>()
  const h = readyPrompt(storage)
  h.page.onPointerDown(pointer())
  h.page.onPointerUp(pointer(4, 2))
  const tap = click()
  h.page.onClick(tap.event)
  assert.equal(tap.prevented(), false)
  h.page.onPointerDown(pointer())
  h.page.onPointerUp(pointer(30, 0))
  const keyboard = click(0)
  h.page.onClick(keyboard.event)
  assert.equal(keyboard.prevented(), false)
  h.page.onPointerDown(pointer())
  h.page.onPointerUp(pointer())
  const nextTap = click()
  h.page.onClick(nextTap.event)
  assert.equal(nextTap.prevented(), false)
  assert.equal(storage.size, 0)
  h.route.path = '/cinemas'
  assert.equal(h.page.visible.value, false)
  h.route.path = '/'
  assert.equal(h.page.visible.value, true)
})

test('mouse, pen, nonprimary and unavailable-coarse input cannot swipe; second touch cancels current drag', () => {
  for (const overrides of [
    { pointerType: 'mouse' },
    { pointerType: 'pen' },
    { isPrimary: false },
    { target: new ElementFixture('button') },
  ]) {
    const h = readyPrompt()
    h.page.onPointerDown(pointer(0, 0, overrides))
    h.page.onPointerMove(pointer(90, 0, overrides))
    h.page.onPointerUp(pointer(90, 0, overrides))
    assert.equal(h.page.visible.value, true)
    assert.equal(h.page.dragX.value, 0)
  }
  const fine = harness(new Map(), false, false)
  fine.mount()
  fine.preferences.hasSavedTheaterSelection.value = false
  fine.page.onPointerDown(pointer())
  fine.page.onPointerUp(pointer(90, 0))
  assert.equal(fine.page.visible.value, true)
  const h = readyPrompt()
  h.page.onPointerDown(pointer())
  h.page.onPointerMove(pointer(80, 0, { pointerId: 2 }))
  assert.equal(h.page.dragX.value, 0)
  h.page.onPointerMove(pointer(80, 0))
  h.page.onPointerDown(pointer(0, 0, { pointerId: 2, isPrimary: false }))
  h.page.onPointerUp(pointer(90, 0))
  assert.equal(h.page.visible.value, true)
  assert.equal(h.page.dragX.value, 0)
})

test('pointer target guard ignores null, non-elements and button descendants but accepts CTA descendants', () => {
  for (const target of [
    null,
    new EventTarget(),
    new ElementFixture('svg', new ElementFixture('button')),
  ]) {
    const h = readyPrompt()
    h.page.onPointerDown(pointer(0, 0, { target }))
    h.page.onPointerMove(pointer(80, 0, { target }))
    h.page.onPointerUp(pointer(80, 0, { target }))
    assert.equal(h.page.visible.value, true)
    assert.equal(h.page.dragX.value, 0)
    assert.equal(h.page.popup.value!.hasPointerCapture(1), false)
  }
  const h = readyPrompt()
  const target = new ElementFixture('svg', new ElementFixture('a'))
  h.page.onPointerDown(pointer(0, 0, { target }))
  h.page.onPointerMove(pointer(-80, 0, { target }))
  assert.equal(h.page.popup.value!.hasPointerCapture(1), true)
  h.page.onPointerUp(pointer(-80, 0, { target }))
  assert.equal(h.page.visible.value, false)
})

test('visibility changes reset active gestures and release capture before prompt reappears', () => {
  const h = readyPrompt()
  h.page.onPointerDown(pointer())
  h.page.onPointerMove(pointer(80, 0))
  assert.equal(h.page.popup.value!.hasPointerCapture(1), true)
  h.update.available = true
  assert.equal(h.page.dragX.value, 0)
  assert.equal(h.page.popup.value!.hasPointerCapture(1), false)
  h.update.available = false
  h.page.onPointerUp(pointer(100, 0))
  assert.equal(h.page.visible.value, true)
  h.page.onPointerDown(pointer())
  h.page.onPointerMove(pointer(80, 0))
  h.route.path = '/cinemas'
  h.route.path = '/'
  h.page.onPointerUp(pointer(100, 0))
  assert.equal(h.page.visible.value, true)
  assert.equal(h.page.dragX.value, 0)
})

test('transferring implicit CTA capture does not cancel swipe; losing popup capture does', () => {
  const h = readyPrompt()
  h.page.onPointerDown(pointer())
  h.page.onPointerMove(pointer(80, 0))
  h.page.onLostPointerCapture(pointer())
  assert.equal(h.page.dragX.value, 80)
  h.page.onPointerUp(pointer(80, 0))
  assert.equal(h.page.visible.value, false)
  const lost = readyPrompt()
  lost.page.onPointerDown(pointer())
  lost.page.onPointerMove(pointer(80, 0))
  lost.page.onLostPointerCapture(
    pointer(80, 0, { target: lost.page.popup.value }),
  )
  lost.page.onPointerUp(pointer(80, 0))
  assert.equal(lost.page.dragX.value, 0)
  assert.equal(lost.page.visible.value, true)
})
