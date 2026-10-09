import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { cinemaMovieTarget } from '../app/utils/cinemaMovieTarget.ts'
import {
  cityDetail,
  cinemaDiscovery,
  discoveryCities,
  discoveryMovies,
  movieSchedule,
} from '../e2e/data.mjs'

const source = (path: string) =>
  readFileSync(new URL(`../app/pages/${path}`, import.meta.url), 'utf8')

test('pages consume bounded server discoveries without client ranking, dedupe, slicing or date construction', () => {
  const film = source('film/[slug].vue')
  const cinema = source('cinema/[slug].vue')
  const city = source('ville/[slug]/cinemas.vue')
  assert.match(film, /v-for="city in schedule\.discovery\.cities"/u)
  assert.match(cinema, /v-for="movie in response\.discovery\.movies"/u)
  assert.match(cinema, /v-for="other in response\.discovery\.other_theaters"/u)
  assert.match(city, /detail\.value\?\.discovery\.theaters\.map/u)
  for (const page of [film, cinema, city]) {
    assert.doesNotMatch(
      page,
      /discovery\.(?:movies|cities|theaters|other_theaters)\.(?:sort|slice|filter)/u,
    )
    assert.match(page, /formatLongDate\([^)]*discovery\.window\.from\)/u)
    assert.match(page, /formatLongDate\([^)]*discovery\.window\.through\)/u)
  }
})

test('runtime-unknown teaser counts use release slot and scoped canonical film targets', () => {
  const cinema = source('cinema/[slug].vue')
  assert.match(
    cinema,
    /<template v-if="movie\.runtime_minutes <= 0" #release>/u,
  )
  assert.match(cinema, /formatShowtimeCount\(movie\.showtime_count!\)/u)
  assert.match(
    cinema,
    /:to="cinemaMovieTarget\(movie\.slug, response\.theater\.id\)"/u,
  )
  assert.equal(
    cinemaMovieTarget('film & inconnu', 'venue-test'),
    '/film/film%20%26%20inconnu?shared_theaters=venue-test',
  )
})

test('optional sections guard both window and nonempty arrays; same-entity errors retain data and route changes clear it', () => {
  assert.match(
    source('film/[slug].vue'),
    /v-if="schedule\.discovery\.window && schedule\.discovery\.cities\.length"/u,
  )
  const cinema = source('cinema/[slug].vue')
  assert.match(
    cinema,
    /response\.discovery\.window && response\.discovery\.movies\.length/u,
  )
  assert.match(
    cinema,
    /response\.discovery\.window && response\.discovery\.other_theaters\.length/u,
  )
  assert.match(
    cinema,
    /state\.kind !== 'upstream-error'\) response\.value = state\.response/u,
  )
  assert.match(
    cinema,
    /nextSlug !== previousSlug\) \{\s*response\.value = null/u,
  )
  const city = source('ville/[slug]/cinemas.vue')
  assert.match(
    city,
    /state\.kind !== 'upstream-error'\) detail\.value = state\.detail/u,
  )
  assert.match(city, /watch\(slug, \(\) => \{\s*detail\.value = null/u)
})

test('synthetic acceptance has bounded canonical tied/sorted data, zero counts and empty/ended/upcoming cases', () => {
  assert.equal(discoveryCities.length, 6)
  assert.equal(discoveryMovies.length, 6)
  assert.equal(new Set(discoveryMovies.map((movie) => movie.slug)).size, 6)
  assert.equal(new Set(discoveryCities.map((city) => city.slug)).size, 6)
  assert.deepEqual(
    discoveryMovies.slice(2).map((movie) => movie.title),
    ['Alpha', 'Écho', 'Écho bis', 'Zèbre'],
  )
  for (const mode of ['empty', 'null-window']) {
    assert.deepEqual(cinemaDiscovery(mode).movies, [])
    assert.deepEqual(
      movieSchedule('film-playwright', new URLSearchParams(), mode).discovery
        .cities,
      [],
    )
  }
  for (const slug of ['film-ended', 'film-upcoming'])
    assert.deepEqual(
      movieSchedule(slug, new URLSearchParams()).discovery.cities,
      [],
    )
  assert.deepEqual(cinemaDiscovery('single-cinema').other_theaters, [])
  assert(
    cityDetail('lille').discovery.theaters.some(
      (item) => item.movie_count === 0 && item.showtime_count === 0,
    ),
  )
  assert.equal(
    movieSchedule('merged-film', new URLSearchParams()).movie.slug,
    'film-playwright',
  )
  assert.deepEqual(
    movieSchedule(
      'film-playwright',
      new URLSearchParams('theaters=unrelated&language=VOF&page=8'),
    ).discovery.cities,
    discoveryCities,
  )
})
