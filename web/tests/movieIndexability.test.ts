import assert from 'node:assert/strict'
import test from 'node:test'
import type { CatalogMovie } from '../app/types/api.ts'
import { hasSubstantialEvergreenMovieMetadata, isIndexableMovie } from '../app/utils/movieIndexability.ts'

function movie(overrides: Partial<CatalogMovie> = {}): CatalogMovie {
  return {
    slug: 'film-42',
    title: 'Film test',
    runtime_minutes: 100,
    updated_at: '2026-08-30T10:00:00Z',
    poster_url: ' https://example.test/poster.jpg ',
    tmdb_id: null,
    imdb_id: null,
    overview: ' Une histoire durable. ',
    release_date: '2026-02-28',
    genres: [' Drame '],
    ...overrides
  }
}

test('current films stay indexable without evergreen metadata', () => {
  const current = movie({ overview: null, release_date: null, genres: [], poster_url: null })
  assert.equal(isIndexableMovie(current, true), true)
})

test('ended films require every core metadata field and one identity signal', () => {
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie()), true)
  assert.equal(isIndexableMovie(movie(), false), true)

  for (const thin of [
    movie({ overview: '  ' }),
    movie({ release_date: '2026-02-29' }),
    movie({ genres: ['  '] }),
    movie({ poster_url: null, tmdb_id: null, imdb_id: null })
  ]) {
    assert.equal(hasSubstantialEvergreenMovieMetadata(thin), false)
    assert.equal(isIndexableMovie(thin, false), false)
  }
})

test('positive safe TMDB or nonblank IMDb identity can replace poster', () => {
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, tmdb_id: 42 })), true)
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, imdb_id: ' tt0000042 ' })), true)
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, tmdb_id: 0 })), false)
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, tmdb_id: -1 })), false)
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, tmdb_id: 1.5 })), false)
  assert.equal(hasSubstantialEvergreenMovieMetadata(movie({ poster_url: null, tmdb_id: Number.MAX_SAFE_INTEGER + 1 })), false)
})

test('qualification does not mutate movie metadata', () => {
  const candidate = movie()
  const before = structuredClone(candidate)
  isIndexableMovie(candidate, false)
  assert.deepEqual(candidate, before)
})
