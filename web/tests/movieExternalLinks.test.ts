import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
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

test('menu exposes safe links and accessible menu-button semantics', async () => {
  const component = await readFile(
    new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url),
    'utf8',
  )

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
  assert.match(component, /ref="root"\s+class="relative shrink-0"/u)
  assert.doesNotMatch(component, /class="absolute right-4 top-4/u)
})

test('menu renders every service logo decoratively beside its visible label', async () => {
  const component = await readFile(
    new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url),
    'utf8',
  )

  assert.match(component, /IMDb_logo\.svg\?no-inline/u)
  assert.match(component, /letterboxd_logo\.svg\?no-inline/u)
  assert.match(component, /logo_tmdb\.svg\?no-inline/u)
  assert.match(component, /box_office_mojo\.webp\?no-inline/u)
  assert.match(component, /metacritic\.svg\?no-inline/u)
  assert.match(component, /tmdb: tmdbLogo/u)
  assert.match(component, /letterboxd: letterboxdLogo/u)
  assert.match(component, /imdb: imdbLogo/u)
  assert.match(component, /boxofficemojo: boxOfficeMojoLogo/u)
  assert.match(component, /metacritic: metacriticLogo/u)
  assert.match(
    component,
    /aria-hidden="true"\s*>\s*<img\s+:src="serviceLogos\[link\.destination\]"\s+alt=""/u,
  )
  assert.match(component, /class="max-h-5 max-w-8 object-contain"/u)
  assert.match(component, /<span>\{\{ link\.label \}\}<\/span>/u)
})

test('menu implements keyboard focus and all dismissal paths', async () => {
  const component = await readFile(
    new URL('../app/components/MovieExternalLinksMenu.vue', import.meta.url),
    'utf8',
  )

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
  assert.match(
    component,
    /document\.addEventListener\('pointerdown', handleDocumentPointerDown\)/u,
  )
  assert.match(
    component,
    /document\.removeEventListener\('pointerdown', handleDocumentPointerDown\)/u,
  )
  assert.match(
    component,
    /document\.addEventListener\('keydown', handleDocumentKeydown\)/u,
  )
  assert.match(
    component,
    /document\.removeEventListener\('keydown', handleDocumentKeydown\)/u,
  )
  assert.match(component, /@click="closeMenu\(\{ restoreFocus: true \}\)"/u)
})

test('movie page integrates menu while passing only TMDB external identity to film JSON-LD', async () => {
  const page = await readFile(
    new URL('../app/pages/film/[slug].vue', import.meta.url),
    'utf8',
  )

  assert.match(
    page,
    /buildMovieExternalLinks\(\s*schedule\.value\?\.movie\.tmdb_id,\s*schedule\.value\?\.movie\.imdb_id,\s*schedule\.value\?\.movie\.metacritic_id,?\s*\)/u,
  )
  assert.match(
    page,
    /<MovieExternalLinksMenu\s+:links="externalLinks"\s+:movie-title="schedule\.movie\.title"\s*\/>/u,
  )
  assert.match(page, /externalLinks\.length \? 'sm:pr-28' : 'sm:pr-16'/u)
  assert.match(page, /tmdbUrl: tmdbUrl\.value \|\| undefined/u)
  assert.doesNotMatch(page, /logo_tmdb/u)
  assert.doesNotMatch(page, /(?:sameAs|tmdbUrl): externalLinks/u)
})

test('catalog API type requires a nullable Metacritic ID', async () => {
  const api = await readFile(
    new URL('../app/types/api.ts', import.meta.url),
    'utf8',
  )
  assert.match(
    api,
    /export interface CatalogMovie extends Movie \{[^}]*\n  metacritic_id: string \| null\n/u,
  )
})

test('Metacritic asset retains supplied SVG geometry and colors', async () => {
  const svg = await readFile(
    new URL('../app/assets/imgs/metacritic.svg', import.meta.url),
    'utf8',
  )
  assert.match(svg, /viewBox="0 0 40 40"/u)
  assert.deepEqual(
    [...svg.matchAll(/<path\s+([^>]+)>/gu)].map(([, attributes]) => ({
      d: attributes?.match(/\bd="([^"]+)"/u)?.[1],
      fill: attributes?.match(/\bfill="([^"]+)"/u)?.[1] ?? 'black',
    })),
    [
      {
        d: 'M36.978 19.49a17.49 17.49 0 1 1 0-.021',
        fill: 'black',
      },
      {
        d: 'm17.209 32.937 3.41-3.41-6.567-6.567c-.276-.276-.576-.622-.737-1.014-.369-.783-.53-2.004.369-2.903 1.106-1.106 2.58-.645 4.009.784l6.313 6.313 3.41-3.41-6.59-6.59c-.276-.276-.599-.691-.76-1.037-.438-.898-.415-2.027.392-2.834 1.129-1.129 2.603-.714 4.24.922l6.128 6.129 3.41-3.41L27.6 9.274c-3.364-3.364-6.52-3.249-8.686-1.083-.83.83-1.337 1.705-1.59 2.696a6.7 6.7 0 0 0-.092 2.81l-.046.047c-1.66-.691-3.549-.277-5 1.175-1.936 1.935-1.866 3.986-1.636 5.184l-.07.07-1.681-1.36-2.95 2.949c1.037.945 2.282 2.097 3.687 3.502z',
        fill: '#f2f2f2',
      },
      {
        d: 'M19.982 0A20 20 0 1 0 40 20v-.024A20 20 0 0 0 19.982 0m-.091 4.274A15.665 15.665 0 0 1 35.57 19.921v.018A15.665 15.665 0 1 1 19.89 4.274Z',
        fill: '#ffbd3f',
      },
    ],
  )
  assert.doesNotMatch(svg, /<(?:script|image|foreignObject)\b|\bon\w+=/u)
})
