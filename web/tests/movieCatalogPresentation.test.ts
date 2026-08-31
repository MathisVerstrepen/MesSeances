import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import type { CatalogMovie } from '../app/types/api.ts'
import { filterAndSortCatalogMovies, movieCatalogSortOptions } from '../app/utils/movieCatalogPresentation.ts'

const [card, controls, pagination, films, city, cinema] = await Promise.all([
  readFile(new URL('../app/components/MovieCatalogCard.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/components/MovieCatalogControls.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/components/MovieCatalogPagination.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/films.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/ville/[slug]/cinemas.vue', import.meta.url), 'utf8'),
  readFile(new URL('../app/pages/cinema/[slug].vue', import.meta.url), 'utf8')
])

function movie(overrides: Partial<CatalogMovie>): CatalogMovie {
  return {
    slug: 'film', title: 'Film', runtime_minutes: 90, updated_at: '', poster_url: null,
    tmdb_id: null, imdb_id: null, overview: null, release_date: null, genres: [], ...overrides
  }
}

test('shares canonical catalog sort ordering', () => {
  assert.deepEqual(movieCatalogSortOptions.map(({ value }) => value), [
    'title_asc', 'title_desc', 'release_date_desc', 'runtime_asc', 'runtime_desc', 'showtimes_desc'
  ])
})

test('filters titles without case or diacritics and applies deterministic sort tie-breakers', () => {
  const movies = [
    movie({ slug: 'z', title: 'Été', runtime_minutes: 100, showtime_count: 2 }),
    movie({ slug: 'b', title: 'Alpha', runtime_minutes: 100, showtime_count: 3 }),
    movie({ slug: 'a', title: 'Alpha', runtime_minutes: 100, showtime_count: 3 })
  ]
  assert.deepEqual(filterAndSortCatalogMovies(movies, ' ETE ', 'title_asc').map(({ slug }) => slug), ['z'])
  assert.deepEqual(filterAndSortCatalogMovies(movies, '', 'showtimes_desc').map(({ slug }) => slug), ['a', 'b', 'z'])
})

test('shared components preserve card, controls, and pagination contracts', () => {
  assert.match(card, /movie\.showtime_count !== undefined/)
  assert.match(card, /formatRuntime\(movie\.runtime_minutes\)/)
  assert.match(controls, />Rechercher un film</)
  assert.match(controls, />Trier par</)
  assert.match(controls, /emit\('search', searchInput\.value\.trim\(\)\)/)
  assert.match(pagination, /<NuxtLink v-else :to="previousTo"/)
  assert.match(pagination, /<NuxtLink v-else :to="nextTo"/)
  assert.match(pagination, /aria-live="polite"/)
})

test('films and city render shared catalog primitives', () => {
  for (const source of [films, city]) {
    assert.match(source, /<MovieCatalogControls/)
    assert.match(source, /<MovieCatalogCard/)
    assert.match(source, /<MovieCatalogPagination/)
  }
  assert.match(city, /theaters: currentDetail\.theaters\.map\(\(theater\) => theater\.id\)\.join\(','\)/)
  assert.match(city, /currently_screened: true/)
  assert.match(city, /page_size: PAGE_SIZE/)
  assert.match(city, /v-else-if="catalogErrorMessage"/)
  assert.match(city, /page\.value > lastPage[\s\S]*router\.replace\(\{ query \}\)/)
})

test('cinema Films keeps aggregation and derives compact filtered shared cards', () => {
  assert.match(cinema, /const remainingPages = await Promise\.all/)
  assert.match(cinema, /filterAndSortCatalogMovies\(cinemaMovies\.value, filmSearch\.value, filmSort\.value\)/)
  assert.match(cinema, /<MovieCatalogControls[^>]+compact/)
  assert.match(cinema, /v-for="movie in displayedCinemaMovies"/)
  assert.match(cinema, /<MovieCatalogCard :movie="movie" :to="cinemaMovieTarget\(movie\.slug, response\.theater\.id\)"/)
  assert.match(cinema, /Aucun film à l’affiche/)
  assert.match(cinema, /Aucun film ne correspond à la recherche/)
  assert.match(cinema, /FILMS_QUERY_KEYS = \['view', 'q', 'sort'\]/)
})
