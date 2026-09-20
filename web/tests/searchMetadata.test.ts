import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildSearchMetaDescription,
  DEFAULT_SEARCH_DESCRIPTION,
} from '../app/utils/searchMetadata.ts'

test('describes complete shared search parameters', () => {
  assert.equal(
    buildSearchMetaDescription({
      theaters: 'ugc-46,ugc-45,ugc-25',
      date: '2026-09-21',
      start_after: '12:45',
      finish_before: '21:30',
      language: 'VOSTFR',
      format: 'IMAX',
    }),
    'Séances dans 3 cinémas le lundi 21 septembre, de 12h45 à 21h30. Filtres : VOSTFR, IMAX.',
  )
})

test('keeps complete default-filter searches concise', () => {
  assert.equal(
    buildSearchMetaDescription({
      theaters: 'ugc-46',
      date: '2026-09-21',
      start_after: '12:45',
      finish_before: '21:30',
      language: 'ALL',
      format: 'ALL',
    }),
    'Séances dans 1 cinéma le lundi 21 septembre, de 12h45 à 21h30.',
  )
})

test('does not reflect malformed or incomplete query values into metadata', () => {
  for (const query of [
    {},
    {
      theaters: '<script>',
      date: '2026-09-21',
      start_after: '12:45',
      finish_before: '21:30',
    },
    {
      theaters: 'ugc-46',
      date: 'invalid',
      start_after: '12:45',
      finish_before: '21:30',
    },
    {
      theaters: 'ugc-46',
      date: '2026-09-21',
      start_after: ['12:45'],
      finish_before: '21:30',
    },
  ])
    assert.equal(buildSearchMetaDescription(query), DEFAULT_SEARCH_DESCRIPTION)
})
