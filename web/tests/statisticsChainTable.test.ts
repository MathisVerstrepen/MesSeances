import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { computed, createSSRApp, type Component } from 'vue'
import type { HistoryChainRank, Provider } from '../app/types/api.ts'
import {
  statisticsChainLabels,
  statisticsCount,
} from '../app/utils/statistics.ts'

const require = createRequire(import.meta.url)
const source = await readFile(
  new URL('../app/components/StatisticsChainTable.vue', import.meta.url),
  'utf8',
)
interface ComponentModule {
  default?: Component
}

// Render real SFCs, supplying Nuxt's computed auto-import and local bundled asset URLs.
async function component(name: string): Promise<Component> {
  const { descriptor } = parse(
    await readFile(
      new URL(`../app/components/${name}.vue`, import.meta.url),
      'utf8',
    ),
  )
  const script = compileScript(descriptor, { id: name, inlineTemplate: true })
  const compiled = ts.transpileModule(script.content, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText
  const exports: ComponentModule = {}
  const localRequire = (id: string) => {
    if (id === '~/utils/statistics')
      return { statisticsChainLabels, statisticsCount }
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

const Table = await component('StatisticsChainTable')
const TheaterName = await component('TheaterName')
const BrandLogo = await component('BrandLogo')
const render = (rows: HistoryChainRank[]) => {
  const app = createSSRApp(Table, { rows })
  app.component('TheaterName', TheaterName)
  app.component('BrandLogo', BrandLogo)
  return renderToString(app)
}
const cells = (html: string, tag: 'th' | 'td') =>
  [...html.matchAll(new RegExp(`<${tag}\\b[^>]*>(.*?)<\\/${tag}>`, 'gs'))].map(
    (match) => match[1]!.replace(/<[^>]*>/g, '').trim(),
  )

test('SSR preserves provider order and exact French counts without mutating rows', async () => {
  const rows: HistoryChainRank[] = [
    {
      chain: 'noecinemas',
      showtime_count: 12,
      movie_count: 3,
      theater_count: 2,
    },
    {
      chain: 'pathe',
      showtime_count: 1_234_567,
      movie_count: 2345,
      theater_count: 1234,
    },
    { chain: 'ugc', showtime_count: 45, movie_count: 6, theater_count: 7 },
    {
      chain: 'grandecran',
      showtime_count: 9,
      movie_count: 8,
      theater_count: 1,
    },
  ]
  const before = structuredClone(rows)
  const html = await render(rows)
  assert.deepEqual(rows, before)
  assert.deepEqual(cells(html, 'th'), [
    'Circuit',
    'Séances',
    'Films',
    'Cinémas',
    'Noé Cinémas',
    'Pathé',
    'UGC',
    'Grand Écran',
  ])
  assert.deepEqual(
    cells(html, 'td'),
    rows.flatMap((row) => [
      statisticsCount(row.showtime_count),
      statisticsCount(row.movie_count),
      statisticsCount(row.theater_count),
    ]),
  )
  assert.ok(html.includes('1\u202f234\u202f567'))
  assert.equal((html.match(/scope="row"/g) ?? []).length, rows.length)
  assert.equal(
    (html.match(/text-right font-mono tabular-nums/g) ?? []).length,
    rows.length * 3,
  )
  assert.match(source, /v-for="row in rows" :key="row.chain"/)
  assert.doesNotMatch(html, /<button|<select|<a\b|Aucune donnée/)
})

test('SSR exposes scoped headers, ranking caption and keyboard-scroll region', async () => {
  const html = await render([
    { chain: 'cineville', showtime_count: 5, movie_count: 4, theater_count: 3 },
  ])
  assert.match(html, /<table\b/)
  assert.match(
    html,
    /<caption class="sr-only">\s*Circuits classés par séances puis films, par ordre décroissant\.\s*<\/caption>/,
  )
  assert.match(
    html,
    /role="region" aria-label="Statistiques par circuit, tableau défilant" tabindex="0"/,
  )
  assert.match(
    html,
    /overflow-x-auto focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink/,
  )
  assert.equal((html.match(/scope="col"/g) ?? []).length, 4)
  assert.equal((html.match(/aria-sort=/g) ?? []).length, 1)
  assert.match(
    html,
    /<th scope="col" aria-sort="descending"[^>]*>\s*Séances\s*<\/th>/,
  )
  assert.match(
    html,
    /<th scope="row"[^>]*whitespace-normal[^>]*><span><img\b[^>]*> Cinéville<\/span><\/th>/,
  )
  assert.deepEqual(cells(html, 'td'), ['5', '4', '3'])
  assert.equal((html.match(/scope="row"/g) ?? []).length, 1)
})

const providerLogos = {
  ugc: ['UGC', 'ugc_logo_small.webp'],
  kinepolis: ['Kinepolis', 'kinepolis_logo_small.webp'],
  pathe: ['Pathé', 'pathe_logo_small.webp'],
  cgr: ['CGR', 'cgr_logo_small.webp'],
  megarama: ['Megarama', 'megarama_logo_small.webp'],
  cineville: ['Cinéville', 'cineville_logo_small.webp'],
  mk2: ['MK2', 'mk2_logo.svg'],
  cinewest: ['CinéWest', 'cinewest_logo_small.webp'],
  grandecran: ['Grand Écran', 'grand_ecran_logo_small.webp'],
  noecinemas: ['Noé Cinémas', 'noe_cinema_logo_small.webp'],
} satisfies Record<Provider, [string, string]>

// SAFETY: providerLogos is a local literal checked exhaustively against Provider above.
const providers = Object.keys(providerLogos) as Provider[]

for (const provider of providers) {
  test(`${provider}: Circuit cell keeps its label and one decorative bundled inline logo`, async () => {
    const html = await render([
      { chain: provider, showtime_count: 5, movie_count: 4, theater_count: 3 },
    ])
    const [label, asset] = providerLogos[provider]
    const header = html.match(/<th scope="row"[^>]*>(.*?)<\/th>/s)?.[1]
    assert.ok(header)
    assert.equal((html.match(/<img\b/g) ?? []).length, 1)
    assert.ok(header.includes(`src="~/assets/imgs/${asset}?no-inline"`))
    assert.match(header, /<img\b[^>]*alt(?:="")? aria-hidden="true"/)
    assert.ok(header.endsWith(` ${label}</span>`))
    assert.doesNotMatch(
      header,
      /<(?:span|th)\b[^>]*aria-hidden|aria-label=|https?:\/\//,
    )
    assert.deepEqual(cells(html, 'th'), [
      'Circuit',
      'Séances',
      'Films',
      'Cinémas',
      label,
    ])
  })
}

test('SSR keeps the table and spans all columns for empty results', async () => {
  const html = await render([])
  assert.match(html, /<table\b/)
  assert.match(
    html,
    /<td colspan="4"[^>]*>Aucune donnée pour ces filtres\.<\/td>/,
  )
  assert.doesNotMatch(html, /scope="row"|<img\b/)
  assert.deepEqual(cells(html, 'td'), ['Aucune donnée pour ces filtres.'])
})

test('page passes history chains directly before local rankings within nonzero details', async () => {
  const page = await readFile(
    new URL('../app/pages/statistiques.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    page,
    /<template v-else>\s*<section[^>]*aria-labelledby="statistics-daily"/,
  )
  assert.match(
    page,
    /<section :class="sectionClass" aria-labelledby="statistics-chains">\s*<h2 id="statistics-chains" :class="headingClass">Par circuit<\/h2>\s*<StatisticsChainTable :rows="data.chains"\s*\/>\s*<\/section>\s*<section[^>]*aria-labelledby="statistics-local"/,
  )
  const totals = page.indexOf('id="statistics-totals"')
  const empty = page.indexOf('v-if="data.totals.showtimes === 0"')
  const daily = page.indexOf('id="statistics-daily"')
  const chains = page.indexOf('id="statistics-chains"')
  const local = page.indexOf('id="statistics-local"')
  assert.ok(
    totals >= 0 &&
      totals < empty &&
      empty < daily &&
      daily < chains &&
      chains < local,
  )
  assert.equal((page.match(/<StatisticsChainTable\b/g) ?? []).length, 1)
})
