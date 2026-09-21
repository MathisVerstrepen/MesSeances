import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import { computed, createSSRApp, type Component } from 'vue'
import type { StatisticsDailyShowtimes } from '../app/types/api.ts'
import { statisticsCount } from '../app/utils/statistics.ts'
import { statisticsLineChart } from '../app/utils/statisticsLineChart.ts'

const require = createRequire(import.meta.url)
const source = await readFile(
  new URL('../app/components/StatisticsLineChart.vue', import.meta.url),
  'utf8',
)
const { descriptor } = parse(source)
const script = compileScript(descriptor, {
  id: 'statistics-line-chart',
  inlineTemplate: true,
})
const compiled = ts.transpileModule(script.content, {
  compilerOptions: {
    module: ts.ModuleKind.CommonJS,
    target: ts.ScriptTarget.ES2022,
  },
}).outputText
interface ComponentModule {
  default?: Component
}
const exports: ComponentModule = {}
new Function('require', 'exports', 'computed', compiled)(
  (id: string) => {
    if (id === '~/utils/statistics') return { statisticsCount }
    if (id === '~/utils/statisticsLineChart') return { statisticsLineChart }
    return require(id)
  },
  exports,
  computed,
)
assert.ok(exports.default)
const Chart = exports.default
const render = (rows: StatisticsDailyShowtimes[]) =>
  renderToString(createSSRApp(Chart, { rows }))

test('daily chart keeps exact counts, calendar spacing and zero-count days without mutating rows', () => {
  const rows = [
    { date: '2026-03-28', showtime_count: 12 },
    { date: '2026-03-29', showtime_count: 0 },
    { date: '2026-03-30', showtime_count: 6 },
  ]
  const before = structuredClone(rows)
  const chart = statisticsLineChart(rows)
  assert.deepEqual(rows, before)
  assert.equal(chart.line, '0,0 50,100 100,50')
  assert.deepEqual(
    chart.ticks.map((tick) => tick.count),
    [12, 9, 6, 3, 0],
  )
  assert.equal(chart.points[1]?.label, '29 mars 2026')
  assert.equal(chart.points[1]?.shortLabel, '29 mars 2026')
  assert.deepEqual(
    chart.points.map((point) => point.showtime_count),
    [12, 0, 6],
  )
  const sparse = statisticsLineChart([
    rows[0]!,
    rows[1]!,
    { date: '2026-04-01', showtime_count: 3 },
  ])
  assert.equal(sparse.points[1]?.x, 25)
})

test('empty, single-day and all-zero series have finite geometry and integer scales', () => {
  const empty = statisticsLineChart([])
  assert.deepEqual(empty.points, [])
  assert.deepEqual(empty.dateTicks, [])
  assert.equal(empty.line, '')
  for (const count of [0, 1, 3, 7, 1_234_567, Number.MAX_SAFE_INTEGER]) {
    const chart = statisticsLineChart([
      { date: '2026-12-31', showtime_count: count },
    ])
    assert.equal(chart.points[0]?.x, 50)
    assert.equal(chart.points[0]?.label, '31 décembre 2026')
    assert.equal(chart.dateTicks.length, 1)
    assert.ok(chart.ticks.every((tick) => Number.isInteger(tick.count)))
    assert.ok(
      chart.points.every(
        (point) => Number.isFinite(point.y) && point.y >= 0 && point.y <= 100,
      ),
    )
    assert.ok(chart.ceiling >= count)
  }
  const zeros = statisticsLineChart([
    { date: '2026-01-01', showtime_count: 0 },
    { date: '2026-01-02', showtime_count: 0 },
  ])
  assert.equal(zeros.line, '0,100 100,100')
})

test('long histories keep every day while limiting visible date ticks', () => {
  const rows = Array.from({ length: 1000 }, (_, index) => ({
    date: new Date(Date.UTC(2024, 0, index + 1)).toISOString().slice(0, 10),
    showtime_count: index,
  }))
  const chart = statisticsLineChart(rows)
  assert.equal(chart.points.length, rows.length)
  assert.equal(chart.dateTicks.length, 3)
  assert.equal(chart.dateTicks[0]?.date, rows[0]?.date)
  assert.equal(chart.dateTicks.at(-1)?.date, rows.at(-1)?.date)
  assert.ok(chart.points.every((point) => point.x >= 0 && point.x <= 100))
})

test('SSR renders accessible French data table with native SVG and no interactive plot', async () => {
  const html = await render([
    { date: '2026-09-21', showtime_count: 1234 },
    { date: '2026-09-22', showtime_count: 0 },
    { date: '2026-09-23', showtime_count: 1 },
  ])
  assert.match(
    html,
    /role="img" aria-label="Évolution du nombre de séances par jour/,
  )
  assert.match(html, /<svg/)
  assert.match(html, /<polyline/)
  assert.match(html, /vector-effect="non-scaling-stroke"/)
  assert.match(html, /<details[^>]*><summary/)
  assert.match(html, /Voir les données par jour/)
  assert.match(
    html,
    /role="region" aria-label="Séances par jour, tableau défilant" tabindex="0"/,
  )
  assert.match(
    html,
    /<caption[^>]*>\s*Nombre de séances par jour de programmation\s*<\/caption>/,
  )
  assert.match(html, /scope="col"[^>]*>Date/)
  assert.match(html, /scope="col"[^>]*>Séances/)
  assert.equal((html.match(/scope="row"/g) ?? []).length, 3)
  assert.match(html, /datetime="2026-09-21">21 septembre 2026<\/time>/)
  assert.ok(html.includes(statisticsCount(1234)))
  assert.match(html, /23 septembre 2026 : 1 séance\s*<\/title>/)
  assert.match(html, /22 septembre 2026 : 0 séances\s*<\/title>/)
  assert.doesNotMatch(html, /<canvas|NaN|Infinity|<svg[^>]*tabindex/)
})

test('SSR gives empty state and single-point marker without inventing a line', async () => {
  const empty = await render([])
  assert.match(empty, /Aucune donnée pour ces filtres/)
  assert.doesNotMatch(empty, /<svg|<table|<details/)
  const single = await render([{ date: '2026-10-25', showtime_count: 1 }])
  assert.match(single, /<circle cx="50%" cy="0%" r="5"/)
  assert.doesNotMatch(single, /<polyline|NaN|Infinity/)
  assert.match(single, /25 octobre 2026/)
  assert.equal((single.match(/scope="row"/g) ?? []).length, 1)
})

test('page puts daily section after totals within nonzero details and passes filtered response directly', async () => {
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
    /<StatisticsLineChart\s+:key="signature"\s+:rows="data.daily_showtimes"/,
  )
  const totals = page.indexOf('id="statistics-totals"')
  const empty = page.indexOf('v-if="data.totals.showtimes === 0"')
  const daily = page.indexOf('id="statistics-daily"')
  const ranks = page.indexOf('id="statistics-top"')
  assert.ok(totals < empty && empty < daily && daily < ranks)
})
