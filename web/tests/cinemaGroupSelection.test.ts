import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, ref } from 'vue'
import {
  groupSelectedTheatersFirst,
  updateTheaterSelection,
} from '../app/utils/cinemaSelection.ts'
import type { Theater } from '../app/types/api.ts'

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
const names = ['groupSelectionState', 'updateGroup', 'applyDraftSelection']
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

function theater(id: string): Theater {
  return {
    id,
    provider: 'ugc',
    slug: id,
    name: id,
    city: 'Paris',
    city_slug: 'paris',
    address: '',
    postal_code: '75000',
    available_dates: [],
    accepted_passes: [],
  }
}
const members = [theater('shown-a'), theater('shown-b')]

function harness(ids: string[], blocked = false) {
  let acknowledge!: (saved: boolean) => void
  const writes: string[][] = []
  const bindings = {
    draftFavoriteTheaterIds: ref([...ids]),
    favoriteTheaterIds: ref([...ids]),
    writesBlocked: ref(blocked),
    selectionScopeKey: ref(0),
    statusMessage: ref(''),
    isUnmounted: false,
    updateTheaterSelection,
    setFavoriteTheaterIds: (nextIds: string[]) => {
      writes.push([...nextIds])
      return new Promise<boolean>((resolve) => {
        acknowledge = resolve
      })
    },
  }
  const selectedIds = computed(
    () => new Set(bindings.draftFavoriteTheaterIds.value),
  )
  // SAFETY: Actual page functions execute only with explicit selection bindings.
  const page = new Function(
    ...Object.keys(bindings),
    'selectedIds',
    `${code}\nreturn { groupSelectionState, updateGroup, depart: () => { isUnmounted = true } }`,
  )(...Object.values(bindings), selectedIds) as {
    groupSelectionState: (
      theaters: readonly Theater[],
    ) => 'all' | 'some' | 'none'
    updateGroup: (theaters: readonly Theater[], select: boolean) => void
    depart: () => void
  }
  return {
    page,
    bindings,
    writes,
    settle: async (saved: boolean) => {
      acknowledge(saved)
      await Promise.resolve()
    },
  }
}

test('group checkbox derives unchecked, checked and mixed from displayed members only', () => {
  for (const [ids, expected] of [
    [['hidden'], 'none'],
    [['hidden', 'shown-a'], 'some'],
    [['shown-a', 'shown-b'], 'all'],
  ] as const) {
    const { page } = harness([...ids])
    assert.equal(page.groupSelectionState(members), expected)
  }
})

test('unchecked and synthetic mixed checkbox select displayed group, preserving hidden IDs', async () => {
  for (const ids of [['hidden'], ['hidden', 'shown-a']]) {
    const { page, bindings, writes, settle } = harness(ids)
    page.updateGroup(members, page.groupSelectionState(members) !== 'all')
    assert.deepEqual(writes, [['hidden', 'shown-a', 'shown-b']])
    assert.equal(page.groupSelectionState(members), 'all')
    bindings.favoriteTheaterIds.value = ['hidden', 'shown-a', 'shown-b']
    await settle(true)
    assert.deepEqual(bindings.draftFavoriteTheaterIds.value, [
      'hidden',
      'shown-a',
      'shown-b',
    ])
    assert.equal(bindings.statusMessage.value, '')
  }
})

test('checked checkbox deselects only displayed group and selected-first partitions remain intact', async () => {
  const { page, bindings, writes, settle } = harness([
    'hidden',
    'shown-a',
    'shown-b',
  ])
  page.updateGroup(members, page.groupSelectionState(members) !== 'all')
  assert.deepEqual(writes, [['hidden']])
  bindings.favoriteTheaterIds.value = ['hidden']
  await settle(true)
  assert.deepEqual(bindings.draftFavoriteTheaterIds.value, ['hidden'])
  const groups = groupSelectedTheatersFirst(members, new Set(['shown-a']))
  assert.deepEqual(
    groups.map((group) => group.theaters.map((item) => item.id)),
    [['shown-a'], ['shown-b']],
  )
  const mixed = harness(['shown-a']).page
  assert.deepEqual(
    groups.map((group) => mixed.groupSelectionState(group.theaters)),
    ['all', 'none'],
  )
})

test('group writes keep read-only and unmount fences before mutations', () => {
  for (const scenario of ['blocked', 'departed']) {
    const { page, bindings, writes } = harness(
      ['hidden'],
      scenario === 'blocked',
    )
    if (scenario === 'departed') page.depart()
    page.updateGroup(members, true)
    assert.deepEqual(writes, [])
    assert.deepEqual(bindings.draftFavoriteTheaterIds.value, ['hidden'])
  }
})

test('pending group acknowledgments cannot update departed or changed owners', async () => {
  for (const scenario of ['departed', 'switched', 'failed']) {
    const { page, bindings, settle } = harness(['hidden'])
    page.updateGroup(members, true)
    if (scenario === 'departed') page.depart()
    if (scenario === 'switched') bindings.selectionScopeKey.value++
    bindings.favoriteTheaterIds.value = ['new-owner']
    await settle(false)
    assert.deepEqual(bindings.draftFavoriteTheaterIds.value, [
      'hidden',
      'shown-a',
      'shown-b',
    ])
    assert.equal(
      bindings.statusMessage.value,
      scenario === 'failed' ? 'La sélection n’a pas pu être enregistrée.' : '',
    )
  }
})

test('mobile heading has native mixed checkbox only for multi-member groups; desktop pair stays', () => {
  const header = source.slice(
    source.indexOf('<header', source.indexOf('v-for="group in visibleGroups"')),
    source.indexOf(
      '</header>',
      source.indexOf('v-for="group in visibleGroups"'),
    ),
  )
  const checkbox = header.slice(
    header.indexOf('<label'),
    header.indexOf('</label>') + 8,
  )
  assert.match(checkbox, /v-if="group\.theaters\.length > 1"/)
  assert.match(checkbox, /size-11[^"]*lg:hidden/)
  assert.match(checkbox, /type="checkbox"/)
  assert.match(
    checkbox,
    /:checked="groupSelectionState\(group\.theaters\) === 'all'"/,
  )
  assert.match(
    checkbox,
    /:indeterminate\.prop="groupSelectionState\(group\.theaters\) === 'some'"/,
  )
  assert.match(checkbox, /:disabled="!preferencesReady \|\| writesBlocked"/)
  assert.match(
    checkbox,
    /:aria-label="[^"]*les cinémas affichés du groupe \$\{group\.city\}/,
  )
  assert.match(
    checkbox,
    /@change="updateGroup\(group\.theaters, groupSelectionState\(group\.theaters\) !== 'all'\)"/,
  )
  assert.match(checkbox, /peer-focus-visible:outline-3/)
  assert.match(checkbox, /<Minus\s+v-else-if=/)
  assert.doesNotMatch(checkbox, /NuxtLink/)
  assert.match(header, /max-w-full[^"]*overflow-wrap:anywhere/)
  assert.match(header, /class="hidden gap-2 lg:flex"/)
  assert.match(header, /@click="updateGroup\(group\.theaters, true\)"/)
  assert.match(header, /@click="updateGroup\(group\.theaters, false\)"/)
})
