import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import { buildMovieExternalLinks } from '../app/utils/movieExternalLinks.ts'

test('builds exact movie links in TMDB, Letterboxd, IMDb order', () => {
  assert.deepEqual(buildMovieExternalLinks(550, 'tt0137523'), [
    { destination: 'tmdb', label: 'TMDB', url: 'https://www.themoviedb.org/movie/550' },
    { destination: 'letterboxd', label: 'Letterboxd', url: 'https://letterboxd.com/tmdb/550' },
    { destination: 'imdb', label: 'IMDb', url: 'https://www.imdb.com/title/tt0137523/' }
  ])
})

test('omits unavailable destinations without placeholders', () => {
  assert.deepEqual(buildMovieExternalLinks(550, null), [
    { destination: 'tmdb', label: 'TMDB', url: 'https://www.themoviedb.org/movie/550' },
    { destination: 'letterboxd', label: 'Letterboxd', url: 'https://letterboxd.com/tmdb/550' }
  ])
  assert.deepEqual(buildMovieExternalLinks(null, 'tt0137523'), [
    { destination: 'imdb', label: 'IMDb', url: 'https://www.imdb.com/title/tt0137523/' }
  ])
  assert.deepEqual(buildMovieExternalLinks(null, null), [])
})

test('rejects unsafe TMDB and malformed IMDb identifiers', () => {
  const invalidTmdbIds = [0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, Number.MAX_SAFE_INTEGER + 1]
  for (const tmdbId of invalidTmdbIds) assert.deepEqual(buildMovieExternalLinks(tmdbId, null), [], String(tmdbId))

  const invalidImdbIds = [
    '',
    'tt123456',
    `tt${'1'.repeat(31)}`,
    'TT0137523',
    'tt013752x',
    ' tt0137523',
    'tt0137523 ',
    'https://www.imdb.com/title/tt0137523/'
  ]
  for (const imdbId of invalidImdbIds) assert.deepEqual(buildMovieExternalLinks(null, imdbId), [], imdbId)
})

test('menu exposes safe links and accessible menu-button semantics', async () => {
  const component = await readFile(new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url), 'utf8')

  assert.match(component, /v-if="links\.length"/u)
  assert.match(component, /aria-haspopup="menu"/u)
  assert.match(component, /:aria-controls="menuId"/u)
  assert.match(component, /:aria-expanded="isOpen"/u)
  assert.match(component, /Voir les liens externes de/u)
  assert.match(component, /role="menu"/u)
  assert.match(component, /role="menuitem"/u)
  assert.match(component, /target="_blank"/u)
  assert.match(component, /rel="noopener noreferrer"/u)
  assert.match(component, /dans un nouvel onglet/u)
  assert.match(component, /<ExternalLink/u)
})

test('menu renders every service logo decoratively beside its visible label', async () => {
  const component = await readFile(new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url), 'utf8')

  assert.match(component, /IMDb_logo\.svg\?no-inline/u)
  assert.match(component, /letterboxd_logo\.svg\?no-inline/u)
  assert.match(component, /logo_tmdb\.svg\?no-inline/u)
  assert.match(component, /tmdb: tmdbLogo/u)
  assert.match(component, /letterboxd: letterboxdLogo/u)
  assert.match(component, /imdb: imdbLogo/u)
  assert.match(component, /aria-hidden="true">\s*<img :src="serviceLogos\[link\.destination\]" alt=""/u)
  assert.match(component, /class="max-h-5 max-w-8 object-contain"/u)
  assert.match(component, /<span>\{\{ link\.label \}\}<\/span>/u)
})

test('menu implements keyboard focus and all dismissal paths', async () => {
  const component = await readFile(new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url), 'utf8')

  assert.match(component, /await nextTick\(\)/u)
  assert.match(component, /focus === 'last' \? props\.links\.length - 1 : 0/u)
  assert.match(component, /event\.key === 'ArrowUp' \? 'last' : 'first'/u)
  assert.match(component, /event\.key === 'ArrowDown'/u)
  assert.match(component, /event\.key === 'ArrowUp'/u)
  assert.match(component, /event\.key === 'Home'/u)
  assert.match(component, /event\.key === 'End'/u)
  assert.match(component, /event\.key === 'Escape'/u)
  assert.match(component, /event\.key === 'Tab'/u)
  assert.match(component, /closeMenu\(\{ restoreFocus: true \}\)/u)
  assert.match(component, /@focusout="handleFocusOut"/u)
  assert.match(component, /document\.addEventListener\('pointerdown', handleDocumentPointerDown\)/u)
  assert.match(component, /document\.removeEventListener\('pointerdown', handleDocumentPointerDown\)/u)
  assert.match(component, /document\.addEventListener\('keydown', handleDocumentKeydown\)/u)
  assert.match(component, /document\.removeEventListener\('keydown', handleDocumentKeydown\)/u)
  assert.match(component, /@click="closeMenu\(\{ restoreFocus: true \}\)"/u)
})

test('movie page integrates menu while keeping TMDB-only JSON-LD', async () => {
  const page = await readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8')

  assert.match(page, /buildMovieExternalLinks\(schedule\.value\?\.movie\.tmdb_id, schedule\.value\?\.movie\.imdb_id\)/u)
  assert.match(page, /<MovieExternalLinksMenu :links="externalLinks" :movie-title="schedule\.movie\.title" \/>/u)
  assert.match(page, /externalLinks\.length \? 'sm:pr-16'/u)
  assert.match(page, /if \(tmdbUrl\.value\) movie\.sameAs = tmdbUrl\.value/u)
  assert.doesNotMatch(page, /logo_tmdb/u)
  assert.doesNotMatch(page, /movie\.sameAs = externalLinks/u)
})
