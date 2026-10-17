import assert from 'node:assert/strict'
import test from 'node:test'
import { buildMovieExternalLinks } from '../app/utils/movieExternalLinks.ts'

test('builds exact movie links in TMDB, Letterboxd, IMDb, Box Office Mojo order', () => {
  assert.deepEqual(buildMovieExternalLinks(550, 'tt0137523'), [
    {
      destination: 'tmdb',
      label: 'TMDB',
      url: 'https://www.themoviedb.org/movie/550',
    },
    {
      destination: 'letterboxd',
      label: 'Letterboxd',
      url: 'https://letterboxd.com/tmdb/550',
    },
    {
      destination: 'imdb',
      label: 'IMDb',
      url: 'https://www.imdb.com/title/tt0137523/',
    },
    {
      destination: 'boxofficemojo',
      label: 'Box Office',
      url: 'https://www.boxofficemojo.com/title/tt0137523/?ref_=bo_rl_rl',
    },
  ])
})

test('builds the exact Box Office Mojo URL for the supplied IMDb example', () => {
  assert.deepEqual(buildMovieExternalLinks(null, 'tt4154796'), [
    {
      destination: 'imdb',
      label: 'IMDb',
      url: 'https://www.imdb.com/title/tt4154796/',
    },
    {
      destination: 'boxofficemojo',
      label: 'Box Office',
      url: 'https://www.boxofficemojo.com/title/tt4154796/?ref_=bo_rl_rl',
    },
  ])
})

test('appends the exact Metacritic link after the existing four destinations', () => {
  assert.deepEqual(
    buildMovieExternalLinks(550, 'tt0137523', 'movie/fight-club'),
    [
      ...buildMovieExternalLinks(550, 'tt0137523'),
      {
        destination: 'metacritic',
        label: 'Metacritic',
        url: 'https://www.metacritic.com/movie/fight-club/',
      },
    ],
  )
})

test('accepts exact ASCII Metacritic paths, punctuation and length boundaries', () => {
  const validIds = [
    'movie/a',
    'movie/0',
    'movie/abcdefghijklmnopqrstuvwxyz0123456789!+_()-',
    `movie/${'a'.repeat(249)}`,
  ]
  for (const metacriticId of validIds) {
    assert.deepEqual(buildMovieExternalLinks(550, null, metacriticId), [
      ...buildMovieExternalLinks(550, null),
      {
        destination: 'metacritic',
        label: 'Metacritic',
        url: `https://www.metacritic.com/${metacriticId}/`,
      },
    ])
  }
})

test('omits missing or malformed Metacritic IDs without changing other links', () => {
  const invalidIds = [
    null,
    undefined,
    '',
    'movie/',
    `movie/${'a'.repeat(250)}`,
    'movie/fight-club\n',
    'movie/fight-club\r\n',
    'movie/fight-club\r',
    'movie/fight-club\u2028',
    'movie/fight-club\u2029',
    'movie/fight\nclub',
    ' movie/fight-club',
    'movie/fight-club ',
    'movie/fight club',
    'movie/fight\tclub',
    'movie/fight-club\0',
    'movie/élan',
    'movie/Ｆight-club',
    'movie/Fight-club',
    'Movie/fight-club',
    'https://www.metacritic.com/movie/fight-club/',
    'game/fight-club',
    'movie/../fight-club',
    'movie/fight.club',
    'movie/fight%2fclub',
    'movie/fight%2Fclub',
    'movie/fight\\club',
    'movie/fight-club?x=1',
    'movie/fight-club#reviews',
    'movie/fight-club/',
    'movie/fight/club',
  ]
  for (const metacriticId of invalidIds) {
    assert.deepEqual(
      buildMovieExternalLinks(550, 'tt0137523', metacriticId),
      buildMovieExternalLinks(550, 'tt0137523'),
      String(metacriticId),
    )
  }
})

test('requires a positive safe TMDB identity for Metacritic even with valid IMDb', () => {
  const invalidTmdbIds = [
    null,
    undefined,
    0,
    -1,
    1.5,
    Number.NaN,
    Number.POSITIVE_INFINITY,
    Number.MAX_SAFE_INTEGER + 1,
  ]
  for (const tmdbId of invalidTmdbIds) {
    assert.deepEqual(
      buildMovieExternalLinks(tmdbId, 'tt0137523', 'movie/fight-club'),
      buildMovieExternalLinks(tmdbId, 'tt0137523'),
      String(tmdbId),
    )
    assert.deepEqual(
      buildMovieExternalLinks(tmdbId, null, 'movie/fight-club'),
      [],
      String(tmdbId),
    )
  }
})

test('omits unavailable destinations without placeholders', () => {
  assert.deepEqual(buildMovieExternalLinks(550, null), [
    {
      destination: 'tmdb',
      label: 'TMDB',
      url: 'https://www.themoviedb.org/movie/550',
    },
    {
      destination: 'letterboxd',
      label: 'Letterboxd',
      url: 'https://letterboxd.com/tmdb/550',
    },
  ])
  assert.deepEqual(buildMovieExternalLinks(null, 'tt0137523'), [
    {
      destination: 'imdb',
      label: 'IMDb',
      url: 'https://www.imdb.com/title/tt0137523/',
    },
    {
      destination: 'boxofficemojo',
      label: 'Box Office',
      url: 'https://www.boxofficemojo.com/title/tt0137523/?ref_=bo_rl_rl',
    },
  ])
  assert.deepEqual(buildMovieExternalLinks(null, null), [])
  assert.deepEqual(buildMovieExternalLinks(undefined, undefined), [])
  assert.deepEqual(
    buildMovieExternalLinks(550, undefined),
    buildMovieExternalLinks(550, null),
  )
})

test('rejects unsafe TMDB and malformed IMDb identifiers', () => {
  const invalidTmdbIds = [
    0,
    -1,
    1.5,
    Number.NaN,
    Number.POSITIVE_INFINITY,
    Number.MAX_SAFE_INTEGER + 1,
  ]
  for (const tmdbId of invalidTmdbIds)
    assert.deepEqual(buildMovieExternalLinks(tmdbId, null), [], String(tmdbId))

  const invalidImdbIds = [
    '',
    'tt123456',
    `tt${'1'.repeat(31)}`,
    'TT0137523',
    'tt013752x',
    ' tt0137523',
    'tt0137523 ',
    'https://www.imdb.com/title/tt0137523/',
  ]
  for (const imdbId of invalidImdbIds) {
    assert.deepEqual(buildMovieExternalLinks(null, imdbId), [], imdbId)
    assert.deepEqual(
      buildMovieExternalLinks(550, imdbId),
      buildMovieExternalLinks(550, null),
      imdbId,
    )
  }
})
