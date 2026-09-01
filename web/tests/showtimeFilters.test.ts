import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
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
  assert.deepEqual(showtimeLanguageOptions.map((option) => option.value), ['ALL', 'VOSTFR', 'VF', 'VO', 'VF_SME'])
  assert.deepEqual(queryFormatOptions.map((option) => option.value), ['ALL', '2D', '3D', 'IMAX', 'DOLBY', 'SCREENX', 'LASER_ULTRA', '4DX', 'ICE'])
  assert.equal(queryLanguageOptions[0].label, 'Toutes les langues')
  assert.equal(queryFormatOptions[0]?.label, 'Tous les formats')
})

test('filters dynamic film options while preserving canonical order', () => {
  assert.deepEqual(availableLanguageOptions(['VF_SME', 'VOSTFR', 'VO']).map((option) => option.value), ['ALL', 'VOSTFR', 'VO', 'VF_SME'])
  assert.deepEqual(availableFormatOptions(['ICE', '2D', 'IMAX']).map((option) => option.value), ['ALL', '2D', 'IMAX', 'ICE'])
})

test('uses shared labels in compact summaries', () => {
  assert.equal(languageLabel('VF_SME'), 'VF SME')
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
