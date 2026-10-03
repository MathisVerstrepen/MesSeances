import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import { computed, createSSRApp, ref, type Component } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import ts from 'typescript'
import type { Provider, Theater } from '../app/types/api.ts'
import * as chainHelpers from '../app/utils/cinemaDirectoryChains.ts'
import {
  cinemaDirectoryChains,
  filterCinemaDirectory,
  normalizeCinemaChains,
  toggleCinemaChain,
} from '../app/utils/cinemaDirectoryChains.ts'
import {
  cinemaDirectoryQuery,
  parseCinemaDirectoryQuery,
} from '../app/utils/cinemaDirectoryQuery.ts'
import {
  cinemaListSections,
  groupTheatersByCityIdentity,
  updateTheaterSelection,
} from '../app/utils/cinemaSelection.ts'
import { sortTheatersByDistance } from '../app/utils/theaterDistance.ts'
import * as theaterMap from '../app/utils/theaterMap.ts'

function theater(id: string, provider: Provider, city = 'Lille'): Theater {
  return {
    id,
    provider,
    slug: id,
    name: `${provider} Centre`,
    city,
    city_slug: city.toLowerCase(),
    address: '',
    postal_code: '',
    available_dates: [],
    accepted_passes: [],
    latitude: 50,
    longitude: 3,
  }
}
const directory = [
  theater('u', 'ugc'),
  theater('p', 'pathe'),
  theater('c', 'cgr', 'Paris'),
  theater('u-paris', 'ugc', 'Paris'),
]

test('chain query accepts all allowed identifiers, normalizes case, whitespace and duplicates', () => {
  const providers: Provider[] = [
    'ugc',
    'kinepolis',
    'pathe',
    'cgr',
    'megarama',
    'cineville',
    'mk2',
    'cinewest',
    'grandecran',
    'noecinemas',
  ]
  assert.deepEqual(normalizeCinemaChains(providers), providers)
  assert.deepEqual(
    parseCinemaDirectoryQuery({
      chains: [' CGR,ugc,unknown', null, 'UGC, pathe '],
    }).chains,
    ['ugc', 'pathe', 'cgr'],
  )
  const query = {
    chains: ['cgr,ugc', 'UGC'],
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  }
  const before = structuredClone(query)
  assert.deepEqual(cinemaDirectoryQuery(query), {
    chains: 'ugc,cgr',
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  })
  assert.deepEqual(query, before)
  assert.deepEqual(cinemaDirectoryQuery(query, { chains: [] }), {
    view: 'map',
    flag: null,
    tag: ['one', 'two'],
  })
  assert.equal(
    cinemaDirectoryQuery({}, { chains: ['cgr', 'ugc', 'cgr'] }).chains,
    'ugc,cgr',
  )
  for (const chains of [
    undefined,
    null,
    '',
    'all',
    'invalid',
    [null, 'invalid'],
  ]) {
    assert.deepEqual(cinemaDirectoryQuery({ chains }), {})
    assert.deepEqual(parseCinemaDirectoryQuery({ chains }).chains, [])
  }
})

test('chain options use full directory and selections are a union intersected with search', () => {
  const before = structuredClone(directory)
  assert.deepEqual(cinemaDirectoryChains(directory), ['cgr', 'pathe', 'ugc'])
  assert.deepEqual(cinemaDirectoryChains([]), [])
  assert.deepEqual(filterCinemaDirectory(directory, '', []), directory)
  assert.deepEqual(
    filterCinemaDirectory(directory, '', ['ugc']).map((row) => row.id),
    ['u', 'u-paris'],
  )
  assert.deepEqual(
    filterCinemaDirectory(directory, ' LILLE ', ['ugc', 'pathe']).map(
      (row) => row.id,
    ),
    ['u', 'p'],
  )
  assert.deepEqual(filterCinemaDirectory(directory, 'cgr', ['ugc']), [])
  assert.deepEqual(filterCinemaDirectory(directory, '', ['kinepolis']), [])
  assert.deepEqual(filterCinemaDirectory([], '', []), [])
  assert.deepEqual(directory, before)
})

test('selection toggles preserve other chains and removing final checkbox restores all', () => {
  const selected: Provider[] = ['ugc']
  const next = toggleCinemaChain(selected, 'cgr')
  assert.deepEqual(selected, ['ugc'])
  assert.deepEqual(next, ['ugc', 'cgr'])
  assert.deepEqual(toggleCinemaChain(next, 'ugc'), ['cgr'])
  const cleared = toggleCinemaChain(['cgr'], 'cgr')
  assert.deepEqual(cleared, [])
  assert.deepEqual(filterCinemaDirectory(directory, '', cleared), directory)
})

test('filtered list, nearby rows and bulk actions keep hidden favorites intact', () => {
  const favorites = ['c', 'u-paris', 'missing']
  const visible = filterCinemaDirectory(directory, 'Lille', ['ugc', 'pathe'])
  const sections = cinemaListSections(
    visible,
    new Set(['u', 'c']),
    (row) => row.id,
  )
  assert.deepEqual(
    sections.map((section) => section.rows.map((row) => row.id)),
    [['u'], ['u', 'p']],
  )
  assert.deepEqual(groupTheatersByCityIdentity(visible)[0]?.theaters, visible)
  assert.deepEqual(
    sortTheatersByDistance(visible, { latitude: 50, longitude: 3 }).map(
      (row) => row.theater.id,
    ),
    ['p', 'u'],
  )
  const selected = updateTheaterSelection(favorites, visible, true)
  assert.deepEqual(selected, ['c', 'u-paris', 'missing', 'u', 'p'])
  assert.deepEqual(updateTheaterSelection(selected, visible, false), favorites)
  assert.deepEqual(favorites, ['c', 'u-paris', 'missing'])
})

test('chain URL history restores selection and retains search, modes and unrelated keys', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/cinemas', component: {} }],
  })
  await router.push('/cinemas?q=Lille&view=map&location=nearby&tag=kept')
  const query = () => router.currentRoute.value.query
  async function traverse(direction: 'back' | 'forward') {
    await new Promise<void>((resolve) => {
      const remove = router.afterEach(() => {
        remove()
        resolve()
      })
      router[direction]()
    })
  }
  await router.push({
    query: cinemaDirectoryQuery(query(), { chains: ['ugc'] }),
  })
  await router.push({
    query: cinemaDirectoryQuery(query(), { chains: ['ugc', 'cgr'] }),
  })
  await router.replace({
    query: cinemaDirectoryQuery(query(), { search: 'Paris' }),
  })
  await router.push({
    query: cinemaDirectoryQuery(query(), { view: 'list', location: 'city' }),
  })
  assert.deepEqual(parseCinemaDirectoryQuery(query()), {
    search: 'Paris',
    view: 'list',
    location: 'city',
    chains: ['ugc', 'cgr'],
  })
  await traverse('back')
  assert.deepEqual(parseCinemaDirectoryQuery(query()), {
    search: 'Paris',
    view: 'map',
    location: 'nearby',
    chains: ['ugc', 'cgr'],
  })
  await traverse('back')
  assert.deepEqual(parseCinemaDirectoryQuery(query()), {
    search: 'Lille',
    view: 'map',
    location: 'nearby',
    chains: ['ugc'],
  })
  await traverse('forward')
  assert.equal(query().tag, 'kept')
  await router.push({ query: cinemaDirectoryQuery(query(), { chains: [] }) })
  assert.equal(query().chains, undefined)
  assert.equal(query().q, 'Paris')
  assert.equal(query().view, 'map')
  assert.equal(query().location, 'nearby')
})

const require = createRequire(import.meta.url)
const source = (name: string) =>
  readFile(new URL(`../app/components/${name}.vue`, import.meta.url), 'utf8')
interface ComponentModule {
  default?: Component
}
async function component(name: string): Promise<Component> {
  const { descriptor } = parse(await source(name))
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: ComponentModule = {}
  function localRequire(id: string) {
    if (id === '~/utils/cinemaDirectoryChains') return chainHelpers
    if (id === '~/utils/theaterMap') return theaterMap
    if (id.startsWith('~/assets/')) return { __esModule: true, default: id }
    return require(id)
  }
  new Function('require', 'exports', 'computed', compiled)(
    localRequire,
    exports,
    computed,
  )
  assert.ok(exports.default)
  return exports.default
}

const [CinemaChainFilter, TheaterName, BrandLogo] = await Promise.all(
  ['CinemaChainFilter', 'TheaterName', 'BrandLogo'].map(component),
)
async function render(
  selected: Provider[],
  options = cinemaDirectoryChains(directory),
  inSettings = false,
) {
  const app = createSSRApp(CinemaChainFilter!, {
    modelValue: selected,
    options,
    inSettings,
  })
  app.component('TheaterName', TheaterName!)
  app.component('BrandLogo', BrandLogo!)
  return renderToString(app)
}

test('filter renders real checkbox boxes, existing logos and readable labels without redundant brand alt text', async () => {
  const html = await render(['ugc'])
  assert.match(html, />UGC<\/span>/)
  assert.match(html, /<legend class="sr-only">Filtrer par enseigne<\/legend>/)
  assert.equal((html.match(/type="checkbox"/g) ?? []).length, 3)
  assert.equal((html.match(/<img /g) ?? []).length, 3)
  assert.match(html, /value="ugc" checked/)
  assert.doesNotMatch(html, /value="(?:cgr|pathe)" checked/)
  for (const provider of ['ugc', 'cgr', 'pathe'])
    assert.ok(html.includes(`${provider}_logo_small.webp`))
  assert.match(html, /alt(?:="")? aria-hidden="true"/)
  assert.match(html, /Pathé/)
  assert.match(html, /has-focus-visible:outline-3/)
  assert.match(html, /bg-\[#fff3c4\] shadow-\[inset_0_0_0_1px/)
  assert.match(html, /peer sr-only/)
  assert.match(html, /peer-checked:bg-ink peer-checked:text-white/)
  assert.match(html, /!h-6 !w-12/)
})

test('default summary and clear button reflect no selection; selected unavailable provider remains clearable', async () => {
  const empty = await render([])
  assert.match(empty, /Toutes les enseignes/)
  assert.doesNotMatch(empty, /value="[^"]+" checked/)
  assert.match(empty, /<button[^>]* disabled(?:\s|>)/)
  const multiple = await render(['ugc', 'cgr'])
  assert.match(multiple, /2 enseignes/)
  assert.doesNotMatch(multiple, /<button[^>]* disabled(?:\s|>)/)
  const absent = await render(['kinepolis'])
  assert.match(absent, />Kinepolis<\/span>/)
  assert.doesNotMatch(absent, /<button[^>]* disabled(?:\s|>)/)
})

test('desktop has compact named popover; mobile exposes same options as flat always-visible section', async () => {
  const desktop = await render([])
  assert.match(desktop, /<details class="group relative min-w-0"/)
  assert.match(desktop, /<fieldset[^>]*fixed z-30/)
  assert.match(desktop, /max-h-\[440px\]/)
  assert.match(desktop, /w-\[350px\]/)
  assert.match(desktop, /max-w-\[calc\(100vw-1rem\)\]/)
  assert.match(desktop, /overflow-y-auto overscroll-contain/)
  assert.match(desktop, /grid grid-cols-2 gap-2/)
  assert.match(desktop, /min-h-16/)
  assert.match(desktop, /<h3[^>]*>Enseignes<\/h3>/)
  assert.match(desktop, /Réinitialiser/)
  const mobile = await render([], undefined, true)
  assert.match(mobile, /<section class="group relative min-w-0 w-full"/)
  assert.match(mobile, /<fieldset class="min-w-0 w-full"/)
  assert.doesNotMatch(
    mobile,
    /<summary|<details|fixed z-30|overflow-y-auto|role="dialog"/,
  )
  assert.equal((mobile.match(/type="checkbox"/g) ?? []).length, 3)
})

test('page shares filtered results and places one filter beside bulk actions inside mobile settings teleport', async () => {
  const page = await readFile(
    new URL('../app/pages/cinemas.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    page,
    /const selectedChains = computed\(\(\) => routeState.value.chains\)/,
  )
  assert.match(page, /cinemaDirectoryChains\(directoryTheaters.value\)/)
  assert.match(
    page,
    /filterCinemaDirectory\(\s*directoryTheaters.value,\s*search.value,\s*selectedChains.value/,
  )
  assert.match(page, /const displayedTheaters = searchResults/)
  assert.match(
    page,
    /sortTheatersByDistance\(searchResults.value, userPosition.value\)/,
  )
  assert.match(
    page,
    /const visibleTheaterCount = computed\(\(\) => displayedTheaters.value.length\)/,
  )
  assert.match(page, /:theaters="displayedTheaters"/)
  assert.match(
    page,
    /function updateDisplayedSelection[\s\S]*?draftFavoriteTheaterIds.value,\s*displayedTheaters.value/,
  )
  assert.match(
    page,
    /function setChains\(chains: Provider\[\]\) \{\s*setDisplayMode\(\{ chains \}\)/,
  )
  const searchWorkspace = page.slice(
    page.indexOf('class="search-workspace'),
    page.indexOf(
      '<Teleport to="#cinema-settings-location" :disabled="!settingsOpen">',
      page.indexOf('class="search-workspace'),
    ),
  )
  assert.doesNotMatch(searchWorkspace, /<CinemaChainFilter/)
  assert.equal((page.match(/<CinemaChainFilter/g) ?? []).length, 1)
  assert.match(page, /textarea, summary, \[tabindex\]/)
  assert.match(
    page,
    /<Teleport to="#cinema-settings-controls" :disabled="!settingsOpen">[\s\S]*?class="selection-controls[^>]*>\s*<CinemaChainFilter[\s\S]*?:in-settings="settingsOpen"[\s\S]*?@update:model-value="setChains"[\s\S]*?class="bulk-actions/,
  )
  assert.match(page, /@click="resetFilters"/)
  assert.doesNotMatch(
    page,
    /function setChains[\s\S]*?setFavoriteTheaterIds[\s\S]*?function resetFilters/,
  )
  const filter = await source('CinemaChainFilter')
  assert.match(filter, /@keydown.esc="close"/)
  assert.match(
    filter,
    /event.preventDefault\(\)[\s\S]*event.stopPropagation\(\)[\s\S]*resetDisclosure\(\)[\s\S]*summary.value\?\.focus\(\{ preventScroll: true \}\)/,
  )
  assert.match(filter, /@focusout="closeOnFocusLeave"/)
  assert.match(filter, /removeEventListener\('pointerdown', closeOutside\)/)
  assert.match(
    filter,
    /watch\(\(\) => props.inSettings, resetDisclosure, \{ flush: 'sync' \}\)/,
  )
  assert.match(filter, /window.matchMedia\('\(min-width: 1024px\)'\)/)
  assert.match(filter, /addEventListener\('change', resetDisclosure\)/)
  assert.match(filter, /removeEventListener\('change', resetDisclosure\)/)
  assert.match(
    filter,
    /removeEventListener\('scroll', schedulePosition, true\)/,
  )
  assert.match(filter, /removeEventListener\('resize', schedulePosition\)/)
  assert.match(filter, /cancelAnimationFrame\(frame\)/)
  assert.match(filter, /pointer-events-none[^"]*bottom-0/)
})

async function positioningHarness() {
  const script = (await source('CinemaChainFilter')).match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )![1]!
  const parsed = ts.createSourceFile(
    'filter.ts',
    script,
    ts.ScriptTarget.Latest,
    true,
  )
  const names = ['isOpen', 'positionPanel', 'updateOverflow']
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
  class Details {
    open = true
  }
  const anchor = { top: 100, bottom: 144, right: 1400 }
  const bounds = { width: 350, height: 432 }
  const scroll = { scrollHeight: 360, clientHeight: 360, scrollTop: 0 }
  const bindings = {
    disclosure: { value: new Details() },
    summary: { value: { getBoundingClientRect: () => anchor } },
    panel: { value: { getBoundingClientRect: () => bounds } },
    optionScroll: { value: scroll },
    panelStyle: ref<{ left: string; top: string; maxHeight: string }>(),
    overflowRemaining: { value: false },
    props: { inSettings: false },
    window: { innerHeight: 900, innerWidth: 1440 },
    HTMLDetailsElement: Details,
    nextTick: (callback: () => void) => callback(),
    resetDisclosure: () => {
      bindings.disclosure.value.open = false
      bindings.panelStyle.value = undefined
    },
  }
  // SAFETY: Execute only extracted positioning functions with synthetic geometry.
  const filter = new Function(
    ...Object.keys(bindings),
    `${code}\nreturn { positionPanel, updateOverflow }`,
  )(...Object.values(bindings)) as {
    positionPanel: () => void
    updateOverflow: () => void
  }
  return { filter, bindings, anchor, bounds, scroll }
}

test('desktop positions below when it fits and above near viewport bottom, updating after scroll/resize', async () => {
  const { filter, bindings, anchor } = await positioningHarness()
  filter.positionPanel()
  assert.deepEqual(bindings.panelStyle.value, {
    left: '1050px',
    top: '152px',
    maxHeight: '432px',
  })
  anchor.top = 780
  anchor.bottom = 824
  filter.positionPanel()
  assert.equal(bindings.panelStyle.value?.top, '340px')
  anchor.top = 250
  anchor.bottom = 294
  anchor.right = 300
  bindings.window.innerHeight = 500
  bindings.window.innerWidth = 360
  filter.positionPanel()
  assert.deepEqual(bindings.panelStyle.value, {
    left: '8px',
    top: '8px',
    maxHeight: '234px',
  })
  anchor.top = 550
  anchor.bottom = 594
  filter.positionPanel()
  assert.equal(bindings.disclosure.value.open, false)
  assert.equal(bindings.panelStyle.value, undefined)
  bindings.disclosure.value.open = false
  bindings.panelStyle.value = undefined
  filter.positionPanel()
  assert.equal(bindings.panelStyle.value, undefined)
})

test('overflow cue appears only with remaining desktop content and disappears at bottom or in mobile section', async () => {
  const { filter, bindings, scroll } = await positioningHarness()
  filter.updateOverflow()
  assert.equal(bindings.overflowRemaining.value, false)
  scroll.clientHeight = 200
  filter.updateOverflow()
  assert.equal(bindings.overflowRemaining.value, true)
  scroll.scrollTop = 160
  filter.updateOverflow()
  assert.equal(bindings.overflowRemaining.value, false)
  scroll.scrollTop = 0
  bindings.props.inSettings = true
  filter.updateOverflow()
  assert.equal(bindings.overflowRemaining.value, false)
})
