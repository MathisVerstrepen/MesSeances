import assert from 'node:assert/strict'
import test from 'node:test'
import {
  formatShortCalendarDate,
  movieCatalogFilterDraft,
  movieCatalogFiltersFromDraft,
  normalizeMovieGenres,
  parseMovieCatalogFilters,
  serializeMovieCatalogFilters
} from '../app/utils/movieCatalogFilters.ts'

const TODAY = '2026-08-26'

test('normalizes genre CSV and rejects repeated or malformed query values', () => {
  assert.deepEqual(parseMovieCatalogFilters({ genres: ' Drame,Comédie,drame, Animation ' }, TODAY).genres, ['Animation', 'Comédie', 'Drame'])
  assert.deepEqual(parseMovieCatalogFilters({ genres: ['Drame', 'Comédie'] }, TODAY).genres, [])
  assert.deepEqual(parseMovieCatalogFilters({ genres: 'Drame,,Comédie' }, TODAY).genres, [])
  assert.deepEqual(normalizeMovieGenres([' Drame ', '', 'drame', 'Animation']), ['Animation', 'Drame'])
  assert.equal(serializeMovieCatalogFilters({ genres: ['Drame', 'Animation', 'Drame'] }).genres, 'Animation,Drame')
})

test('hydrates duration values and omits unknown defaults', () => {
  assert.equal(parseMovieCatalogFilters({ duration: 'medium' }, TODAY).duration, 'medium')
  assert.equal(parseMovieCatalogFilters({ duration: 'exact' }, TODAY).duration, undefined)
  assert.equal(parseMovieCatalogFilters({ duration: ['short', 'long'] }, TODAY).duration, undefined)
  assert.deepEqual(serializeMovieCatalogFilters({ genres: [] }), { genres: undefined, duration: undefined, date: undefined, date_to: undefined })
})

test('hydrates presets, custom dates, and inclusive ranges canonically', () => {
  assert.deepEqual(parseMovieCatalogFilters({ date: 'today', date_to: '2026-08-30' }, TODAY), { genres: [], date: 'today' })
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-08-29' }, TODAY), { genres: [], date: '2026-08-29' })
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-08-29', date_to: '2026-09-02' }, TODAY), { genres: [], date: '2026-08-29', dateTo: '2026-09-02' })
  assert.deepEqual(parseMovieCatalogFilters({ date: ['today', 'tomorrow'] }, TODAY), { genres: [] })
})

test('removes past, malformed, inverted, and redundant date endpoints', () => {
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-08-25', date_to: '2026-08-30' }, TODAY), { genres: [] })
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-02-30' }, TODAY), { genres: [] })
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-08-29', date_to: '2026-08-28' }, TODAY), { genres: [], date: '2026-08-29' })
  assert.deepEqual(parseMovieCatalogFilters({ date: '2026-08-29', date_to: '2026-08-29' }, TODAY), { genres: [], date: '2026-08-29' })
  assert.deepEqual(parseMovieCatalogFilters({ date_to: '2026-08-29' }, TODAY), { genres: [] })
})

test('round-trips applied filters through preset, custom, and range drafts', () => {
  const preset = movieCatalogFilterDraft({ genres: ['Drame'], duration: 'short', date: 'weekend' }, TODAY)
  assert.equal(preset.dateMode, 'weekend')
  assert.deepEqual(movieCatalogFiltersFromDraft(preset, TODAY), { genres: ['Drame'], duration: 'short', date: 'weekend' })

  const custom = movieCatalogFilterDraft({ genres: [], date: '2026-08-30' }, TODAY)
  assert.equal(custom.dateMode, 'custom')
  assert.deepEqual(movieCatalogFiltersFromDraft(custom, TODAY), { genres: [], duration: undefined, date: '2026-08-30' })

  const range = movieCatalogFilterDraft({ genres: [], date: '2026-08-30', dateTo: '2026-09-02' }, TODAY)
  assert.equal(range.dateMode, 'range')
  assert.deepEqual(movieCatalogFiltersFromDraft(range, TODAY), { genres: [], duration: undefined, date: '2026-08-30', dateTo: '2026-09-02' })
  range.rangeEnd = range.rangeStart
  assert.deepEqual(movieCatalogFiltersFromDraft(range, TODAY), { genres: [], duration: undefined, date: '2026-08-30' })
})

test('rejects incomplete, past, and inverted custom drafts', () => {
  const custom = movieCatalogFilterDraft({ genres: [] }, TODAY)
  custom.dateMode = 'custom'
  custom.customDate = ''
  assert.equal(movieCatalogFiltersFromDraft(custom, TODAY), null)
  custom.customDate = '2026-08-25'
  assert.equal(movieCatalogFiltersFromDraft(custom, TODAY), null)

  const range = movieCatalogFilterDraft({ genres: [] }, TODAY)
  range.dateMode = 'range'
  range.rangeStart = '2026-08-30'
  range.rangeEnd = '2026-08-29'
  assert.equal(movieCatalogFiltersFromDraft(range, TODAY), null)
})

test('formats valid ISO dates as visible dd-MM-yy values', () => {
  assert.equal(formatShortCalendarDate('2026-08-26'), '26-08-26')
  assert.equal(formatShortCalendarDate('invalid'), 'invalid')
})
