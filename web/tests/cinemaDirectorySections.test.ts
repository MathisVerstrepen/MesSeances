import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import ts from 'typescript'
import { computed, ref } from 'vue'
import type { Theater } from '../app/types/api.ts'
import {
  cinemaListSections,
  groupTheatersByCityIdentity,
} from '../app/utils/cinemaSelection.ts'
import { sortTheatersByDistance } from '../app/utils/theaterDistance.ts'

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
  'selectedIds',
  'displayedTheaters',
  'nearbyRows',
  'listSections',
  'visibleTheaterCount',
]
const nodes = parsed.statements.filter(
  (node) =>
    ts.isVariableStatement(node) &&
    node.declarationList.declarations.some(
      (d) => ts.isIdentifier(d.name) && names.includes(d.name.text),
    ),
)
assert.equal(nodes.length, names.length)
const code = ts.transpileModule(
  nodes.map((node) => node.getFullText(parsed)).join('\n'),
  { compilerOptions: { target: ts.ScriptTarget.ES2022 } },
).outputText

function theater(id: string, city: string, latitude: number): Theater {
  return {
    id,
    slug: id,
    provider: 'ugc',
    name: id,
    city,
    city_slug: city.toLowerCase(),
    address: '',
    postal_code: '75000',
    latitude,
    longitude: 2.35,
    available_dates: [],
    accepted_passes: [],
  }
}
const catalog = [
  theater('a', 'Paris', 48.85),
  theater('b', 'Paris', 48.9),
  theater('c', 'Lille', 49.1),
]
function harness(ids = ['b']) {
  const bindings = {
    computed,
    cinemaListSections,
    groupTheatersByCityIdentity,
    sortTheatersByDistance,
    searchResults: ref(catalog),
    draftFavoriteTheaterIds: ref(ids),
    userPosition: ref({ latitude: 48.85, longitude: 2.35 }),
  }
  // SAFETY: Actual page derivations execute with explicit synthetic reactive bindings only.
  const page = new Function(
    ...Object.keys(bindings),
    `${code}\nreturn { displayedTheaters, listSections, visibleTheaterCount }`,
  )(...Object.values(bindings)) as {
    displayedTheaters: { value: Theater[] }
    listSections: {
      value: {
        key: string
        groups: ReturnType<typeof groupTheatersByCityIdentity>
        nearbyRows: ReturnType<typeof sortTheatersByDistance>
      }[]
    }
    visibleTheaterCount: { value: number }
  }
  return { bindings, page }
}

test('actual page keeps full city and distance order while summary shares checked members', () => {
  const { bindings, page } = harness()
  const all = () => page.listSections.value.find((s) => s.key === 'all')!
  assert.deepEqual(
    all().groups.map((g) => g.theaters.map((t) => t.id)),
    [['a', 'b'], ['c']],
  )
  assert.deepEqual(
    page.listSections.value[0]!.nearbyRows.map((r) => [
      r.theater.id,
      r.isNearest,
    ]),
    [['b', false]],
  )
  const order = all().nearbyRows.map((r) => r.theater.id)
  for (const ids of [['b', 'c'], ['c'], [], ['a', 'b', 'c']]) {
    bindings.draftFavoriteTheaterIds.value = ids
    assert.deepEqual(
      all().nearbyRows.map((r) => r.theater.id),
      order,
    )
    assert.deepEqual(
      all().groups.map((g) => g.theaters.map((t) => t.id)),
      [['a', 'b'], ['c']],
    )
    assert.equal(page.visibleTheaterCount.value, 3)
    assert.equal(page.displayedTheaters.value, bindings.searchResults.value)
  }
})

test('search scopes summary and full inventory without hidden IDs or nearest inflation', () => {
  const { bindings, page } = harness(['b', 'c'])
  bindings.searchResults.value = catalog.slice(0, 2)
  assert.deepEqual(
    page.listSections.value.map((s) => s.nearbyRows.map((r) => r.theater.id)),
    [['b'], ['a', 'b']],
  )
  assert.equal(page.visibleTheaterCount.value, 2)
  assert.deepEqual(
    page.listSections.value.map((s) => s.key),
    ['selected', 'all'],
  )
  assert.equal(page.listSections.value[0]!.nearbyRows[0]!.isNearest, false)
  assert.equal(page.visibleTheaterCount.value, 2)
  bindings.draftFavoriteTheaterIds.value = ['c']
  assert.deepEqual(
    page.listSections.value.map((s) => s.key),
    ['all'],
  )
  assert.deepEqual(bindings.draftFavoriteTheaterIds.value, ['c'])
  assert.equal(page.visibleTheaterCount.value, 2)
  assert.equal(page.displayedTheaters.value, bindings.searchResults.value)
})

test('one keyed renderer preserves section/city/row identity and unique map/count/SEO inputs', () => {
  assert.match(source, /v-for="section in listSections"\s*:key="section\.key"/)
  assert.match(
    source,
    /v-for="group in section\.groups"\s*:key="group\.citySlug"/,
  )
  assert.match(
    source,
    /v-for="theater in group\.theaters"\s*:key="theater\.id"/,
  )
  assert.match(
    source,
    /v-for="row in section\.nearbyRows"\s*:key="row\.theater\.id"/,
  )
  assert.match(source, /Cinémas sélectionnés/)
  assert.match(source, /Tous les cinémas/)
  assert.match(
    source,
    /:aria-labelledby="`cinema-section-\$\{section\.key\}-title`"/,
  )
  assert.match(source, /<h3\s+:id="`cinema-section-/)
  assert.match(source, /<h4[\s\S]*?group\.city[\s\S]*?<\/h4>/)
  assert.equal(
    source.match(/@change="toggleTheater\(theater\.id, \$event\)"/g)?.length,
    1,
  )
  assert.match(source, /:theaters="displayedTheaters"/)
  assert.match(source, /const theaters = searchResults\.value/)
  assert.doesNotMatch(
    source,
    /selectedFirst|groupSelectedTheatersFirst|scrollIntoView|selectedOnly|ListFilter|Sélectionnés uniquement|Afficher tous les cinémas/,
  )
})

const toggleNode = parsed.statements.find(
  (node) =>
    ts.isFunctionDeclaration(node) && node.name?.text === 'toggleTheater',
)!
const toggleCode = ts.transpileModule(toggleNode.getFullText(parsed), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText

function interactionHarness({ summary = false, blocked = false } = {}) {
  let resolve!: () => void
  const pending = new Promise<void>((done) => {
    resolve = done
  })
  const scrolls: number[] = []
  const focuses: { preventScroll: boolean }[] = []
  const writes: string[][] = []
  const row = {
    isConnected: true,
    top: 100,
    getBoundingClientRect() {
      return { top: this.top }
    },
  }
  const document = { body: {}, activeElement: {} }
  class Element {
    isConnected = true
    closest(selector: string) {
      return selector === '#cinema-section-all' ? (summary ? null : {}) : row
    }
    focus(options: { preventScroll: boolean }) {
      focuses.push(options)
      document.activeElement = this
    }
  }
  const input = new Element()
  document.activeElement = input
  const window = {
    scrollY: 1000,
    scrollBy({ top }: { top: number }) {
      scrolls.push(top)
      this.scrollY += top
      row.top -= top
    },
  }
  const bindings = {
    HTMLElement: Element,
    document,
    window,
    writesBlocked: ref(blocked),
    isUnmounted: false,
    directoryTheaters: ref(catalog),
    selectedIds: ref(new Set(['a'])),
    draftFavoriteTheaterIds: ref(['a']),
    selectionScopeKey: ref(0),
    nextTick: () => Promise.resolve(),
    updateTheaterSelection: (
      ids: string[],
      rows: Theater[],
      select: boolean,
    ) =>
      select
        ? [...ids, ...rows.map((t) => t.id)]
        : ids.filter((id) => !rows.some((t) => t.id === id)),
    applyDraftSelection: (ids: string[]) => {
      writes.push(ids)
      row.top = 300
      document.activeElement = document.body
      return pending
    },
  }
  // SAFETY: Actual row handler runs only against explicit synthetic DOM/state bindings.
  const page = new Function(
    ...Object.keys(bindings),
    `${toggleCode}\nreturn { toggleTheater, depart: () => { isUnmounted = true } }`,
  )(...Object.values(bindings)) as {
    toggleTheater: (
      id: string,
      event?: { currentTarget: Element },
    ) => Promise<void>
    depart: () => void
  }
  return {
    bindings,
    page,
    input,
    row,
    document,
    window,
    scrolls,
    focuses,
    writes,
    resolve,
  }
}

test('full-list action bounds correction to two renders and restores original focus without scrolling', async () => {
  const h = interactionHarness()
  const done = h.page.toggleTheater('b', { currentTarget: h.input })
  await Promise.resolve()
  assert.deepEqual(h.writes, [['a', 'b']])
  assert.deepEqual(h.scrolls, [200])
  h.row.top = 150
  h.resolve()
  await done
  assert.deepEqual(h.scrolls, [200, 50])
  assert.deepEqual(h.focuses, [{ preventScroll: true }])
  assert.equal(h.document.activeElement, h.input)
})

test('owner/unmount/disconnected/focus fences prevent stale action correction or focus theft', async () => {
  for (const scenario of [
    'owner',
    'unmounted',
    'disconnected',
    'focus-moved',
    'readonly',
  ]) {
    const h = interactionHarness()
    const done = h.page.toggleTheater('b', { currentTarget: h.input })
    if (scenario === 'owner') h.bindings.selectionScopeKey.value++
    if (scenario === 'unmounted') h.page.depart()
    if (scenario === 'disconnected') {
      h.row.isConnected = false
      h.input.isConnected = false
    }
    if (scenario === 'focus-moved') h.document.activeElement = {}
    if (scenario === 'readonly') h.bindings.writesBlocked.value = true
    await Promise.resolve()
    h.resolve()
    await done
    assert.deepEqual(h.focuses, [], scenario)
    if (scenario !== 'readonly') assert.deepEqual(h.scrolls, [], scenario)
  }
})

test('user scroll during save suppresses final correction; summary and map never anchor full list', async () => {
  const h = interactionHarness()
  const done = h.page.toggleTheater('b', { currentTarget: h.input })
  await Promise.resolve()
  h.window.scrollY += 100
  h.row.top = 50
  h.resolve()
  await done
  assert.deepEqual(h.scrolls, [200])
  for (const mode of ['summary', 'map']) {
    const other = interactionHarness({ summary: true })
    const done = other.page.toggleTheater(
      'b',
      mode === 'map' ? undefined : { currentTarget: other.input },
    )
    other.resolve()
    await done
    assert.deepEqual(other.scrolls, [])
    assert.equal(other.writes.length, 1)
  }
  const blocked = interactionHarness({ blocked: true })
  await blocked.page.toggleTheater('b', { currentTarget: blocked.input })
  assert.deepEqual(blocked.writes, [])
  assert.deepEqual(blocked.scrolls, [])
})
