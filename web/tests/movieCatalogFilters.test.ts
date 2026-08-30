import assert from 'node:assert/strict'
import test from 'node:test'
import {
  hasMovieCatalogFilters,
  movieCatalogFilterDraft,
  movieCatalogFiltersFromDraft,
  movieCatalogFiltersKey,
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
  assert.deepEqual(serializeMovieCatalogFilters({ genres: [] }), { genres: undefined, all_theaters: undefined, duration: undefined, date: undefined, date_to: undefined })
})

test('hydrates and serializes only the canonical national theater scope', () => {
  assert.deepEqual(parseMovieCatalogFilters({}, TODAY), { genres: [] })
  assert.deepEqual(parseMovieCatalogFilters({ all_theaters: '1' }, TODAY), { genres: [], allTheaters: true })
  assert.deepEqual(parseMovieCatalogFilters({ all_theaters: '1', date: 'today' }, TODAY), { genres: [], allTheaters: true, date: 'today' })

  for (const all_theaters of ['', '0', 'true', 'false', '2', ['1', '1']]) {
    assert.deepEqual(parseMovieCatalogFilters({ all_theaters }, TODAY), { genres: [] })
  }

  const serialized = serializeMovieCatalogFilters({ genres: ['Drame'], allTheaters: true, duration: 'medium', date: 'today' })
  assert.deepEqual(serialized, { genres: 'Drame', all_theaters: '1', duration: 'medium', date: 'today', date_to: undefined })
  assert.deepEqual(parseMovieCatalogFilters(serialized, TODAY), { genres: ['Drame'], allTheaters: true, duration: 'medium', date: 'today' })
})

test('round-trips national scope through drafts, active state, and load keys', () => {
  const inactiveDraft = movieCatalogFilterDraft({ genres: [] }, TODAY)
  assert.equal(inactiveDraft.allTheaters, false)
  assert.deepEqual(movieCatalogFiltersFromDraft(inactiveDraft, TODAY), { genres: [], duration: undefined })
  assert.equal(hasMovieCatalogFilters({ genres: [] }), false)

  const activeDraft = movieCatalogFilterDraft({ genres: [], allTheaters: true }, TODAY)
  assert.equal(activeDraft.allTheaters, true)
  assert.deepEqual(movieCatalogFiltersFromDraft(activeDraft, TODAY), { genres: [], duration: undefined, allTheaters: true })
  assert.equal(hasMovieCatalogFilters({ genres: [], allTheaters: true }), true)
  assert.notEqual(movieCatalogFiltersKey({ genres: [] }), movieCatalogFiltersKey({ genres: [], allTheaters: true }))
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
