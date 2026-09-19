import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { createFetch } from 'ofetch'
import { useMesSeancesApi } from '../app/composables/useMesSeancesApi.ts'
import { isShowtimeFormat } from '../app/utils/formats.ts'
import {
  availableFormatOptions,
  availableLanguageOptions,
  languageLabel,
  queryFormatOptions,
  queryLanguageOptions,
  showtimeFilterSummary,
  showtimeLanguageOptions
} from '../app/utils/showtimeFilters.ts'

const [planning, search, film] = await Promise.all([
  readFile(new URL('../app/pages/planning.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/recherche.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8')
])

test('keeps canonical language and format ordering with explicit ALL labels', () => {
  assert.deepEqual(queryLanguageOptions.map((option) => option.value), ['ALL', 'VOSTFR', 'VF'])
  assert.deepEqual(showtimeLanguageOptions.map((option) => option.value), ['ALL', 'VOSTFR', 'VF', 'VO', 'VF_SME', 'VFSTF'])
  assert.deepEqual(queryFormatOptions.map((option) => option.value), ['ALL', '2D', '3D', 'IMAX', 'DOLBY', 'SCREENX', 'LASER_ULTRA', '4DX', 'ICE', 'INFINITY_VISION'])
  assert.equal(queryLanguageOptions[0].label, 'Toutes les langues')
  assert.equal(queryFormatOptions[0]?.label, 'Tous les formats')
})

test('filters dynamic film options while preserving canonical order', () => {
  assert.deepEqual(availableLanguageOptions(['VF_SME', 'VOSTFR', 'VO']).map((option) => option.value), ['ALL', 'VOSTFR', 'VO', 'VF_SME'])
  assert.deepEqual(availableFormatOptions(['ICE', '2D', 'IMAX']).map((option) => option.value), ['ALL', '2D', 'IMAX', 'ICE'])
  assert.deepEqual(availableFormatOptions(['INFINITY_VISION', 'ICE', 'INFINITY_VISION']).map((option) => option.value), ['ALL', 'ICE', 'INFINITY_VISION'])
  assert.deepEqual(availableFormatOptions([]).map((option) => option.value), ['ALL'])
})

test('uses shared labels in compact summaries', () => {
  assert.equal(languageLabel('VF_SME'), 'VF SME')
  assert.equal(languageLabel('VFSTF'), 'VFSTF')
  assert.equal(showtimeFilterSummary('VFSTF', '3D'), 'VFSTF · 3D')
  assert.equal(showtimeFilterSummary('VF', 'INFINITY_VISION'), 'VF · Infinity Vision')
  assert.deepEqual(availableLanguageOptions(['VFSTF', 'VF']).map((option) => option.value), ['ALL', 'VF', 'VFSTF'])
  assert.equal(showtimeFilterSummary('VF_SME', 'LASER_ULTRA'), 'VF SME · Laser ULTRA by Kinepolis')
  assert.equal(showtimeFilterSummary('ALL', 'ALL'), 'Toutes les langues · Tous les formats')
})

test('planning, recherche, and film consume shared presentation contracts', () => {
  assert.match(planning, /queryLanguageOptions/)
  assert.match(planning, /queryFormatOptions/)
  assert.match(search, /languageLabel\(search\.language\)/)
  assert.match(search, /formatLabel\(search\.format\)/)
  assert.match(film, /availableLanguageOptions\(languages\.value\)/)
  assert.match(film, /availableFormatOptions\(technologyFormats\.value\)/)
  assert.doesNotMatch(film, />Technologie</)
})

test('slot queries serialize Infinity Vision as one canonical scalar without changing ICE or ALL', async () => {
  const urls: URL[] = []
  Object.assign(globalThis, {
    useRuntimeConfig: () => ({ public: { apiBase: 'http://localhost:8080' } }),
    $fetch: createFetch({ fetch: async (input: RequestInfo | URL) => {
      urls.push(new URL(input instanceof Request ? input.url : String(input)))
      return new Response('[]', { headers: { 'content-type': 'application/json' } })
    } })
  })
  try {
    const api = useMesSeancesApi()
    for (const format of ['INFINITY_VISION', 'ICE', 'ALL']) {
      assert.ok(format === 'ALL' || isShowtimeFormat(format))
      await api.searchSlot({ date: '2027-06-27', start_after: '18:00', finish_before: '23:00', format })
      assert.equal(urls.at(-1)?.pathname, '/api/v1/search/slot')
      assert.deepEqual(urls.at(-1)?.searchParams.getAll('format'), [format])
    }
    await api.searchSlot({ date: '2027-06-27', start_after: '18:00', finish_before: '23:00' })
    assert.equal(urls.at(-1)?.searchParams.has('format'), false)
  } finally {
    Reflect.deleteProperty(globalThis, '$fetch')
    Reflect.deleteProperty(globalThis, 'useRuntimeConfig')
  }
})
