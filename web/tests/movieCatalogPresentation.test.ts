import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { CatalogMovie } from '../app/types/api.ts'
import {
  filterAndSortCatalogMovies,
  movieCatalogSortOptions,
  movieOriginalTitleSubtitle,
} from '../app/utils/movieCatalogPresentation.ts'

const [
  card,
  controls,
  pagination,
  films,
  city,
  cinema,
  film,
  watchlistPage,
  watchlist,
  accountApi,
  publicApi,
] = await Promise.all([
  readFile(
    new URL('../app/components/MovieCatalogCard.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/components/MovieCatalogControls.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/components/MovieCatalogPagination.vue', import.meta.url),
    'utf8',
  ),
  readFile(new URL('../app/pages/films/index.vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../app/pages/ville/[slug]/cinemas.vue', import.meta.url),
    'utf8',
  ),
  readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/film/[slug].vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../app/pages/compte/watchlist.vue', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/composables/useWatchlist.ts', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/composables/useAccountApi.ts', import.meta.url),
    'utf8',
  ),
  readFile(
    new URL('../app/composables/useMesSeancesApi.ts', import.meta.url),
    'utf8',
  ),
])

const corpus: {
  name: string
  query: string
  title: string
  original_title: string | null
  expected: boolean
}[] = JSON.parse(
  await readFile(
    new URL('./fixtures/movie-title-search.json', import.meta.url),
    'utf8',
  ),
)

function movie(overrides: Partial<CatalogMovie>): CatalogMovie {
  return {
    slug: 'film',
    title: 'Film',
    original_language: null,
    runtime_minutes: 90,
    updated_at: '',
    poster_url: null,
    tmdb_id: null,
    imdb_id: null,
    metacritic_id: null,
    overview: null,
    release_date: null,
    french_release_date: null,
    genres: [],
    ...overrides,
  }
}

test('shares canonical catalog sort ordering', () => {
  assert.deepEqual(
    movieCatalogSortOptions.map(({ value }) => value),
    [
      'title_asc',
      'title_desc',
      'release_date_desc',
      'runtime_asc',
      'runtime_desc',
      'showtimes_desc',
    ],
  )
})

test('movie detail genre chips link to the filtered film catalog', () => {
  const genres = film.match(
    /<ul[^>]+aria-label="Genres"\s*>([\s\S]*?)<\/ul>/,
  )?.[1]
  assert.ok(genres)
  assert.match(genres, /v-for="genre in schedule\.movie\.genres"/)
  assert.match(genres, /path: '\/films',\s*query: \{ genres: genre \}/)
  assert.match(genres, /hash: isUpcomingFilm \? undefined : '#tous-les-films'/)
  assert.match(
    films,
    /id="tous-les-films"\s+ref="resultsSection"\s+class="scroll-mt-4"/,
  )
  assert.match(genres, /focus-visible:outline-2/)
  assert.match(genres, /\{\{ genre \}\}[\s\S]*<\/NuxtLink>/)
})

test('filters titles without case or diacritics and applies deterministic sort tie-breakers', () => {
  const movies = [
    movie({ slug: 'z', title: 'Été', runtime_minutes: 100, showtime_count: 2 }),
    movie({
      slug: 'b',
      title: 'Alpha',
      runtime_minutes: 100,
      showtime_count: 3,
    }),
    movie({
      slug: 'a',
      title: 'Alpha',
      runtime_minutes: 100,
      showtime_count: 3,
    }),
  ]
  assert.deepEqual(
    filterAndSortCatalogMovies(movies, ' ETE ', 'title_asc').map(
      ({ slug }) => slug,
    ),
    ['z'],
  )
  assert.deepEqual(
    filterAndSortCatalogMovies(movies, '', 'showtimes_desc').map(
      ({ slug }) => slug,
    ),
    ['a', 'b', 'z'],
  )
})

test('original-title subtitles show distinct trimmed titles only', () => {
  assert.equal(
    movieOriginalTitleSubtitle(
      movie({ title: "L'Invitation", original_title: '  The Invite  ' }),
    ),
    'The Invite',
  )
  for (const original_title of [
    undefined,
    null,
    '',
    ' \t\n ',
    "L'Invitation",
  ]) {
    assert.equal(
      movieOriginalTitleSubtitle(
        movie({ title: "L'Invitation", original_title }),
      ),
      '',
    )
  }
  assert.equal(
    movieOriginalTitleSubtitle(
      movie({ title: 'Le Grand Bleu', original_title: '  LE\tgrand  BLEU\n' }),
    ),
    '',
  )
  assert.equal(
    movieOriginalTitleSubtitle(movie({ title: 'Été', original_title: 'Ete' })),
    'Ete',
  )
})

test('film detail retains a smaller muted subtitle while catalog cards show only primary titles', () => {
  const subtitle = film.match(
    /<\/h1>\s*<p\s+v-if="originalTitleSubtitle"[\s\S]*?<\/p>/,
  )?.[0]
  assert.ok(subtitle)
  assert.match(subtitle, /text-lg/)
  assert.match(subtitle, /opacity-70/)
  assert.match(subtitle, /\{\{ originalTitleSubtitle \}\}/)
  assert.match(film, /const originalTitleSubtitle = computed\(/)
  assert.match(film, /movieOriginalTitleSubtitle\(schedule\.value\.movie\)/)
  assert.match(card, /<h3[^>]*>\s*\{\{ movie\.title \}\}\s*<\/h3>/)
  assert.doesNotMatch(
    card,
    /original_title|originalTitleSubtitle|movieOriginalTitleSubtitle/,
  )
})

test('local catalog matches French or original titles with case and accent normalization', () => {
  const movies = [
    movie({
      slug: 'invite',
      title: "L'Invitation",
      original_title: 'The Invite',
    }),
    movie({ slug: 'summer', title: 'Été', original_title: 'Sommerträume' }),
    movie({ slug: 'missing', title: 'Sans original' }),
    movie({ slug: 'null', title: 'Titre absent', original_title: null }),
    movie({ slug: 'blank', title: 'Titre vide', original_title: '  ' }),
  ]
  for (const [search, expected] of [
    [' INVITE ', 'invite'],
    ["l'invitation", 'invite'],
    [' SOMMERTRAUME ', 'summer'],
    ['sömmerträume', 'summer'],
    [' ETE ', 'summer'],
    ['ÉTÉ', 'summer'],
    ['SANS ORIGINAL', 'missing'],
    ['TITRE ABSENT', 'null'],
    ['titre vide', 'blank'],
  ]) {
    assert.deepEqual(
      filterAndSortCatalogMovies(movies, search, 'title_asc').map(
        ({ slug }) => slug,
      ),
      [expected],
    )
  }
  assert.deepEqual(
    filterAndSortCatalogMovies(movies, 'unknown', 'title_asc'),
    [],
  )
})

test('cinema-local filtering matches the shared Go corpus', async (t) => {
  for (const fixture of corpus) {
    await t.test(fixture.name, () => {
      const candidate = movie({
        title: fixture.title,
        original_title: fixture.original_title,
      })
      assert.deepEqual(
        filterAndSortCatalogMovies([candidate], fixture.query, 'title_asc'),
        fixture.expected ? [candidate] : [],
      )
    })
  }
})

test('local filtering never borrows search words across movies or title fields', () => {
  const movies = [
    movie({ slug: 'split', title: 'Spider', original_title: 'Man' }),
    movie({ slug: 'spider', title: 'Spider' }),
    movie({ slug: 'man', title: 'Man' }),
    movie({ slug: 'primary', title: 'Spider-Man' }),
    movie({ slug: 'both', title: 'Spider-Man', original_title: 'Spider-Man' }),
    movie({ slug: 'original', title: 'Un film', original_title: 'Spider-Man' }),
  ]
  assert.deepEqual(
    filterAndSortCatalogMovies(movies, 'man spid', 'title_asc').map(
      ({ slug }) => slug,
    ),
    ['both', 'primary', 'original'],
  )
  assert.deepEqual(filterAndSortCatalogMovies(movies, '%_', 'title_asc'), [])
})

test('all sort modes preserve ordering, tie-breakers and nonmutating catalog copies', () => {
  const movies = Object.freeze([
    Object.freeze(
      movie({
        slug: 'z',
        title: 'Zèbre match',
        runtime_minutes: 120,
        showtime_count: 1,
        release_date: '2026-10-01',
      }),
    ),
    Object.freeze(
      movie({
        slug: 'b',
        title: 'Alpha match',
        runtime_minutes: 90,
        showtime_count: 3,
        release_date: '2025-10-01',
      }),
    ),
    Object.freeze(
      movie({
        slug: 'a',
        title: 'Alpha match',
        runtime_minutes: 90,
        showtime_count: 3,
        release_date: '2025-10-01',
      }),
    ),
    Object.freeze(
      movie({ slug: 'm', title: 'Milieu match', runtime_minutes: 0 }),
    ),
  ])
  const before = structuredClone(movies)
  const expected = {
    title_asc: ['a', 'b', 'm', 'z'],
    title_desc: ['z', 'm', 'b', 'a'],
    release_date_desc: ['z', 'a', 'b', 'm'],
    runtime_asc: ['m', 'a', 'b', 'z'],
    runtime_desc: ['z', 'a', 'b', 'm'],
    showtimes_desc: ['a', 'b', 'z', 'm'],
  }
  for (const { value: sort } of movieCatalogSortOptions) {
    for (const search of ['', ' \t ', 'match']) {
      const result = filterAndSortCatalogMovies(movies, search, sort)
      assert.notEqual(result, movies)
      assert.deepEqual(
        result.map(({ slug }) => slug),
        expected[sort],
      )
      assert.ok(result.every((candidate) => movies.includes(candidate)))
      assert.deepEqual(movies, before)
    }
  }
})

test('search normalization leaves displayed titles and subtitle comparisons unchanged', () => {
  const candidate = Object.freeze(
    movie({ title: 'Spider-Man', original_title: '  Spider Man  ' }),
  )
  assert.deepEqual(
    filterAndSortCatalogMovies([candidate], 'man spider', 'title_asc'),
    [candidate],
  )
  assert.equal(candidate.title, 'Spider-Man')
  assert.equal(candidate.original_title, '  Spider Man  ')
  assert.equal(movieOriginalTitleSubtitle(candidate), 'Spider Man')
})

test('matching either title keeps each movie once and sorting uses French titles', () => {
  const movies = [
    movie({ slug: 'z', title: 'Zèbre', original_title: 'Alpha match' }),
    movie({ slug: 'b', title: 'Alpha match', original_title: 'Zulu match' }),
    movie({ slug: 'a', title: 'Alpha match', original_title: 'Zulu match' }),
  ]
  for (const search of ['match', '  ']) {
    assert.deepEqual(
      filterAndSortCatalogMovies(movies, search, 'title_asc').map(
        ({ slug }) => slug,
      ),
      ['a', 'b', 'z'],
    )
    assert.deepEqual(
      filterAndSortCatalogMovies(movies, search, 'title_desc').map(
        ({ slug }) => slug,
      ),
      ['z', 'b', 'a'],
    )
  }
  assert.deepEqual(
    movies.map(({ slug }) => slug),
    ['z', 'b', 'a'],
  )
})

test('shared components preserve card, controls, and pagination contracts', () => {
  assert.match(card, /movie\.showtime_count !== undefined/)
  assert.match(card, /formatRuntime\(movie\.runtime_minutes\)/)
  assert.match(controls, />Rechercher un film</)
  assert.match(controls, />Trier par</)
  assert.match(controls, /emit\('search', searchInput\.value\.trim\(\)\)/)
  assert.match(pagination, /<NuxtLink\s+v-else\s+:to="previousTo"/)
  assert.match(pagination, /<NuxtLink\s+v-else\s+:to="nextTo"/)
  assert.match(pagination, /aria-live="polite"/)
})

test('cards reserve a runtime line even when duration is unknown', () => {
  assert.match(
    card,
    /<div\s+class="min-h-5 [^"]*leading-5[^"]*"\s*>\s*<template v-if="movie\.runtime_minutes > 0">/,
  )
  assert.match(card, /<\/template>\s*<\/div>\s*<slot name="release"/)
})

test('film runtime and release-date chips share the same sizing', () => {
  const runtimeClass = film.match(
    /<span\s+v-if="schedule\.movie\.runtime_minutes > 0"\s+class="([^"]+)"/,
  )?.[1]
  const releaseClass = film.match(
    /<time\s+v-if="isUpcomingFilm && frenchReleaseLabel"[^>]*class="([^"]+)"/,
  )?.[1]
  assert.ok(runtimeClass)
  assert.equal(releaseClass, runtimeClass)
})

test('films and city render shared catalog primitives', () => {
  for (const source of [films, city]) {
    assert.match(source, /<MovieCatalogControls/)
    assert.match(source, /<MovieCatalogCard/)
    assert.match(source, /<MovieCatalogPagination/)
  }
  assert.match(
    city,
    /theaters: currentDetail\.theaters\.map\(\(theater\) => theater\.id\)\.join\(','\)/,
  )
  assert.match(city, /currently_screened: true/)
  assert.match(city, /page_size: PAGE_SIZE/)
  assert.match(city, /v-else-if="catalogErrorMessage"/)
  assert.match(
    city,
    /page\.value > lastPage[\s\S]*router\.replace\(\{ query \}\)/,
  )
})

test('server-backed catalogs preserve raw trimmed route queries and server pagination', () => {
  for (const source of [films, city]) {
    assert.match(source, /search: appliedSearch\.value \|\| undefined/)
    assert.match(
      source,
      /sort: sort\.value,\s*page: page\.value,\s*page_size: PAGE_SIZE/,
    )
    assert.doesNotMatch(
      source,
      /compileMovieTitleSearch|filterAndSortCatalogMovies/,
    )
  }
  assert.match(
    films,
    /const rawSearch = singularQueryValue\(route\.query\.q\)\s*const nextSearch = rawSearch\?\.trim\(\) \?\? ''/,
  )
  assert.match(films, /appliedSearch\.value = nextSearch/)
  assert.match(films, /q: state\.search \|\| undefined/)
  assert.match(films, /theaters: theaterIds/)
  assert.match(films, /Aucun film ne correspond à cette recherche/)
  assert.match(
    city,
    /const search = singularQueryValue\(route\.query\.q\)\?\.trim\(\) \?\? ''/,
  )
  assert.match(city, /appliedSearch\.value = search/)
  assert.match(city, /q: search \|\| undefined/)
  assert.match(city, /Aucun film ne correspond/)
  assert.match(
    publicApi,
    /movies\(query: MoviesQuery = \{\}, signal\?: AbortSignal\) \{\s*return apiFetch<MoviesResponse>\(`\$\{apiBase\}\/api\/v1\/movies`, \{\s*query: queryValues\(query\)/,
  )
})

test('watchlist catalog submits raw trimmed text with existing validation and owner scope', () => {
  assert.match(watchlistPage, /v-model="query"/)
  assert.match(
    watchlistPage,
    /activeTab\.value = 'catalog'\s*void watchlist\.search\(\)/,
  )
  assert.match(watchlistPage, /v-for="movie in searchResults\.catalog"/)
  assert.match(watchlistPage, /v-if="searchResults\.catalog_has_more"/)
  assert.match(
    watchlistPage,
    /Aucun film du catalogue\. Essayez un autre titre/,
  )
  assert.match(watchlist, /const text = query\.value\.trim\(\)/)
  assert.match(
    watchlist,
    /\[\.\.\.text\]\.length < 2 \|\| \[\.\.\.text\]\.length > 200/,
  )
  assert.match(
    watchlist,
    /await api\.searchWatchlist\(\s*\{ expected_username: token\.expectedOwner, query: text \},\s*searchController\.signal/,
  )
  assert.match(
    watchlist,
    /if \(!token\.valid\(\) \|\| current !== searchGeneration\) return/,
  )
  assert.match(
    watchlist,
    /checkOwner\(value, token\.expectedOwner\)\s*searchResults\.value = value/,
  )
  assert.match(
    accountApi,
    /request<WatchlistSearch>\(\s*'\/account\/watchlist\/search',\s*\{ \.\.\.input \},\s*'POST',\s*signal/,
  )
  for (const source of [watchlistPage, watchlist, accountApi]) {
    assert.doesNotMatch(
      source,
      /compileMovieTitleSearch|filterAndSortCatalogMovies/,
    )
  }
})

test('cinema Films keeps aggregation and derives compact filtered shared cards', () => {
  const fetchMovies = cinema.match(
    /async function fetchMovies\(theaterId: string\)[\s\S]*?(?=async function fetchMoviesState)/,
  )?.[0]
  assert.ok(fetchMovies)
  assert.match(fetchMovies, /currently_screened: true,\s*theaters: theaterId/)
  assert.match(fetchMovies, /page_size: CATALOG_PAGE_SIZE/)
  assert.match(fetchMovies, /api\.movies\(\{ \.\.\.query, page: 1 \}\)/)
  assert.match(
    fetchMovies,
    /Math\.ceil\(firstPage\.total \/ CATALOG_PAGE_SIZE\)/,
  )
  assert.match(fetchMovies, /page: index \+ 2/)
  assert.match(
    fetchMovies,
    /return \[firstPage, \.\.\.remainingPages\]\.flatMap\(\(page\) => page\.items\)/,
  )
  assert.doesNotMatch(
    fetchMovies,
    /search:|filmSearch|compileMovieTitleSearch|filterAndSortCatalogMovies/,
  )
  assert.match(
    cinema,
    /singularQueryValue\(route\.query\.q\)\?\.trim\(\) \?\? ''/,
  )
  assert.match(cinema, /q: search \|\| undefined/)
  assert.match(cinema, /const remainingPages = await Promise\.all/)
  assert.match(
    cinema,
    /filterAndSortCatalogMovies\(\s*cinemaMovies\.value,\s*filmSearch\.value,\s*filmSort\.value,?\s*\)/,
  )
  assert.match(cinema, /<MovieCatalogControls[^>]+compact/)
  assert.match(cinema, /v-for="movie in displayedCinemaMovies"/)
  assert.match(
    cinema,
    /<MovieCatalogCard\s+:movie="movie"\s+:to="cinemaMovieTarget\(movie\.slug, response\.theater\.id\)"/,
  )
  assert.match(cinema, /Aucun film à l’affiche/)
  assert.match(cinema, /Aucun film ne correspond à la recherche/)
  assert.match(cinema, /FILMS_QUERY_KEYS = \['view', 'q', 'sort'\]/)
})
