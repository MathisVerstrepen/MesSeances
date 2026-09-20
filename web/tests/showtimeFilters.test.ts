import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { isShowtimeFormat } from '../app/utils/formats.ts'
import { enumQueryValue, singularQueryValue } from '../app/utils/routeQuery.ts'
import {
  availableFormatOptions,
  availableLanguageOptions,
  availableFilmLanguageOptions,
  matchesFilmLanguageFilter,
  languageLabel,
  queryFormatOptions,
  queryLanguageOptions,
  queryLanguageValues,
  filmLanguageValues,
  showtimeLanguageValues,
  showtimeFilterSummary,
  showtimeLanguageOptions,
} from '../app/utils/showtimeFilters.ts'

const [planning, search, film] = await Promise.all([
  readFile(new URL('../app/pages/planning.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8'),
])

test('keeps canonical language and format ordering with explicit ALL labels', () => {
  assert.deepEqual(queryLanguageOptions, [
    { value: 'ALL', label: 'Toutes les langues' },
    { value: 'ORIGINAL', label: 'Version originale (VOF & VOSTFR)' },
    { value: 'VOF', label: 'VOF' },
    { value: 'VOSTFR', label: 'VOSTFR' },
    { value: 'VF', label: 'VF' },
  ])
  assert.deepEqual(queryLanguageValues, [
    'ALL',
    'ORIGINAL',
    'VOF',
    'VOSTFR',
    'VF',
  ])
  assert.deepEqual(filmLanguageValues, [
    'ORIGINAL',
    'VOF',
    'VOSTFR',
    'VF',
    'VO',
    'VF_SME',
    'VFSTF',
  ])
  assert.equal(
    showtimeLanguageValues.some(
      (value: string) => value === 'ORIGINAL' || value === 'VOF',
    ),
    false,
  )
  assert.deepEqual(
    showtimeLanguageOptions.map((option) => option.value),
    ['ALL', 'VOSTFR', 'VF', 'VO', 'VF_SME', 'VFSTF'],
  )
  assert.deepEqual(
    queryFormatOptions.map((option) => option.value),
    [
      'ALL',
      '2D',
      '3D',
      'IMAX',
      'DOLBY',
      'SCREENX',
      'LASER_ULTRA',
      '4DX',
      'ICE',
      'INFINITY_VISION',
    ],
  )
  assert.equal(queryLanguageOptions[0].label, 'Toutes les langues')
  assert.equal(queryFormatOptions[0]?.label, 'Tous les formats')
})

test('filters dynamic film options while preserving canonical order', () => {
  assert.deepEqual(
    availableLanguageOptions(['VF_SME', 'VOSTFR', 'VO']).map(
      (option) => option.value,
    ),
    ['ALL', 'VOSTFR', 'VO', 'VF_SME'],
  )
  assert.deepEqual(
    availableFormatOptions(['ICE', '2D', 'IMAX']).map((option) => option.value),
    ['ALL', '2D', 'IMAX', 'ICE'],
  )
  assert.deepEqual(
    availableFormatOptions(['INFINITY_VISION', 'ICE', 'INFINITY_VISION']).map(
      (option) => option.value,
    ),
    ['ALL', 'ICE', 'INFINITY_VISION'],
  )
  assert.deepEqual(
    availableFormatOptions([]).map((option) => option.value),
    ['ALL'],
  )
})

test('uses shared labels in compact summaries', () => {
  assert.equal(languageLabel('VF_SME'), 'VF SME')
  assert.equal(languageLabel('VFSTF'), 'VFSTF')
  assert.equal(showtimeFilterSummary('VFSTF', '3D'), 'VFSTF · 3D')
  assert.equal(
    showtimeFilterSummary('VF', 'INFINITY_VISION'),
    'VF · Infinity Vision',
  )
  assert.deepEqual(
    availableLanguageOptions(['VFSTF', 'VF']).map((option) => option.value),
    ['ALL', 'VF', 'VFSTF'],
  )
  assert.equal(
    showtimeFilterSummary('VF_SME', 'LASER_ULTRA'),
    'VF SME · Laser ULTRA by Kinepolis',
  )
  assert.equal(
    showtimeFilterSummary('ALL', 'ALL'),
    'Toutes les langues · Tous les formats',
  )
})

test('planning, recherche, and film consume shared presentation contracts', () => {
  assert.match(planning, /queryLanguageOptions/)
  assert.match(planning, /queryFormatOptions/)
  assert.match(search, /languageLabel\(search\.language\)/)
  assert.match(search, /formatLabel\(search\.format\)/)
  assert.match(film, /availableFilmLanguageOptions\(languages\.value\)/)
  assert.match(film, /availableFormatOptions\(technologyFormats\.value\)/)
  assert.doesNotMatch(film, />Technologie</)
})

test('ORIGINAL keeps all original audio while VOF matches only confirmed French VF-family sessions', () => {
  for (const original of ['fr', 'en', null, undefined]) {
    for (const language of [
      'VO',
      'VOSTFR',
      'VF',
      'VF_SME',
      'VFSTF',
      '',
      'UNKNOWN',
      'VOF',
    ]) {
      const expected =
        language === 'VO' ||
        language === 'VOSTFR' ||
        (original === 'fr' && ['VF', 'VF_SME', 'VFSTF'].includes(language))
      assert.equal(
        matchesFilmLanguageFilter(language, 'ORIGINAL', original),
        expected,
        `${language}/${original}`,
      )
      assert.equal(
        matchesFilmLanguageFilter(language, 'VOF', original),
        original === 'fr' && ['VF', 'VF_SME', 'VFSTF'].includes(language),
        `VOF: ${language}/${original}`,
      )
      assert.equal(matchesFilmLanguageFilter(language, 'ALL', original), true)
      for (const concrete of showtimeLanguageValues) {
        assert.equal(
          matchesFilmLanguageFilter(language, concrete, original),
          language === concrete,
        )
      }
    }
  }
})

test('context changes plain VF label only, preserving accessibility, silence and unknown labels', () => {
  for (const original of ['fr', 'en', null, undefined]) {
    assert.equal(
      languageLabel('VF', original),
      original === 'fr' ? 'VOF' : 'VF',
    )
    for (const [value, label] of [
      ['VF_SME', 'VF SME'],
      ['VFSTF', 'VFSTF'],
      ['VO', 'VO'],
      ['VOSTFR', 'VOSTFR'],
      ['', ''],
      ['UNKNOWN', 'UNKNOWN'],
    ]) {
      assert.equal(languageLabel(value!, original), label)
    }
    assert.equal(
      languageLabel('ORIGINAL', original),
      'Version originale (VOF & VOSTFR)',
    )
    assert.equal(languageLabel('VOF', original), 'VOF')
  }
  assert.equal(languageLabel('VF'), 'VF')
  assert.equal(showtimeFilterSummary('VF', 'ALL', 'fr'), 'VOF')
  assert.equal(
    showtimeFilterSummary('ORIGINAL', '3D', 'fr'),
    'Version originale (VOF & VOSTFR) · 3D',
  )
  assert.equal(showtimeFilterSummary('VOF', '3D', 'fr'), 'VOF · 3D')
  assert.equal(
    queryLanguageOptions.find((option) => option.value === 'VF')?.label,
    'VF',
  )
})

test('film keeps ORIGINAL and VOF available with zero or one concrete language and distinct VF labels', () => {
  for (const available of [[], [''], ['VF']] as const) {
    assert.deepEqual(
      availableFilmLanguageOptions(available).slice(0, 3),
      queryLanguageOptions.slice(0, 3),
    )
  }
  assert.deepEqual(
    availableFilmLanguageOptions(['VFSTF', 'VF_SME', 'VF', 'VO', 'VOSTFR']),
    [
      ...queryLanguageOptions.slice(0, 3),
      { value: 'VOSTFR', label: 'VOSTFR' },
      { value: 'VF', label: 'VF' },
      { value: 'VO', label: 'VO' },
      { value: 'VF_SME', label: 'VF SME' },
      { value: 'VFSTF', label: 'VFSTF' },
    ],
  )
})

test('every result layout, film, timeline and timeline accessible name use contextual labels', async () => {
  for (const [name, count] of [
    ['ShowtimeResultLine', 4],
    ['ShowtimeResultBox', 3],
  ] as const) {
    const source = await readFile(
      new URL(`../app/components/${name}.vue`, import.meta.url),
      'utf8',
    )
    assert.equal(
      source.match(
        /languageLabel\(result\.language, result\.movieOriginalLanguage\)/g,
      )?.length,
      count,
    )
    assert.doesNotMatch(source, /\{\{ result\.language \}\}/)
  }
  const timeline = await readFile(
    new URL('../app/components/TimelineMatrix.vue', import.meta.url),
    'utf8',
  )
  assert.match(
    timeline,
    /:aria-label="[^\n]*languageLabel\(item\.showtime\.language, item\.showtime\.movie\.original_language\)/,
  )
  assert.equal(
    timeline.match(
      /languageLabel\(item\.showtime\.language, item\.showtime\.movie\.original_language\)/g,
    )?.length,
    2,
  )
  assert.match(
    timeline,
    /languageLabel\(selected\.showtime\.language, selected\.showtime\.movie\.original_language\)/,
  )
  assert.match(
    film,
    /languageLabel\(showtime\.language, showtime\.movie\.original_language\)/,
  )
  assert.doesNotMatch(film, /v-if="languages\.length > 1"/)
  assert.match(film, /v-else-if="visibleTheaters\.length === 0"/)
})

test('planning hydrates shared query languages including VOF without accepting display or composite tokens', () => {
  assert.match(
    planning,
    /enumQueryValue\(\s*singularQueryValue\(route\.query\.language\),\s*queryLanguageValues,?\s*\)/,
  )
  assert.match(
    planning,
    /language: language\.value === 'ALL' \? undefined : language\.value/,
  )
  for (const language of queryLanguageValues) {
    assert.equal(
      enumQueryValue(singularQueryValue(language), queryLanguageValues),
      language,
    )
  }
  for (const language of ['vof', ' VOF ', 'VO', 'VOF,VOSTFR', 'UNKNOWN']) {
    assert.equal(
      enumQueryValue(singularQueryValue(language), queryLanguageValues),
      undefined,
    )
  }
})

test('planning and slot APIs serialize ORIGINAL and VOF once without changing existing language tokens', async () => {
  const urls: URL[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080' } }),
    $fetch: createFetch({
      fetch: async (input: RequestInfo | URL) => {
        urls.push(new URL(input instanceof Request ? input.url : String(input)))
        return new Response('[]', {
          headers: { 'content-type': 'application/json' },
        })
      },
    }),
  })
  try {
    const api = useMesSeancesApi()
    for (const language of queryLanguageValues) {
      await api.timeline({ date: '2027-06-27', language })
      assert.equal(urls.at(-1)?.pathname, '/api/v1/timeline')
      assert.deepEqual(urls.at(-1)?.searchParams.getAll('language'), [language])
      await api.searchSlot({
        date: '2027-06-27',
        start_after: '18:00',
        finish_before: '23:00',
        language,
      })
      assert.equal(urls.at(-1)?.pathname, '/api/v1/search/slot')
      assert.deepEqual(urls.at(-1)?.searchParams.getAll('language'), [language])
    }
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})

test('slot queries serialize Infinity Vision as one canonical scalar without changing ICE or ALL', async () => {
  const urls: URL[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080' } }),
    $fetch: createFetch({
      fetch: async (input: RequestInfo | URL) => {
        urls.push(new URL(input instanceof Request ? input.url : String(input)))
        return new Response('[]', {
          headers: { 'content-type': 'application/json' },
        })
      },
    }),
  })
  try {
    const api = useMesSeancesApi()
    for (const format of ['INFINITY_VISION', 'ICE', 'ALL']) {
      assert.ok(format === 'ALL' || isShowtimeFormat(format))
      await api.searchSlot({
        date: '2027-06-27',
        start_after: '18:00',
        finish_before: '23:00',
        format,
      })
      assert.equal(urls.at(-1)?.pathname, '/api/v1/search/slot')
      assert.deepEqual(urls.at(-1)?.searchParams.getAll('format'), [format])
    }
    await api.searchSlot({
      date: '2027-06-27',
      start_after: '18:00',
      finish_before: '23:00',
    })
    assert.equal(urls.at(-1)?.searchParams.has('format'), false)
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})
