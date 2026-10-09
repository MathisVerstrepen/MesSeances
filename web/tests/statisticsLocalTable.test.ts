import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { computed, createSSRApp, h, ref, watch, type Component } from 'vue'
import type { StatisticsResponse } from '../app/types/api.ts'
import * as statistics from '../app/utils/statistics.ts'

const source = await readFile(
  new URL('../app/components/StatisticsLocalTable.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const script = compileScript(descriptor, {
  id: 'local-table',
  inlineTemplate: true,
})
const compiled = ts.transpileModule(script.content, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText
const require = createRequire(import.meta.url)
const localRequire = (id: string) =>
  id === '~/utils/statistics' ? statistics : require(id)

interface ComponentModule {
  default?: Component
}
type LocalUiState =
  | 'cities'
  | 'theaters'
  | statistics.StatisticsLocalSort
  | number
  | null

async function render(
  local: StatisticsResponse['local'],
  mode: 'cities' | 'theaters',
  showMovieCount?: boolean,
  page = 1,
) {
  const exports: ComponentModule = {}
  // Supply Nuxt auto-imports and initial UI state to render both native-radio modes.
  const initialRef = (value: LocalUiState) =>
    ref(value === 'cities' ? mode : value === 1 ? page : value)
  new Function('require', 'exports', 'computed', 'ref', 'watch', compiled)(
    localRequire,
    exports,
    computed,
    initialRef,
    watch,
  )
  assert.ok(exports.default)
  const app = createSSRApp(exports.default, {
    local,
    showMovieCount,
    limits: { cities: true, theaters: true },
  })
  app.component('NuxtLink', {
    props: ['to'],
    setup:
      (props, { slots }) =>
      () =>
        h('a', { href: props.to }, slots.default?.()),
  })
  return renderToString(app)
}

const local: StatisticsResponse['local'] = {
  cities: [
    {
      slug: 'paris',
      name: 'PARIS',
      movie_count: 9,
      showtime_count: 12,
      theater_count: 3,
    },
  ],
  theaters: [
    {
      id: 'ugc-1',
      slug: 'ugc-paris',
      name: 'UGC PARIS',
      city: 'PARIS',
      city_slug: 'paris',
      chain: 'ugc',
      movie_count: 9,
      showtime_count: 12,
    },
  ],
}
const cells = (html: string, tag: 'th' | 'td') =>
  [...html.matchAll(new RegExp(`<${tag}\\b[^>]*>(.*?)<\\/${tag}>`, 'gs'))].map(
    (match) =>
      match[1]!
        .replace(/<[^>]*>/g, '')
        .replace(/[↑↓↕]/g, '')
        .trim(),
  )

for (const mode of ['cities', 'theaters'] as const) {
  test(`${mode}: SSR links use encoded canonical slugs and existing labels without changing row styles`, async () => {
    const slug = 'été /?#% cinéma'
    const data = {
      ...local,
      [mode]: [{ ...local[mode][0]!, slug }],
    }
    const before = structuredClone(data)
    for (const showMovieCount of [true, false]) {
      for (const [rows, expectedSlug] of [
        [local, local[mode][0]!.slug],
        [data, slug],
      ] as const) {
        const html = await render(rows, mode, showMovieCount)
        const anchors = [...html.matchAll(/<a\b([^>]*)>(.*?)<\/a>/gs)]
        assert.equal(anchors.length, 1)
        assert.match(
          anchors[0]![1]!,
          new RegExp(
            `href="${mode === 'cities' ? '/ville/' : '/cinema/'}${encodeURIComponent(expectedSlug)}${mode === 'cities' ? '/cinemas' : ''}"`,
          ),
        )
        assert.equal(
          anchors[0]![2]!.trim(),
          mode === 'cities' ? 'Paris' : 'UGC PARIS',
        )
        assert.doesNotMatch(
          anchors[0]![1]!,
          /(?:inline|block|flex|grid|leading-|min-h-|h-|p[xytrblse]?-|text-(?:xs|sm|base|lg|xl|\[))/,
        )
        assert.match(
          html,
          /<table class="w-full min-w-\[36rem\] border-collapse text-left text-sm">/,
        )
        assert.match(html, /<tr class="border-b border-ink\/20">/)
        assert.match(
          html,
          /<th scope="row" class="max-w-96 px-3 py-4 font-bold">/,
        )
        assert.equal(
          (html.match(/class="px-3 py-4/g) ?? []).length,
          showMovieCount ? 3 : 2,
        )
        assert.match(
          html,
          /type="radio" name="statistics-local-mode" value="cities"/,
        )
        assert.match(
          html,
          /type="radio" name="statistics-local-mode" value="theaters"/,
        )
        assert.match(
          html,
          new RegExp(
            `aria-label="Offre par ${mode === 'cities' ? 'ville' : 'cinéma'}, tableau défilant"`,
          ),
        )
        assert.doesNotMatch(html, /href="\/cinema\/ugc-1"/)
      }
      const empty = await render(
        { cities: [], theaters: [] },
        mode,
        showMovieCount,
      )
      assert.doesNotMatch(empty, /<a\b/)
      assert.match(empty, /Aucune donnée pour ces filtres\./)
    }
    assert.deepEqual(data, before)
  })

  test(`${mode}: movie column defaults visible, with existing row values and ranking copy`, async () => {
    const before = structuredClone(local)
    for (const showMovieCount of [undefined, true]) {
      const html = await render(local, mode, showMovieCount)
      assert.deepEqual(
        cells(html, 'th'),
        mode === 'cities'
          ? ['Ville', 'Films', 'Séances', 'Cinémas', 'Paris']
          : ['Cinéma', 'Films', 'Séances', 'Ville', 'UGC PARIS'],
      )
      assert.deepEqual(
        cells(html, 'td'),
        mode === 'cities' ? ['9', '12', '3'] : ['9', '12', 'Paris'],
      )
      assert.match(
        html,
        /Tri initial : séances puis films, par ordre décroissant\./,
      )
      assert.match(html, /classés initialement par séances puis films\./)
      assert.equal((html.match(/scope="col"/g) ?? []).length, 4)
      assert.match(html, /aria-sort="descending"/)
      assert.match(
        await render({ cities: [], theaters: [] }, mode, showMovieCount),
        /<td colspan="4"/,
      )
    }
    assert.deepEqual(local, before)
  })

  test(`${mode}: hidden movie column omits header, sort button and cells with aligned empty span and copy`, async () => {
    const html = await render(local, mode, false)
    assert.deepEqual(
      cells(html, 'th'),
      mode === 'cities'
        ? ['Ville', 'Séances', 'Cinémas', 'Paris']
        : ['Cinéma', 'Séances', 'Ville', 'UGC PARIS'],
    )
    assert.deepEqual(
      cells(html, 'td'),
      mode === 'cities' ? ['12', '3'] : ['12', 'Paris'],
    )
    assert.equal((html.match(/scope="col"/g) ?? []).length, 3)
    assert.doesNotMatch(html, /Films|puis films/)
    assert.match(html, /Tri initial : séances, par ordre décroissant\./)
    assert.match(html, /classés initialement par séances\./)
    assert.match(html, /Les totaux portent sur tous les résultats filtrés\./)
    assert.match(
      await render({ cities: [], theaters: [] }, mode, false),
      /<td colspan="3"/,
    )
  })

  test(`${mode}: hidden movie column retains screening ranking and pagination`, async () => {
    const rows = Array.from({ length: 25 }, (_, index) => ({
      ...local[mode][0]!,
      slug: `row-${index}`,
      id: `row-${index}`,
      showtime_count: index + 1,
    }))
    const before = structuredClone(rows)
    const data = { ...local, [mode]: rows }
    const first = await render(data, mode, false)
    const second = await render(data, mode, false, 2)
    assert.deepEqual(
      cells(first, 'td').filter((_, index) => index % 2 === 0),
      Array.from({ length: 20 }, (_, index) => String(25 - index)),
    )
    assert.deepEqual(
      cells(second, 'td').filter((_, index) => index % 2 === 0),
      ['5', '4', '3', '2', '1'],
    )
    assert.match(first, /Page 1 sur 2/)
    assert.match(second, /Page 2 sur 2/)
    assert.match(first, /aria-label="Pagination de l’offre locale"/)
    assert.match(first, /Précédent/)
    assert.match(first, /Suivant/)
    assert.deepEqual(rows, before)
  })
}
