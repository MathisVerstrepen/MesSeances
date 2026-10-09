import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { createRequire } from 'node:module'
import test from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'
import ts from 'typescript'
import {
  computed,
  createSSRApp,
  ref,
  watch,
  onMounted,
  onBeforeUnmount,
  useId,
  type Component,
} from 'vue'
import type { StatisticsDailyShowtimes } from '../app/types/api.ts'
import { statisticsCount } from '../app/utils/statistics.ts'
import {
  statisticsLineChart,
  statisticsNearestPoint,
  statisticsWeekLabels,
  type StatisticsChartOptions,
} from '../app/utils/statisticsLineChart.ts'

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
new Function(
  'require',
  'exports',
  'computed',
  'ref',
  'watch',
  'onMounted',
  'onBeforeUnmount',
  'useId',
  compiled,
)(
  (id: string) => {
    if (id === '~/utils/statistics') return { statisticsCount }
    if (id === '~/utils/statisticsLineChart')
      return {
        statisticsLineChart,
        statisticsNearestPoint,
        statisticsWeekLabels,
      }
    return require(id)
  },
  exports,
  computed,
  ref,
  watch,
  onMounted,
  onBeforeUnmount,
  useId,
)
assert.ok(exports.default)
const Chart = exports.default
const render = (
  rows: StatisticsDailyShowtimes[],
  options: StatisticsChartOptions = {},
) =>
  renderToString(createSSRApp(Chart, { rows, today: '2026-09-23', ...options }))

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

test('SSR renders keyboard-accessible native SVG chart and preserves exact French data table', async () => {
  const html = await render([
    { date: '2026-09-21', showtime_count: 1234 },
    { date: '2026-09-22', showtime_count: 0 },
    { date: '2026-09-23', showtime_count: 1 },
  ])
  assert.match(
    html,
    /role="group" tabindex="0" aria-label="Évolution du nombre de séances par jour/,
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
  assert.match(html, /Flèches gauche et droite/)
  assert.match(html, /data-today-marker data-date="2026-09-23" x1="100%"/)
  assert.doesNotMatch(html, /data-week-marker/)
  assert.doesNotMatch(html, /<canvas|NaN|Infinity|<svg[^>]*tabindex/)
})

test('today marker uses calendar geometry including sparse gaps and endpoints, never clamps outside domain', () => {
  const rows = [
    { date: '2026-03-25', showtime_count: 1 },
    { date: '2026-04-01', showtime_count: 2 },
  ]
  for (const [today, x] of [
    ['2026-03-25', 0],
    ['2026-03-29', 400 / 7],
    ['2026-04-01', 100],
  ] as const) {
    assert.ok(
      Math.abs(statisticsLineChart(rows, { today }).today!.x - x) < 1e-8,
    )
  }
  for (const today of ['2026-03-24', '2026-04-02', '2026-02-30', 'invalid'])
    assert.equal(statisticsLineChart(rows, { today }).today, null)
  assert.equal(
    statisticsLineChart([], { today: '1970-01-01', film: true }).today,
    null,
  )
  const single = statisticsLineChart([rows[0]!], {
    today: rows[0]!.date,
    film: true,
  })
  assert.equal(single.today?.x, 50)
  assert.equal(single.weeks[0]?.x, 50)
})

test('Wednesday release weeks stay French-release-relative through clipping and DST', () => {
  const rows = [
    { date: '2026-03-20', showtime_count: 0 },
    { date: '2026-04-08', showtime_count: 1 },
  ]
  const options = {
    film: true,
    frenchReleaseDate: '2026-03-27',
    today: '2026-04-01',
  }
  const chart = statisticsLineChart(rows, options)
  assert.deepEqual(
    chart.weeks.map(({ date, label }) => ({ date, label })),
    [
      { date: '2026-03-25', label: 'Semaine 1' },
      { date: '2026-04-01', label: 'Semaine 2' },
      { date: '2026-04-08', label: 'Semaine 3' },
    ],
  )
  assert.equal(chart.weeks[1]?.x, chart.today?.x)
  assert.equal(
    statisticsLineChart(rows, { ...options, frenchReleaseDate: '2026-04-01' })
      .weeks[0]?.label,
    'Mercredi',
  )
  assert.equal(
    statisticsLineChart(
      [{ date: '2026-04-01', showtime_count: 0 }, rows[1]!],
      options,
    ).weeks[0]?.label,
    'Semaine 2',
  )
  assert.deepEqual(
    statisticsLineChart(rows, { ...options, film: false }).weeks,
    [],
  )
  for (const frenchReleaseDate of [null, undefined, '2026-02-30', '']) {
    const weeks = statisticsLineChart(rows, {
      film: true,
      frenchReleaseDate,
    }).weeks
    assert.equal(weeks.length, 3)
    assert.ok(weeks.every((marker) => marker.label === 'Mercredi'))
  }
})

test('dense labels stay inside measured width without collisions or lost boundaries', () => {
  const chart = statisticsLineChart(
    [
      { date: '2024-01-01', showtime_count: 0 },
      { date: '2026-10-28', showtime_count: 1 },
    ],
    { film: true, frenchReleaseDate: '2023-12-27' },
  )
  for (const width of [180, 240, 320, 1200]) {
    const labels = statisticsWeekLabels(chart.weeks, width)
    assert.ok(labels.length < chart.weeks.length)
    let right = -Infinity
    for (const label of labels) {
      const half = (label.label.length * 7 + 8) / 2
      const center = (label.labelX * width) / 100
      assert.ok(center - half >= right + 12 - 1e-8)
      assert.ok(center - half >= 0 && center + half <= width + 1e-8)
      right = center + half
    }
  }
  assert.deepEqual(statisticsWeekLabels(chart.weeks, 0), [])
})

test('pointer selection finds closest calendar day, including sparse and single points', () => {
  const points = [{ x: 0 }, { x: 10 }, { x: 100 }]
  assert.equal(statisticsNearestPoint(points, 9), 1)
  assert.equal(statisticsNearestPoint(points, 51), 1)
  assert.equal(statisticsNearestPoint(points, 90), 2)
  assert.equal(statisticsNearestPoint([{ x: 50 }], 0), 0)
})

test('SSR keeps full Wednesday information even when dense visual labels are thinned', async () => {
  const html = await render(
    [
      { date: '2026-03-25', showtime_count: 1 },
      { date: '2026-06-24', showtime_count: 1 },
    ],
    { film: true, frenchReleaseDate: '2026-03-25', today: '2026-04-01' },
  )
  assert.equal((html.match(/data-week-marker/g) ?? []).length, 14)
  assert.ok((html.match(/data-week-label/g) ?? []).length < 14)
  assert.match(html, /<li>Semaine 14 : 24 juin 2026\.\s*<\/li>/)
  assert.match(html, /<li>Aujourd’hui : 1 avril 2026\.<\/li>/)
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
