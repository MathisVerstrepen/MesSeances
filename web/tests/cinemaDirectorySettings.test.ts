import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { nextTick, ref, shallowRef } from 'vue'

const source = await readFile(
  new URL('../app/pages/cinemas.vue', import.meta.url),
  'utf8',
)
const script = source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)![1]!
const parsed = ts.createSourceFile(
  'page.ts',
  script,
  ts.ScriptTarget.Latest,
  true,
)
const names = [
  'openSettings',
  'closeSettings',
  'handleSettingsBackdrop',
  'handleSettingsViewport',
  'handleSettingsKeydown',
]
const functions = parsed.statements.filter(
  (node) =>
    ts.isFunctionDeclaration(node) &&
    node.name &&
    names.includes(node.name.text),
)
assert.equal(functions.length, names.length)
const code = ts.transpileModule(
  functions.map((node) => node.getFullText(parsed)).join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

class FocusTarget {
  isConnected = true
  focusCalls = 0
  focus() {
    this.focusCalls++
  }
  hasAttribute() {
    return false
  }
  getClientRects() {
    return [{}]
  }
}

function harness({
  desktop = false,
  unmounted = false,
  overflow = 'auto',
} = {}) {
  const trigger = new FocusTarget()
  const close = new FocusTarget()
  const dialog = {
    open: false,
    opens: 0,
    closes: 0,
    showModal() {
      this.open = true
      this.opens++
    },
    close() {
      this.open = false
      this.closes++
    },
    getBoundingClientRect: () => ({
      left: 0,
      right: 390,
      top: 400,
      bottom: 844,
    }),
    querySelectorAll: () => [close, trigger],
  }
  const bindings = {
    settingsOpen: ref(false),
    isDesktop: ref(desktop),
    previousRootStyles: null,
    window: { innerWidth: desktop ? 1440 : 390 },
    settingsDialog: shallowRef(dialog),
    settingsCloseButton: shallowRef(close),
    settingsTrigger: null,
    desktopMediaQuery: { matches: desktop },
    isUnmounted: unmounted,
    bodyOverflowBeforeLock: null,
    document: {
      body: { style: { overflow } },
      documentElement: {
        clientWidth: desktop ? 1425 : 390,
        style: { overflow: 'scroll', scrollbarGutter: 'auto' },
      },
      activeElement: close,
    },
    HTMLElement: FocusTarget,
    nextTick,
  }
  // SAFETY: Extracted page functions run only with explicit synthetic DOM bindings.
  const page = new Function(
    ...Object.keys(bindings),
    `${code}\nreturn { ${names.join(',')}, depart: () => { isUnmounted = true } }`,
  )(...Object.values(bindings)) as {
    openSettings: (event: { currentTarget: FocusTarget }) => Promise<void>
    closeSettings: (options?: { restoreFocus: boolean }) => void
    handleSettingsBackdrop: (event: {
      target: unknown
      clientX: number
      clientY: number
    }) => void
    handleSettingsViewport: (event: { matches: boolean }) => void
    handleSettingsKeydown: (event: {
      key: string
      shiftKey: boolean
      preventDefault: () => void
    }) => void
    depart: () => void
  }
  return { page, bindings, dialog, trigger, close }
}

test('settings use native modality, focus close, restore exact body overflow and trigger without mutations', async () => {
  const { page, bindings, dialog, trigger, close } = harness()
  await page.openSettings({ currentTarget: trigger })
  assert.equal(dialog.opens, 1)
  assert.equal(dialog.open, true)
  assert.equal(bindings.settingsOpen.value, true)
  assert.equal(bindings.document.body.style.overflow, 'hidden')
  assert.equal(bindings.document.documentElement.style.overflow, 'hidden')
  assert.equal(close.focusCalls, 1)
  await page.openSettings({ currentTarget: trigger })
  assert.equal(dialog.opens, 1)
  page.closeSettings()
  await nextTick()
  assert.equal(dialog.open, false)
  assert.equal(bindings.settingsOpen.value, false)
  assert.equal(bindings.document.body.style.overflow, 'auto')
  assert.deepEqual(bindings.document.documentElement.style, {
    overflow: 'scroll',
    scrollbarGutter: 'auto',
  })
  assert.equal(trigger.focusCalls, 1)
  page.closeSettings()
  assert.equal(dialog.closes, 1)
  assert.doesNotMatch(
    code,
    /router\.|location\.|selectedOnly|setFavoriteTheaterIds|draftFavoriteTheaterIds/,
  )
})

test('Tab boundaries loop both ways and ignore other keys, hidden and disabled controls', async () => {
  const { page, trigger, close, dialog, bindings } = harness()
  await page.openSettings({ currentTarget: trigger })
  let prevented = 0
  const event = {
    key: 'Tab',
    shiftKey: true,
    preventDefault: () => {
      prevented++
    },
  }
  page.handleSettingsKeydown(event)
  assert.equal(prevented, 1)
  assert.equal(trigger.focusCalls, 1)
  bindings.document.activeElement = trigger
  event.shiftKey = false
  page.handleSettingsKeydown(event)
  assert.equal(prevented, 2)
  assert.equal(close.focusCalls, 2)
  event.key = 'Enter'
  page.handleSettingsKeydown(event)
  assert.equal(prevented, 2)
  dialog.querySelectorAll = () => [
    Object.assign(new FocusTarget(), { hasAttribute: () => true }),
    Object.assign(new FocusTarget(), { getClientRects: () => [] }),
    close,
    trigger,
  ]
  event.key = 'Tab'
  page.handleSettingsKeydown(event)
  assert.equal(prevented, 3)
  assert.equal(close.focusCalls, 3)
  page.closeSettings({ restoreFocus: false })
  page.handleSettingsKeydown(event)
  assert.equal(prevented, 3)
})

test('unmounted and cancelled pending opens never enter modal or lock body', async () => {
  for (const scenario of ['unmounted', 'closed', 'departed']) {
    const { page, dialog, trigger, bindings } = harness({
      unmounted: scenario === 'unmounted',
    })
    const opening = page.openSettings({ currentTarget: trigger })
    if (scenario === 'closed') page.closeSettings({ restoreFocus: false })
    if (scenario === 'departed') {
      page.depart()
      page.closeSettings({ restoreFocus: false })
    }
    await opening
    assert.equal(dialog.opens, 0)
    assert.equal(bindings.document.body.style.overflow, 'auto')
    assert.equal(trigger.focusCalls, 0)
  }
})

test('backdrop dismisses the overlay; resizing retains it and unmount restores scrolling', async () => {
  const { page, dialog, trigger, bindings } = harness({ overflow: '' })
  await page.openSettings({ currentTarget: trigger })
  page.handleSettingsBackdrop({ target: trigger, clientX: 100, clientY: 100 })
  assert.equal(dialog.open, true)
  page.handleSettingsBackdrop({ target: dialog, clientX: 100, clientY: 100 })
  await nextTick()
  assert.equal(dialog.open, false)
  assert.equal(trigger.focusCalls, 1)
  await page.openSettings({ currentTarget: trigger })
  page.handleSettingsViewport({ matches: false })
  assert.equal(dialog.open, true)
  page.handleSettingsViewport({ matches: true })
  await nextTick()
  assert.equal(dialog.open, true)
  assert.equal(bindings.isDesktop.value, true)
  page.closeSettings({ restoreFocus: false })
  assert.equal(bindings.document.body.style.overflow, '')
  assert.equal(trigger.focusCalls, 1)
  await page.openSettings({ currentTarget: trigger })
  page.depart()
  page.closeSettings({ restoreFocus: false })
  await nextTick()
  assert.equal(bindings.document.body.style.overflow, '')
  assert.equal(trigger.focusCalls, 1)
})

test('same native overlay exposes both filters, desktop drawer and mobile sheet with one visible trigger', () => {
  const template = source.slice(source.indexOf('<template>'))
  const sheet = template.slice(template.indexOf('<dialog'))
  assert.match(sheet, /aria-labelledby="cinema-settings-title"/)
  assert.match(sheet, /aria-modal="true"/)
  assert.match(sheet, /@cancel.prevent="closeSettings\(\)"/)
  assert.match(sheet, /lg:w-\[32rem\]/)
  assert.match(sheet, /h-dvh/)
  assert.match(sheet, /bg-\[#ffcf3f\]/)
  assert.match(
    sheet,
    /cinema-settings-content min-h-0 flex-1 space-y-6 overflow-y-auto overscroll-contain/,
  )
  assert.match(sheet, /<CinemaChainFilter[\s\S]*?<CinemaTechnologyFilter/)
  assert.match(sheet, /aria-label="Fermer les filtres"/)
  assert.match(sheet, /<footer[^>]*sticky bottom-0[^>]*shrink-0/)
  assert.match(sheet, /safe-area-inset-bottom/)
  assert.match(sheet, /@click="resetFilters"/)
  assert.match(sheet, /Voir \{\{ visibleTheaterCount \}\}/)
  assert.match(
    template,
    /aria-controls="cinema-settings"[\s\S]*?aria-haspopup="dialog"/,
  )
  assert.match(template, /:aria-expanded="settingsOpen"/)
  assert.match(
    template,
    /<Teleport\s+to="#cinema-settings-controls"\s+:disabled="!settingsOpen \|\| isDesktop"/,
  )
  assert.match(
    source,
    /onBeforeUnmount\([\s\S]*?closeSettings\(\{ restoreFocus: false \}\)/,
  )
})

test('desktop drawer locks root scrolling without losing the scrollbar gutter', async () => {
  const { page, bindings, trigger } = harness({ desktop: true })
  await page.openSettings({ currentTarget: trigger })
  assert.equal(bindings.settingsOpen.value, true)
  assert.equal(
    bindings.document.documentElement.style.scrollbarGutter,
    'stable',
  )
  page.closeSettings()
  await nextTick()
  assert.equal(bindings.document.documentElement.style.scrollbarGutter, 'auto')
  assert.equal(trigger.focusCalls, 1)
})

test('one named responsive location launch stays beside explicitly labeled search, outside the sheet', () => {
  const template = source.slice(source.indexOf('<template>'))
  const label = template.match(
    /<label\s+for="cinema-directory-search"[\s\S]*?<\/label\s*>/,
  )?.[0]
  assert.ok(label)
  assert.doesNotMatch(label, /<button|<input/)
  assert.match(template, /<input\s+id="cinema-directory-search"/)
  assert.match(template, /grid-cols-\[minmax\(0,1fr\)_auto\]/)
  const launch = template.match(
    /<button\s+v-if="!isNearbyMode"[\s\S]*?<\/button>/,
  )?.[0]
  assert.ok(launch)
  assert.match(launch, /min-h-\[3\.25rem\] w-\[3\.25rem\]/)
  assert.match(launch, /lg:w-auto lg:px-4/)
  assert.match(launch, /:aria-label="[^"]*Utiliser ma position'/)
  assert.match(launch, /:title="[^"]*Utiliser ma position'/)
  assert.match(launch, /:disabled="locationStatus === 'requesting'"/)
  assert.match(launch, /:aria-busy="locationStatus === 'requesting'"/)
  assert.match(
    launch,
    /<LoaderCircle[\s\S]*?v-if="locationStatus === 'requesting'"/,
  )
  assert.match(launch, /<span class="hidden lg:inline">/)
  assert.equal(template.match(/@click="useCurrentPosition"/g)?.length, 1)
  assert.ok(
    template.indexOf(launch) <
      template.indexOf('<Teleport to="#cinema-settings-location"'),
  )
  assert.match(
    template,
    /<Teleport to="#cinema-settings-location"[\s\S]*?@click="showByCity"/,
  )
})
