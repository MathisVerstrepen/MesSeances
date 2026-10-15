import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import { parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import {
  compile,
  createSSRApp,
  effectScope,
  nextTick,
  ref,
  watch,
  type Ref,
} from 'vue'
import { watchedSections } from '../app/types/watched.ts'
import type {
  WatchedPreferences,
  WatchedSection,
} from '../app/types/watched.ts'

const source = await readFile(
  new URL('../app/components/WatchedPreferences.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const defaults: WatchedPreferences = {
  default_visibility: 'hide',
  search_visibility: 'inherit',
  planning_visibility: 'inherit',
  films_visibility: 'inherit',
  upcoming_visibility: 'inherit',
  home_visibility: 'inherit',
  cinema_city_visibility: 'inherit',
  auto_remove_watchlist: false,
}
interface ComponentState {
  draft: Ref<WatchedPreferences | null>
  dirty: Ref<boolean>
  saved: Ref<boolean>
  watched: { writesBlocked: Ref<boolean> }
  sections: { key: WatchedSection; label: string }[]
  edit: () => void
  submit: () => Promise<void>
}
interface ComponentModule {
  state?: ComponentState
}
function fixture() {
  const preferences = ref({ ...defaults })
  const scopeKey = ref(0)
  const writesBlocked = ref(false)
  const calls: WatchedPreferences[] = []
  let accepted = true
  const exports: ComponentModule = {}
  const scope = effectScope()
  scope.run(() =>
    runInNewContext(
      ts.transpileModule(
        `${descriptor.scriptSetup!.content}\nexports.state = { draft, dirty, saved, watched, sections, edit, submit };`,
        { compilerOptions: { module: ts.ModuleKind.CommonJS } },
      ).outputText,
      {
        exports,
        ref,
        watch,
        useWatched: () => ({
          preferences,
          scopeKey,
          writesBlocked,
          savePreferences: async (value: WatchedPreferences) => {
            calls.push(value)
            return accepted
          },
        }),
      },
    ),
  )
  assert.ok(exports.state)
  const state = exports.state
  return {
    state,
    calls,
    preferences,
    scopeKey,
    writesBlocked,
    accept: (value: boolean) => {
      accepted = value
    },
    stop: () => scope.stop(),
    render: () =>
      renderToString(
        createSSRApp({
          setup: () => state,
          render: compile(
            descriptor.template!.content.replaceAll(' as const', ''),
          ),
        }),
      ),
  }
}
function radios(html: string, group: string) {
  return [...html.matchAll(/<input\b[^>]*>/g)]
    .map(([input]) => input)
    .filter((input) => input.includes(`name="watched-${group}"`))
}
test('segmented preferences retain ordered native radio groups with one checked option each', async (t) => {
  const f = fixture()
  t.after(f.stop)
  for (const choice of ['inherit', 'show', 'hide'] as const) {
    assert.ok(f.state.draft.value)
    for (const section of watchedSections)
      f.state.draft.value[`${section}_visibility`] = choice
    for (const global of ['hide', 'show'] as const) {
      f.state.draft.value.default_visibility = global
      const html = await f.render()
      for (const group of ['default', ...watchedSections]) {
        const options = radios(html, group)
        assert.deepEqual(
          options.map((input) => input.match(/value="([^"]+)"/)?.[1]),
          group === 'default' ? ['hide', 'show'] : ['inherit', 'show', 'hide'],
        )
        const checked = options.filter((input) =>
          /\bchecked(?:\s|>|=)/.test(input),
        )
        assert.equal(checked.length, 1)
        assert.ok(
          checked[0]!.includes(
            `value="${group === 'default' ? global : choice}"`,
          ),
        )
        for (const input of options) {
          assert.match(input, /type="radio"/)
          assert.match(
            input,
            /class="peer absolute[^"]*size-full[^"]*opacity-0/,
          )
        }
      }
      assert.match(html, /peer-checked:bg-ink/)
      assert.match(html, /peer-focus-visible:outline-2/)
      assert.match(
        html,
        /Retirer de la watchlist quand je marque un film comme vu/,
      )
    }
  }
  assert.equal(f.calls.length, 0)
})
test('visibility controls align left on mobile and right on desktop with a centered native checkbox label', async (t) => {
  const f = fixture()
  t.after(f.stop)
  const html = await f.render()
  const controls = [
    ...html.matchAll(/<div class="([^"]*grid-cols-[23][^"]*)"/g),
  ]
  assert.equal(controls.length, 7)
  for (const [, classes] of controls) {
    assert.match(classes!, /\bjustify-self-start\b/)
    assert.match(classes!, /\bsm:justify-self-end\b/)
  }
  const checkbox = html.match(
    /<label class="([^"]*)">\s*<input\b([^>]*type="checkbox"[^>]*)>\s*<span>Retirer de la watchlist quand je marque un film comme vu<\/span>\s*<\/label>/,
  )
  assert.ok(checkbox)
  assert.match(checkbox[1]!, /\bitems-center\b/)
  assert.match(checkbox[1]!, /\bmin-h-11\b/)
  assert.doesNotMatch(checkbox[2]!, /\bmt-1\b/)
  assert.equal(f.calls.length, 0)
})
test('segment edits only save on explicit submit and survive rejected saves and reconciliation', async (t) => {
  const f = fixture()
  t.after(f.stop)
  assert.ok(f.state.draft.value)
  f.state.draft.value.default_visibility = 'show'
  f.state.draft.value.search_visibility = 'hide'
  f.state.draft.value.auto_remove_watchlist = true
  f.state.edit()
  assert.equal(f.calls.length, 0)
  assert.equal(f.state.dirty.value, true)
  f.accept(false)
  await f.state.submit()
  f.preferences.value = { ...defaults, planning_visibility: 'hide' }
  await nextTick()
  assert.equal(f.state.draft.value.default_visibility, 'show')
  assert.equal(f.state.draft.value.search_visibility, 'hide')
  assert.equal(f.state.dirty.value, true)
  assert.equal(f.state.saved.value, false)
  assert.equal(f.calls.length, 1)
  f.accept(true)
  await f.state.submit()
  assert.equal(f.calls.length, 2)
  assert.deepEqual(
    { ...f.calls[1] },
    {
      ...defaults,
      default_visibility: 'show',
      search_visibility: 'hide',
      auto_remove_watchlist: true,
    },
  )
  assert.equal(f.state.dirty.value, false)
  assert.equal(f.state.saved.value, true)
  f.scopeKey.value++
  await nextTick()
  assert.equal(f.state.draft.value, null)
  assert.equal(f.state.saved.value, false)
})
test('blocked preferences disable the native fieldset and save action without losing checked state', async (t) => {
  const f = fixture()
  t.after(f.stop)
  f.writesBlocked.value = true
  const blocked = await f.render()
  assert.match(blocked, /<fieldset\b[^>]*\bdisabled(?:\s|>|=)/)
  assert.match(blocked, /<button\b[^>]*\bdisabled(?:\s|>|=)/)
  assert.equal(
    radios(blocked, 'default').filter((input) =>
      /\bchecked(?:\s|>|=)/.test(input),
    ).length,
    1,
  )
  f.writesBlocked.value = false
  const ready = await f.render()
  assert.doesNotMatch(ready, /<(?:fieldset|button)\b[^>]*\bdisabled(?:\s|>|=)/)
  assert.equal(f.calls.length, 0)
})
