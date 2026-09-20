import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import type { LocationQuery } from 'vue-router'
import {
  statisticsCityName,
  statisticsHeatmapColors,
  statisticsHeatmapLevel,
  statisticsHeatmapStyle,
} from '../app/utils/statistics.ts'
import type {
  StatisticsCityRank,
  StatisticsResponse,
  StatisticsTheaterRank,
} from '../app/types/api.ts'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import {
  createStatisticsRequest,
  nextStatisticsSort,
  parseStatisticsQuery,
  statisticsBars,
  statisticsBucketLabel,
  statisticsDateError,
  statisticsDraft,
  statisticsDraftQuery,
  statisticsHeatmapRows,
  statisticsHours,
  statisticsLocalPage,
  statisticsMaxSelections,
  statisticsOptionsWithSelection,
  statisticsQuerySignature,
  statisticsRouteQuery,
  statisticsSearchOptions,
  statisticsSelectionSummary,
  statisticsShare,
  toggleStatisticsSelection,
} from '../app/utils/statistics.ts'

test('local city names use consistent French casing without changing cinema names', async () => {
  for (const [input, expected] of [
    ['PARIS', 'Paris'],
    ['bOrDeAuX', 'Bordeaux'],
    ['LYON', 'Lyon'],
    ['MÂCON', 'Mâcon'],
    ['FONTENAY-LE-COMTE', 'Fontenay-le-Comte'],
    ['LA ROCHELLE', 'La Rochelle'],
    ['LOMME (LILLE)', 'Lomme (Lille)'],
    ['VILLENEUVE-D’ASCQ', 'Villeneuve-d’Ascq'],
    ["L'HAŸ-LES-ROSES", "L'Haÿ-les-Roses"],
    ['ÉVRY-COURCOURONNES', 'Évry-Courcouronnes'],
    ['  CLERMONT-FERRAND  ', 'Clermont-Ferrand'],
    ['', ''],
  ]) {
    assert.equal(statisticsCityName(input!), expected)
    assert.equal(statisticsCityName(expected!), expected)
  }
  const component = await readFile(
    new URL('../app/components/StatisticsLocalTable.vue', import.meta.url),
    'utf8',
  )
  assert.ok(
    component.includes(
      "'theater_count' in row ? statisticsCityName(row.name) : row.name",
    ),
  )
  assert.ok(component.includes('statisticsCityName(row.city)'))
})

test('heatmap distinguishes typical counts despite peaks and reserves white for zero', () => {
  assert.equal(statisticsHeatmapLevel(0, 1957), 0)
  assert.equal(statisticsHeatmapLevel(1957, 1957), 9)
  assert.equal(statisticsHeatmapLevel(2000, 1957), 9)
  for (const [count, maximum] of [
    [1, 0],
    [NaN, 10],
    [1, Infinity],
    [-1, 10],
  ])
    assert.equal(statisticsHeatmapLevel(count!, maximum!), 0)
  const levels = [2, 76, 197, 397, 565, 837, 1192, 1276, 1957].map((count) =>
    statisticsHeatmapLevel(count, 1957),
  )
  assert.deepEqual(levels, [1, 2, 3, 5, 5, 6, 8, 8, 9])
  assert.equal(new Set(statisticsHeatmapColors).size, 10)
  assert.equal(statisticsHeatmapStyle(0, 1957).backgroundColor, '#ffffff')
  assert.equal(statisticsHeatmapStyle(1957, 1957).color, '#ffffff')
})

test('heatmap text meets WCAG AA contrast at every color level', () => {
  function luminance(hex: string) {
    const channels = hex
      .slice(1)
      .match(/../g)!
      .map((value) => {
        const channel = parseInt(value, 16) / 255
        return channel <= 0.04045
          ? channel / 12.92
          : ((channel + 0.055) / 1.055) ** 2.4
      })
    return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722
  }
  for (let level = 0; level <= 9; level++) {
    const style = statisticsHeatmapStyle(
      level === 0 ? 0 : (level - 0.5) ** 2,
      81,
    )
    const a = luminance(style.backgroundColor!)
    const b = luminance(style.color)
    assert.ok(
      (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05) >= 4.5,
      `level ${level}`,
    )
  }
})

function fixture(): StatisticsResponse {
  return {
    generated_at: '2026-09-15T10:00:00Z',
    timezone: 'Europe/Paris',
    range: { from: '2026-09-15', through: '2026-09-21' },
    coverage: {
      snapshot_window: { from: '2026-09-15', through: '2026-09-21' },
      intersection: { from: '2026-09-15', through: '2026-09-21' },
      completeness: 'unknown',
      stale: false,
    },
    options: {
      cities: [{ slug: 'paris', name: 'Paris' }],
      theaters: [
        {
          id: 'ugc-1',
          slug: 'ugc-1',
          name: 'UGC',
          city: 'Paris',
          city_slug: 'paris',
          chain: 'ugc',
          passes: ['UGC Illimité'],
        },
      ],
      chains: ['ugc'],
      languages: ['VF', 'unknown'],
      formats: ['2D'],
      genres: [{ value: 'drame', label: 'Drame' }],
      passes: ['UGC Illimité'],
    },
    totals: { showtimes: 2, movies: 1, theaters: 1, cities: 1 },
    top_movies: {
      by_showtimes: [
        { slug: 'film', title: 'Film', showtime_count: 2, theater_count: 1 },
      ],
      by_theaters: [
        { slug: 'film', title: 'Film', showtime_count: 2, theater_count: 1 },
      ],
    },
    heatmap: Array.from({ length: 168 }, (_, index) => ({
      weekday: Math.floor(index / 24) + 1,
      hour: index % 24,
      showtime_count: index === 32 ? 2 : 0,
    })),
    versions: [
      { value: 'VF', label: 'VF', count: 1 },
      { value: 'unknown', label: 'Non renseigné', count: 1 },
    ],
    formats: [{ value: '2D', label: '2D', count: 2 }],
    genres: [{ value: 'drame', label: 'Drame', count: 1 }],
    runtimes: [
      { value: 'short', label: 'Moins de 1h30', count: 0 },
      { value: 'medium', label: 'De 1h30 à 2h', count: 1 },
      { value: 'long', label: 'Plus de 2h', count: 0 },
      { value: 'unknown', label: 'Non renseignée', count: 0 },
    ],
    local: {
      cities: [
        {
          slug: 'paris',
          name: 'Paris',
          showtime_count: 2,
          movie_count: 1,
          theater_count: 1,
        },
      ],
      theaters: [
        {
          id: 'ugc-1',
          slug: 'ugc-1',
          name: 'UGC',
          city: 'Paris',
          city_slug: 'paris',
          chain: 'ugc',
          showtime_count: 2,
          movie_count: 1,
        },
      ],
    },
    concentration: {
      top_movie_count: 1,
      top_showtime_count: 2,
      other_showtime_count: 0,
    },
  }
}

test('statistics keeps backend default dates implicit across non-date edits and reset', () => {
  const draft = statisticsDraft({}, fixture().range)
  assert.equal(draft.date, '2026-09-15')
  assert.equal(draft.date_to, '2026-09-21')
  assert.equal(draft.explicitDates, false)
  assert.deepEqual(statisticsDraftQuery(draft), { query: {}, error: '' })
  draft.city = ['paris']
  assert.deepEqual(statisticsDraftQuery(draft).query, { city: ['paris'] })
  draft.explicitDates = true
  assert.deepEqual(statisticsDraftQuery(draft).query, {
    date: '2026-09-15',
    date_to: '2026-09-21',
    city: ['paris'],
  })
  assert.deepEqual(
    statisticsRouteQuery({
      city: 'paris',
      date: '2026-09-15',
      campaign: 'footer',
    }),
    { campaign: 'footer' },
  )
  assert.deepEqual(statisticsDraftQuery(statisticsDraft({})).query, {})
})

test('query round trip preserves accents, exact versions, passes and unrelated URL keys without forwarding them', () => {
  const query = {
    date: '2026-09-15',
    date_to: '2026-09-21',
    city: ['créteil', 'lille & paris'],
    theater: ['ugc-1', 'pathé/+?#,2'],
    chain: 'ugc',
    language: 'VF_SME',
    format: 'IMAX',
    genre: 'comédie',
    pass: 'UGC Illimité & cinéma',
  }
  const encoded = new URLSearchParams()
  for (const [key, values] of Object.entries(query))
    for (const value of Array.isArray(values) ? values : [values])
      encoded.append(key, value)
  const route: LocationQuery = {}
  for (const key of encoded.keys())
    route[key] =
      key === 'city' || key === 'theater'
        ? encoded.getAll(key)
        : encoded.get(key)
  const parsed = parseStatisticsQuery({ ...route, campaign: 'footer' })
  assert.equal(parsed.error, '')
  assert.deepEqual(parsed.query, query)
  assert.deepEqual(statisticsDraftQuery(statisticsDraft(route)).query, query)
  assert.deepEqual(
    statisticsRouteQuery({ campaign: 'footer', language: 'VF' }, parsed.query),
    { campaign: 'footer', ...query },
  )
  assert.equal(
    statisticsQuerySignature({ ...route, campaign: 'footer' }),
    statisticsQuerySignature(route),
  )
  assert.notEqual(
    statisticsQuerySignature(route),
    statisticsQuerySignature({ ...route, language: 'VF' }),
  )
  assert.deepEqual(
    parseStatisticsQuery({
      city: ' deleted-city ',
      theater: 'missing',
      genre: 'absent',
      pass: 'missing',
    }).query,
    {
      city: ['deleted-city'],
      theater: ['missing'],
      genre: 'absent',
      pass: 'missing',
    },
  )
})

test('back and forward reconstruct applied drafts without dependent filter clearing', () => {
  const history = [
    {},
    {
      city: ['paris', 'absent'],
      theater: ['other-city', 'ugc-1'],
      language: 'unknown',
      format: 'unknown',
    },
    { city: ['lille'], pass: 'UGC Illimité' },
  ]
  for (const route of [...history, ...history.toReversed()]) {
    assert.deepEqual(
      statisticsDraftQuery(statisticsDraft(route, fixture().range)).query,
      route,
    )
  }
  const available = [{ value: 'paris', label: 'Paris' }]
  assert.deepEqual(
    statisticsOptionsWithSelection(available, [
      'paris',
      'absent',
      'other',
      'absent',
    ]),
    [
      ...available,
      { value: 'absent', label: 'absent (indisponible)' },
      { value: 'other', label: 'other (indisponible)' },
    ],
  )
  assert.equal(statisticsOptionsWithSelection(available, ['paris']), available)
  assert.equal(statisticsOptionsWithSelection(available, []), available)
})

test('Infinity Vision remains canonical through statistics URL, draft and bucket labels', () => {
  const query = { format: 'INFINITY_VISION' } as const
  assert.deepEqual(parseStatisticsQuery(query), { query, error: '' })
  assert.deepEqual(statisticsDraftQuery(statisticsDraft(query)), {
    query,
    error: '',
  })
  assert.deepEqual(
    statisticsRouteQuery({ format: 'ICE', campaign: 'footer' }, query),
    { campaign: 'footer', ...query },
  )
  assert.equal(
    statisticsBucketLabel(query.format, query.format, 'format'),
    'Infinity Vision',
  )
  assert.notEqual(
    statisticsQuerySignature(query),
    statisticsQuerySignature({ format: 'ICE' }),
  )
  for (const format of [
    'infinity_vision',
    'Infinity Vision',
    'INFINITY_VISION+ICE',
    '',
    ['INFINITY_VISION', 'ICE'],
  ]) {
    assert.ok(parseStatisticsQuery({ format }).error)
  }
})

test('VOF current statistics survives parsing, draft edits and shared URL reconstruction', () => {
  const query = { language: 'VOF', city: ['paris'], film: 'film-42' } as const
  const route = {
    ...query,
    city: [...query.city],
    campaign: ['footer', 'test'],
  }
  const before = structuredClone(route)
  const parsed = parseStatisticsQuery(route)
  assert.deepEqual(parsed, { query, error: '' })
  assert.deepEqual(parseStatisticsQuery({ language: ' VOF ' }), {
    query: { language: 'VOF' },
    error: '',
  })
  const draft = statisticsDraft(route, fixture().range)
  assert.equal(draft.language, 'VOF')
  assert.deepEqual(statisticsDraftQuery(draft), parsed)
  draft.genre = 'drame'
  const applied = statisticsRouteQuery(route, statisticsDraftQuery(draft).query)
  assert.deepEqual(applied, { ...route, genre: 'drame' })
  for (const entry of [route, applied, route, applied]) {
    assert.deepEqual(
      statisticsDraftQuery(statisticsDraft(entry)),
      parseStatisticsQuery(entry),
    )
    assert.equal(statisticsDraft(entry).language, 'VOF')
  }
  assert.notEqual(
    statisticsQuerySignature(route),
    statisticsQuerySignature({ ...route, language: 'VF' }),
  )
  assert.deepEqual(statisticsRouteQuery(applied), { campaign: route.campaign })
  assert.deepEqual(route, before)
  for (const language of [
    'vof',
    'Vof',
    'ORIGINAL',
    'ALL',
    'VOF,VOSTFR',
    'VOF+VOSTFR',
    '',
    null,
    ['VOF'],
    ['VOF', 'VOF'],
    ['VOF', 'VF'],
  ]) {
    assert.ok(
      parseStatisticsQuery({ language }).error,
      JSON.stringify(language),
    )
  }
})

test('VOF versions preserve backend buckets, separate accessibility, counts and French shares', async () => {
  const response = fixture()
  response.totals.showtimes = 15
  response.versions = [
    { value: 'VOF', label: 'VOF', count: 5 },
    { value: 'VF', label: 'VF', count: 4 },
    { value: 'VF_SME', label: 'VF_SME', count: 2 },
    { value: 'VFSTF', label: 'VFSTF', count: 1 },
    { value: 'VOSTFR', label: 'VOSTFR', count: 1 },
    { value: 'VO', label: 'VO', count: 1 },
    { value: 'unknown', label: 'Non renseigné', count: 1 },
  ]
  response.options.languages = response.versions.map((row) => row.value)
  const before = structuredClone(response)
  const rows = response.versions.map((row) => ({
    ...row,
    label: statisticsBucketLabel(row.value, row.label, 'language'),
  }))
  const bars = statisticsBars(rows, response.totals.showtimes)
  assert.deepEqual(
    bars.map((row) => [row.value, row.label, row.count, row.width, row.share]),
    [
      ['VOF', 'VOF', 5, 100, '33,3 %'],
      ['VF', 'VF', 4, 80, '26,7 %'],
      ['VF_SME', 'VF SME', 2, 40, '13,3 %'],
      ['VFSTF', 'VFSTF', 1, 20, '6,7 %'],
      ['VOSTFR', 'VOSTFR', 1, 20, '6,7 %'],
      ['VO', 'VO', 1, 20, '6,7 %'],
      ['unknown', 'Non renseigné', 1, 20, '6,7 %'],
    ],
  )
  assert.equal(
    statisticsBars(rows.slice(0, 1), response.totals.showtimes)[0]?.share,
    '33,3 %',
  )
  assert.equal(statisticsBars(rows.slice(0, 1), 5)[0]?.share, '100,0 %')
  assert.deepEqual(statisticsBars([], 0), [])
  assert.equal(statisticsShare(0, 0), 'Non calculable')
  const options = response.options.languages.map((value) => ({
    value,
    label: statisticsBucketLabel(value, value, 'language'),
  }))
  assert.equal(statisticsOptionsWithSelection(options, ['VOF']), options)
  const unavailable = options.filter((option) => option.value !== 'VOF')
  assert.equal(
    statisticsOptionsWithSelection(unavailable, []).some(
      (option) => option.value === 'VOF',
    ),
    false,
  )
  assert.deepEqual(
    statisticsOptionsWithSelection(unavailable, ['VOF']).at(-1),
    { value: 'VOF', label: 'VOF (indisponible)' },
  )
  assert.deepEqual(response, before)
  const page = await readFile(
    new URL('../app/pages/statistiques.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    page,
    /:rows="buckets\(\s*data\.versions,\s*'language'\s*\)"\s+:total="data\.totals\.showtimes"/,
  )
  assert.match(
    page,
    /source\?\.languages\.map\(\s*(?:value|\(\s*value\s*\))\s*=>\s*\(\s*\{\s*value,\s*label:\s*statisticsBucketLabel\(\s*value,\s*value,\s*'language'\s*\),?\s*\}\s*\)\s*\)/,
  )
})

test('rejects repeated scalars, empty, oversized and unknown enum selections instead of silently dropping filters', () => {
  for (const key of [
    'date',
    'date_to',
    'city',
    'theater',
    'chain',
    'language',
    'format',
    'genre',
    'pass',
    'film',
  ]) {
    for (const value of [null, '', ' '.repeat(3), 'é'.repeat(101)])
      assert.ok(
        parseStatisticsQuery({ [key]: value }).error,
        `${key}: ${value}`,
      )
    if (key !== 'city' && key !== 'theater')
      for (const value of [['VF', 'VO'], ['VF', 'VF'], ['VF'], []])
        assert.ok(
          parseStatisticsQuery({ [key]: value }).error,
          `${key}: repeated scalar`,
        )
  }
  for (const query of [
    { language: 'ALL' },
    { language: 'vf' },
    { format: 'ALL' },
    { format: 'IMAX+3D' },
    { chain: 'unknown' },
  ])
    assert.ok(parseStatisticsQuery(query).error)
  assert.equal(
    parseStatisticsQuery({
      language: 'VFSTF',
      format: 'ICE',
      chain: 'noecinemas',
    }).error,
    '',
  )
  assert.equal(
    parseStatisticsQuery({
      city: '界'.repeat(66),
      theater: '界'.repeat(66),
      pass: '界'.repeat(66),
      genre: '界'.repeat(66),
    }).error,
    '',
  )
  assert.ok(parseStatisticsQuery({ city: '界'.repeat(67) }).error)
})

test('list bounds apply before deduplication, byte trimming and total owned query normalization', () => {
  for (const key of ['city', 'theater']) {
    assert.equal(
      parseStatisticsQuery({
        [key]: Array.from({ length: 50 }, (_, i) => `id${i}`),
      }).error,
      '',
    )
    assert.deepEqual(
      parseStatisticsQuery({ [key]: Array(50).fill('same') }).query,
      { [key]: ['same'] },
    )
    for (const entries of [
      Array(51).fill('same'),
      Array.from({ length: 51 }, (_, i) => `id${i}`),
      ['paris', ''],
      ['paris', null],
      ['paris', '  '],
    ])
      assert.ok(parseStatisticsQuery({ [key]: entries }).error)
    assert.deepEqual(
      parseStatisticsQuery({
        [key]: [' paris ', 'lille', 'paris', 'lille', 'PARIS', 'a,b'],
      }).query,
      { [key]: ['paris', 'lille', 'PARIS', 'a,b'] },
    )
    for (const value of [
      'é'.repeat(100),
      '界'.repeat(66) + 'ab',
      ' '.repeat(199) + 'x',
    ])
      assert.equal(parseStatisticsQuery({ [key]: [value] }).error, '')
    for (const value of [
      'é'.repeat(100) + ' ',
      ' '.repeat(200) + 'x',
      '界'.repeat(67),
    ])
      assert.ok(parseStatisticsQuery({ [key]: [value] }).error)
  }
  assert.deepEqual(parseStatisticsQuery({ city: [], theater: [] }), {
    query: {},
    error: '',
  })
  const exactly4096 = {
    city: [...Array(19).fill('a'.repeat(200)), 'b'.repeat(177)],
  }
  assert.equal(parseStatisticsQuery(exactly4096).error, '')
  assert.ok(
    parseStatisticsQuery({
      city: [...exactly4096.city.slice(0, -1), 'b'.repeat(178)],
    }).error,
  )
  assert.ok(
    parseStatisticsQuery({ city: Array(50).fill('界'.repeat(66)) }).error,
  )
  assert.equal(
    parseStatisticsQuery({ city: ['paris'], unrelated: 'x'.repeat(10000) })
      .error,
    '',
  )
})

test('film is an exact bounded scalar, never split, case folded or silently removed', () => {
  assert.deepEqual(parseStatisticsQuery({}).query, {})
  for (const film of [
    'film-42',
    'Film-42',
    'unknown-film',
    'film-9223372036854775808',
    'registered,alias',
    'é'.repeat(100),
    '😀'.repeat(50),
    ' '.repeat(199) + 'x',
    '<b>&/+?#,é',
  ]) {
    assert.deepEqual(parseStatisticsQuery({ film }), {
      query: { film: film.trim() },
      error: '',
    })
    assert.equal(
      statisticsDraftQuery(statisticsDraft({ film })).query.film,
      film.trim(),
    )
  }
  for (const film of [
    '\0film',
    'film\0',
    '\ud800',
    '\udfff',
    'x\ud800y',
    'é'.repeat(100) + ' ',
    '😀'.repeat(50) + 'x',
    ' '.repeat(200) + 'x',
  ]) {
    assert.ok(parseStatisticsQuery({ film }).error)
    assert.equal(statisticsDraft({ film }).film, film)
  }
  const route = { film: 'film-42', campaign: ['footer', 'test'] }
  assert.deepEqual(statisticsRouteQuery(route), {
    campaign: ['footer', 'test'],
  })
  assert.deepEqual(route, { film: 'film-42', campaign: ['footer', 'test'] })
  assert.notEqual(
    statisticsQuerySignature(route),
    statisticsQuerySignature({ ...route, film: 'film-43' }),
  )
  assert.ok(
    parseStatisticsQuery({
      city: [...Array(19).fill('a'.repeat(200)), 'b'.repeat(177)],
      film: 'x',
    }).error,
  )
})

test('draft, route and query arrays are independent, invalid lists remain recoverable and empty arrays disappear', () => {
  const route = {
    city: [' paris ', 'paris', 'missing'],
    theater: ['ugc-1'],
    campaign: ['footer', 'test'],
  }
  const before = structuredClone(route)
  const draft = statisticsDraft(route, fixture().range)
  assert.deepEqual(draft.city, ['paris', 'missing'])
  draft.city.push('lille')
  draft.theater.length = 0
  const query = statisticsDraftQuery(draft).query
  assert.deepEqual(query, { city: ['paris', 'missing', 'lille'] })
  const next = statisticsRouteQuery(route, query)
  assert.deepEqual(next, {
    city: ['paris', 'missing', 'lille'],
    campaign: ['footer', 'test'],
  })
  query.city!.push('rouen')
  assert.deepEqual(next.city, ['paris', 'missing', 'lille'])
  assert.deepEqual(route, before)
  assert.deepEqual(statisticsRouteQuery(route, { city: [], theater: [] }), {
    campaign: ['footer', 'test'],
  })
  for (const values of [
    ['paris', null],
    Array(51).fill('same'),
    ['a'.repeat(201)],
  ]) {
    const invalid = statisticsDraft({ city: values })
    assert.equal(invalid.city.length, values.length)
    assert.ok(statisticsDraftQuery(invalid).error)
    invalid.city = []
    assert.equal(statisticsDraftQuery(invalid).error, '')
  }
  assert.notEqual(
    statisticsQuerySignature({ city: ['paris'] }),
    statisticsQuerySignature({ city: ['paris', 'lille'] }),
  )
})

test('multi-selection search, summaries, clear and limit keep selected unavailable rows removable', () => {
  const options = statisticsOptionsWithSelection(
    [
      { value: 'paris', label: 'Paris' },
      { value: 'lille', label: 'Lille' },
      { value: 'creteil', label: 'Créteil' },
    ],
    ['missing'],
  )
  const selected = ['paris', 'missing']
  assert.deepEqual(
    statisticsSearchOptions(options, selected, '  LILLE ').map(
      (option) => option.value,
    ),
    ['paris', 'lille', 'missing'],
  )
  assert.deepEqual(
    statisticsSearchOptions(options, selected, 'none').map(
      (option) => option.value,
    ),
    selected,
  )
  assert.deepEqual(statisticsSearchOptions(options, [], 'CRETEIL'), [])
  assert.deepEqual(statisticsSearchOptions([], [], ''), [])
  assert.deepEqual(toggleStatisticsSelection(selected, 'lille', 50), [
    'paris',
    'missing',
    'lille',
  ])
  assert.deepEqual(toggleStatisticsSelection(selected, 'missing', 50), [
    'paris',
  ])
  assert.deepEqual(selected, ['paris', 'missing'])
  const full = Array.from(
    { length: statisticsMaxSelections },
    (_, i) => `id${i}`,
  )
  assert.deepEqual(toggleStatisticsSelection(full, 'extra', 50), full)
  assert.notEqual(toggleStatisticsSelection(full, 'extra', 50), full)
  assert.equal(toggleStatisticsSelection(full, 'id0', 50).length, 49)
  assert.equal(
    toggleStatisticsSelection(
      toggleStatisticsSelection(full, 'id0', 50),
      'extra',
      50,
    ).length,
    50,
  )
  assert.equal(
    statisticsSelectionSummary(options, [], 'Ville', 'Toutes les villes'),
    'Toutes les villes',
  )
  assert.equal(
    statisticsSelectionSummary(
      options,
      ['paris'],
      'Ville',
      'Toutes les villes',
    ),
    'Paris',
  )
  assert.equal(
    statisticsSelectionSummary(
      options,
      ['missing'],
      'Ville',
      'Toutes les villes',
    ),
    'missing (indisponible)',
  )
  assert.equal(
    statisticsSelectionSummary(options, selected, 'Ville', 'Toutes les villes'),
    '2 villes sélectionnées',
  )
  assert.equal(
    statisticsSelectionSummary(options, selected, 'Cinéma', 'Tous les cinémas'),
    '2 cinémas sélectionnés',
  )
})

test('validates inclusive calendar ranges, leap days, DST and today without browser timezone defaults', () => {
  for (const date of [
    '2027-02-29',
    '2026-02-30',
    '2026-13-01',
    '2026-9-15',
    '2026-09-15T00:00:00Z',
    ' 2026-09-15',
  ])
    assert.ok(statisticsDateError(date, undefined))
  assert.ok(statisticsDateError(undefined, '2026-09-15'))
  assert.ok(statisticsDateError('2026-09-15', '2026-09-14'))
  assert.ok(statisticsDateError('2026-09-14', undefined, '2026-09-15'))
  assert.equal(statisticsDateError('2026-09-15', undefined, '2026-09-15'), '')
  assert.equal(statisticsDateError('2028-02-29', '2028-03-30'), '')
  assert.ok(statisticsDateError('2028-02-29', '2028-03-31'))
  assert.equal(statisticsDateError('2026-10-01', '2026-10-31'), '')
  assert.ok(statisticsDateError('2026-10-01', '2026-11-01'))
  assert.equal(statisticsDateError('2027-03-01', '2027-03-31'), '')
  assert.equal(statisticsDraft({ date: '2026-09-15' }).date_to, '2026-09-15')
  const draft = statisticsDraft({}, fixture().range)
  draft.explicitDates = true
  draft.date = '2026-09-22'
  const before = structuredClone(draft)
  assert.ok(statisticsDraftQuery(draft).error)
  assert.deepEqual(draft, before)
})

test('chart shares have exact denominators and widths independently scale to largest bucket', () => {
  assert.equal(statisticsShare(0, 0), 'Non calculable')
  assert.equal(statisticsShare(2, 0), 'Non calculable')
  assert.equal(statisticsShare(Number.NaN, 4), 'Non calculable')
  assert.equal(statisticsShare(1, 3), '33,3 %')
  const bars = statisticsBars(
    [
      { value: 'a', label: 'A', count: 4 },
      { value: 'b', label: 'B', count: 2 },
    ],
    10,
  )
  assert.deepEqual(
    bars.map((row) => [row.width, row.share]),
    [
      [100, '40,0 %'],
      [50, '20,0 %'],
    ],
  )
  assert.equal(
    statisticsBars([{ value: 'zero', label: 'Zero', count: 0 }], 0)[0]?.width,
    0,
  )
  assert.equal(statisticsBucketLabel('unknown', '', 'format'), 'Non renseigné')
  assert.equal(statisticsBucketLabel('VF_SME', 'VF_SME', 'language'), 'VF SME')
  assert.equal(statisticsBucketLabel('DOLBY', 'DOLBY', 'format'), 'Dolby')
  const response = fixture()
  assert.equal(
    statisticsBars(response.versions, response.totals.showtimes)[0]?.share,
    '50,0 %',
  )
  assert.equal(
    statisticsBars(response.genres, response.totals.movies)[0]?.share,
    '100,0 %',
  )
  assert.equal(
    response.runtimes.reduce((count, row) => count + row.count, 0),
    response.totals.movies,
  )
})

test('heatmap always has seven service weekdays and 24 hours reordered 08 through 07', () => {
  assert.deepEqual(statisticsHours, [
    ...Array.from({ length: 16 }, (_, i) => i + 8),
    ...Array.from({ length: 8 }, (_, i) => i),
  ])
  const rows = statisticsHeatmapRows([
    { weekday: 1, hour: 0, showtime_count: 4 },
    { weekday: 1, hour: 8, showtime_count: 2 },
  ])
  assert.equal(rows.length, 7)
  assert.ok(rows.every((row) => row.cells.length === 24))
  assert.equal(rows[0]?.label, 'Lundi')
  assert.deepEqual(rows[0]?.cells[0], { hour: 8, count: 2 })
  assert.deepEqual(rows[0]?.cells[16], { hour: 0, count: 4 })
  assert.equal(rows[1]?.cells[16]?.count, 0)
})

test('local sorting acts before 20-row slicing, supports whole catalog and never mutates rows', () => {
  const rows: StatisticsCityRank[] = Array.from({ length: 45 }, (_, i) => ({
    slug: `city-${i}`,
    name: `City ${String(i).padStart(2, '0')}`,
    movie_count: i,
    showtime_count: i * 2,
    theater_count: 45 - i,
  }))
  const before = structuredClone(rows)
  assert.deepEqual(
    statisticsLocalPage(rows, null, 1).rows.map((row) => row.movie_count),
    Array.from({ length: 20 }, (_, i) => 44 - i),
  )
  assert.equal(statisticsLocalPage(rows, null, 2).rows[0]?.movie_count, 24)
  assert.equal(statisticsLocalPage(rows, null, 3).rows.length, 5)
  assert.equal(statisticsLocalPage(rows, null, 9).page, 3)
  assert.equal(
    statisticsLocalPage(rows, nextStatisticsSort(null, 'theater_count'), 1)
      .rows[0]?.slug,
    'city-0',
  )
  assert.equal(
    statisticsLocalPage(rows, nextStatisticsSort(null, 'name'), 2).rows[0]
      ?.slug,
    'city-20',
  )
  assert.deepEqual(rows, before)
  assert.deepEqual(statisticsLocalPage([], null, 1), {
    rows: [],
    pages: 1,
    page: 1,
    total: 0,
  })
})

test('sort defaults numeric descending and text ascending, toggles, ties by name then stable identity', () => {
  assert.deepEqual(nextStatisticsSort(null, 'showtime_count'), {
    column: 'showtime_count',
    direction: 'ascending',
  })
  assert.deepEqual(nextStatisticsSort(null, 'movie_count'), {
    column: 'movie_count',
    direction: 'descending',
  })
  assert.deepEqual(
    nextStatisticsSort(nextStatisticsSort(null, 'movie_count'), 'movie_count'),
    { column: 'movie_count', direction: 'ascending' },
  )
  assert.deepEqual(nextStatisticsSort(null, 'city'), {
    column: 'city',
    direction: 'ascending',
  })
  const rows: StatisticsTheaterRank[] = ['z', 'b', 'a'].map((id) => ({
    id,
    slug: id,
    name: id === 'z' ? 'Zoo' : 'Alpha',
    city: 'Paris',
    city_slug: 'paris',
    chain: 'ugc',
    movie_count: 2,
    showtime_count: 3,
  }))
  assert.deepEqual(
    statisticsLocalPage(
      rows,
      nextStatisticsSort(null, 'showtime_count'),
      1,
    ).rows.map((row) => row.slug),
    ['a', 'b', 'z'],
  )
  assert.deepEqual(
    statisticsLocalPage(rows, null, 1).rows.map((row) => row.slug),
    ['a', 'b', 'z'],
  )
})

test('local default ranks screenings before films for cities and theaters', () => {
  const cities: StatisticsCityRank[] = [
    {
      slug: 'many-films',
      name: 'Alpha',
      movie_count: 9,
      showtime_count: 10,
      theater_count: 1,
    },
    {
      slug: 'many-shows',
      name: 'Zulu',
      movie_count: 1,
      showtime_count: 20,
      theater_count: 1,
    },
    {
      slug: 'tie-more-films',
      name: 'Beta',
      movie_count: 2,
      showtime_count: 20,
      theater_count: 1,
    },
  ]
  const theaters: StatisticsTheaterRank[] = cities.map(
    ({ theater_count: _count, ...row }) => ({
      ...row,
      id: row.slug,
      city: 'Paris',
      city_slug: 'paris',
      chain: 'ugc',
    }),
  )
  for (const rows of [cities, theaters]) {
    assert.deepEqual(
      statisticsLocalPage(rows, null, 1).rows.map((row) => row.slug),
      ['tie-more-films', 'many-shows', 'many-films'],
    )
    assert.equal(
      statisticsLocalPage(rows, nextStatisticsSort(null, 'showtime_count'), 1)
        .rows[0]?.slug,
      'many-films',
    )
    assert.equal(
      statisticsLocalPage(rows, nextStatisticsSort(null, 'movie_count'), 1)
        .rows[0]?.slug,
      'many-films',
    )
  }
})

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (cause: unknown) => void
  const promise = new Promise<T>((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}

test('late success and late failure cannot overwrite current results, errors or pending state', async () => {
  const state = { result: '', error: '', pending: false }
  const request = createStatisticsRequest<string>({
    start: () => {
      state.result = ''
      state.error = ''
      state.pending = true
    },
    success: (value) => {
      state.result = value
    },
    error: (cause) => {
      state.error = String(cause)
    },
    finish: () => {
      state.pending = false
    },
  })
  const first = deferred<string>()
  const second = deferred<string>()
  let firstSignal: AbortSignal | undefined
  const old = request.run((signal) => {
    firstSignal = signal
    return first.promise
  })
  const current = request.run(() => second.promise)
  assert.equal(firstSignal?.aborted, true)
  first.resolve('old')
  await old
  assert.deepEqual(state, { result: '', error: '', pending: true })
  second.resolve('current')
  await current
  const third = deferred<string>()
  const lateFailure = request.run(() => third.promise)
  await request.run(async () => 'newest')
  third.reject(new Error('old error'))
  await lateFailure
  assert.deepEqual(state, { result: 'newest', error: '', pending: false })
  await request.run(async () => {
    throw new Error('current error')
  })
  assert.deepEqual(state, {
    result: '',
    error: 'Error: current error',
    pending: false,
  })
  const canceled = deferred<string>()
  const disposed = request.run(() => canceled.promise)
  request.cancel()
  canceled.resolve('disposed')
  await disposed
  assert.equal(state.result, '')
})

test('typed client uses one statistics endpoint, query-only fields, abort signal and no retry', async () => {
  const response = fixture()
  const calls: {
    url: string
    options: {
      query: Record<string, string | string[]>
      retry: false
      signal?: AbortSignal
    }
  }[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080/' } }),
    $fetch: async (url: string, options: (typeof calls)[number]['options']) => {
      calls.push({ url, options })
      return response
    },
  })
  try {
    const signal = new AbortController().signal
    const api = useMesSeancesApi()
    assert.equal(
      await api.statistics(
        parseStatisticsQuery({
          city: ['paris', 'lille'],
          theater: ['ugc-1', 'ugc-2'],
          pass: 'UGC Illimité',
          campaign: 'footer',
        }).query,
        signal,
      ),
      response,
    )
    assert.equal(calls[0]?.url, 'http://localhost:8080/api/v1/statistics')
    assert.deepEqual(calls[0]?.options, {
      query: {
        city: ['paris', 'lille'],
        theater: ['ugc-1', 'ugc-2'],
        pass: 'UGC Illimité',
      },
      signal,
      retry: false,
    })
    await api.statistics()
    assert.deepEqual(calls[1]?.options.query, {})
    await api.statistics({ city: [], theater: [] })
    assert.deepEqual(calls[2]?.options.query, {})
    const cities = ['paris', 'lille']
    await api.statistics({ city: cities })
    assert.equal(calls[3]?.options.query.city, cities)
    await api.statistics(
      parseStatisticsQuery({ format: 'INFINITY_VISION' }).query,
    )
    assert.deepEqual(calls[4]?.options.query, { format: 'INFINITY_VISION' })
    await api.historyStatistics(
      parseStatisticsQuery({ format: 'INFINITY_VISION' }).query,
    )
    assert.equal(
      calls[5]?.url,
      'http://localhost:8080/api/v1/statistics/history',
    )
    assert.deepEqual(calls[5]?.options.query, { format: 'INFINITY_VISION' })
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})

test('installed ofetch serializes client arrays as repeated keys without CSV or bracket keys', async () => {
  const urls: URL[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080' } }),
    $fetch: createFetch({
      fetch: async (input: RequestInfo | URL) => {
        urls.push(new URL(input instanceof Request ? input.url : String(input)))
        return new Response(JSON.stringify(fixture()), {
          headers: { 'content-type': 'application/json' },
        })
      },
    }),
  })
  try {
    const api = useMesSeancesApi()
    const city = ['créteil', 'paris & lille', 'comma,id']
    const theater = ['ugc-1', 'pathé/+?#,2']
    const film = 'alias &+/?#,é'
    await api.statistics({ city, theater, language: 'VF', film })
    assert.deepEqual(urls[0]!.searchParams.getAll('city'), city)
    assert.deepEqual(urls[0]!.searchParams.getAll('theater'), theater)
    assert.deepEqual(urls[0]!.searchParams.getAll('film'), [film])
    assert.deepEqual(
      [...urls[0]!.searchParams.keys()],
      ['city', 'city', 'city', 'theater', 'theater', 'language', 'film'],
    )
    await api.statistics({ city: [], theater: [] })
    assert.equal(urls[1]!.search, '')
    await api.statistics({ city: ['paris'], theater: ['ugc-1'] })
    assert.deepEqual(urls[2]!.searchParams.getAll('city'), ['paris'])
    assert.deepEqual(urls[2]!.searchParams.getAll('theater'), ['ugc-1'])
    await api.statistics(
      parseStatisticsQuery({ language: ' VOF ', campaign: 'footer' }).query,
    )
    assert.equal(urls[3]!.pathname, '/api/v1/statistics')
    assert.deepEqual(
      [...urls[3]!.searchParams.entries()],
      [['language', 'VOF']],
    )
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})

test('two draft multi-controls use native disclosure, labeled search and checkboxes while advanced filters stay scalar', async () => {
  const page = await readFile(
    new URL('../app/pages/statistiques.vue', import.meta.url),
    'utf8',
  )
  const control = await readFile(
    new URL('../app/components/StatisticsMultiSelect.vue', import.meta.url),
    'utf8',
  )
  assert.match(page, /primaryFilters: \{\s*key: StatisticsMultiFilterKey/)
  assert.match(
    page,
    /<StatisticsHistorySelect\s+v-for="filter in primaryFilters"/,
  )
  assert.match(page, /advancedFilters: \{\s*key: StatisticsScalarFilterKey/)
  assert.match(page, /<select\s+v-model="draft\[filter.key\]"/)
  assert.match(page, /@submit.prevent="apply"/)
  assert.match(page, /if \(\s*!changed &&\s*\(error.value/)
  assert.match(page, /<select\s+v-model="draft.period"/)
  assert.match(page, /v-if="draft.period === 'custom'"/)
  assert.match(page, /type="date"\s+required/)
  assert.doesNotMatch(
    page,
    /statistics-mode|switchMode|À venir|api\.statistics\(|:min=/,
  )
  assert.match(page, /api\.historyStatistics\(parsed.query, signal\)/)
  assert.match(page, /useState\('statistics-paris-today'/)
  assert.match(
    page,
    /document.addEventListener\('visibilitychange', refreshCalendarDay\)/,
  )
  assert.match(
    page,
    /document.removeEventListener\('visibilitychange', refreshCalendarDay\)/,
  )
  for (const pattern of [
    /<fieldset/,
    /<legend/,
    /<details/,
    /<summary/,
    /type="search"/,
    /type="checkbox"/,
    /:for=/,
    /@keydown.enter.prevent/,
    /@keydown.esc="close"/,
    /summary.value\?\.focus\(\)/,
    /min-h-11/,
    /max-h-64 overflow-y-auto/,
    /:disabled="atLimit && !selected.has\(option.value\)"/,
    /emit\('update:modelValue', \[\]\)/,
  ])
    assert.match(control, pattern)
  assert.doesNotMatch(
    control,
    /useRoute|useRouter|useMesSeancesApi|\$fetch|localStorage|sessionStorage|v-html|role="(?:menu|listbox|combobox)"/,
  )
})

test('page integrates eight sections, accessible components, recovery and footer-only discovery', async () => {
  const read = (path: string) =>
    readFile(new URL(path, import.meta.url), 'utf8')
  const [page, footer, header, heatmap, local, bars] = await Promise.all([
    read('../app/pages/statistiques.vue'),
    read('../app/components/AppFooter.vue'),
    read('../app/components/AppHeader.vue'),
    read('../app/components/StatisticsHeatmap.vue'),
    read('../app/components/StatisticsLocalTable.vue'),
    read('../app/components/StatisticsBarChart.vue'),
  ])
  const headings = [
    'En chiffres',
    'Films les plus programmés',
    'Quand voir un film',
    'Versions',
    'Formats',
    'Genres et durées',
    'L’offre locale',
    'Concentration des séances',
  ]
  let position = -1
  for (const heading of headings) {
    const current = page.search(new RegExp(`>\\s*${heading}\\s*</h2>`))
    assert.ok(current > position, heading)
    position = current
  }
  assert.match(footer, /to: '\/statistiques', label: 'Statistiques'/)
  assert.doesNotMatch(header, /statistiques/)
  assert.match(page, /useAsyncData/)
  assert.match(page, /createStatisticsRequest/)
  assert.match(page, /Aucune séance enregistrée pour ces filtres/)
  assert.doesNotMatch(page, /coverage\.stale/)
  assert.match(page, /noindex,follow/)
  assert.match(page, /Périmètre et méthode/)
  assert.match(heatmap, /scope="col"/)
  assert.match(heatmap, /scope="row"/)
  assert.match(heatmap, /overflow-x-auto/)
  assert.match(heatmap, /<caption/)
  assert.match(local, /:aria-sort=/)
  assert.match(local, /type="radio"/)
  assert.match(local, /page\.value = 1/)
  assert.match(bars, /aria-hidden="true"/)
  for (const component of [heatmap, local, bars])
    assert.doesNotMatch(component, /useMesSeancesApi|\$fetch|v-html|<canvas/)
})
