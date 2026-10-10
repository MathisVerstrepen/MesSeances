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
  movie,
  date,
  statisticsRelease,
  theater,
} from '../e2e/data.mjs'

const source = (path: string) =>
  readFileSync(new URL(`../app/pages/${path}`, import.meta.url), 'utf8')
const filmDiscovery = readFileSync(
  new URL('../app/components/FilmCityDiscovery.vue', import.meta.url),
  'utf8',
)

test('pages consume bounded server discoveries without client ranking, dedupe, slicing or date construction', () => {
  const film = source('film/[slug].vue')
  const cinema = source('cinema/[slug].vue')
  const city = source('ville/[slug]/cinemas.vue')
  assert.match(film, /:discovery="schedule\.discovery"/u)
  assert.match(filmDiscovery, /v-for="city in discovery\.cities"/u)
  assert.match(cinema, /v-for="movie in response\.discovery\.movies"/u)
  assert.match(cinema, /v-for="other in response\.discovery\.other_theaters"/u)
  assert.match(city, /detail\.value\?\.discovery\.theaters\.map/u)
  for (const page of [filmDiscovery, cinema, city]) {
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

test('cinema discovery keeps one SSR card list with native narrow-screen snapping and desktop grid', () => {
  const cinema = source('cinema/[slug].vue')
  const teaser = cinema.slice(
    cinema.indexOf('aria-labelledby="cinema-discovery-films-heading"'),
    cinema.indexOf('<ShowtimeDateBar'),
  )
  assert.equal(teaser.match(/<ul\b/gu)?.length, 1)
  assert.equal(teaser.match(/<MovieCatalogCard\b/gu)?.length, 1)
  assert.match(teaser, /flex snap-x snap-mandatory scroll-p-2/u)
  assert.match(teaser, /overflow-x-auto/u)
  assert.match(teaser, /focus-within:snap-none/u)
  assert.match(teaser, /@focusin=/u)
  assert.match(
    teaser,
    /scrollIntoView\(\{\s*block: 'nearest',\s*inline: 'nearest'/u,
  )
  assert.match(
    teaser,
    /lg:grid lg:snap-none lg:grid-cols-6 lg:overflow-visible/u,
  )
  assert.match(teaser, /shrink-0 snap-start sm:w-48 lg:w-auto/u)
  assert.doesNotMatch(teaser, /grid-cols-[23]|ClientOnly|tabindex="-1"/u)
})

test('optional sections guard both window and nonempty arrays; same-entity errors retain data and route changes clear it', () => {
  assert.match(
    filmDiscovery,
    /v-if="discovery\.window && discovery\.cities\.length"/u,
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

test('film discovery reuses one native disclosure list below scoped sessions and expanded above broad sessions', () => {
  const film = source('film/[slug].vue')
  const scheduleStart = film.indexOf('class="schedule-section')
  const broadDiscovery = film.indexOf('<FilmCityDiscovery')
  const scopedDiscovery = film.lastIndexOf('<FilmCityDiscovery')
  assert(broadDiscovery < scheduleStart && scopedDiscovery > scheduleStart)
  assert.match(film.slice(broadDiscovery, scheduleStart), /v-if="broadScope"/u)
  assert.match(film.slice(scopedDiscovery), /v-if="!broadScope"/u)
  for (const key of [
    'slug',
    'selectionScopeKey',
    'hasSharedSelection',
    'activeTheaterIds',
  ])
    assert(film.slice(scopedDiscovery).includes(key))
  assert.match(filmDiscovery, /collapsible \? 'details' : 'div'/u)
  assert.match(filmDiscovery, /collapsible \? 'summary' : 'div'/u)
  assert.equal(
    filmDiscovery.match(/v-for="city in discovery\.cities"/gu)?.length,
    1,
  )
  assert.doesNotMatch(
    filmDiscovery,
    /\bopen=|\b(?:fetch|useMesSeancesApi|watch|onMounted)\b/u,
  )
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
  const scopedEmpty = movieSchedule(
    'film-playwright',
    new URLSearchParams('theaters=fixture-second'),
    'no-local-programme',
  )
  assert.deepEqual(scopedEmpty.available_dates, [])
  assert.deepEqual(scopedEmpty.theaters, [])
  assert.deepEqual(scopedEmpty.discovery.cities, discoveryCities)
  assert.equal(
    movieSchedule(
      'film-playwright',
      new URLSearchParams(),
      'no-local-programme',
    ).theaters.length,
    2,
  )
})

test('statistics film metadata composes with the complete discovery schedule and canonical identity', () => {
  for (const slug of [movie.slug, 'merged-film']) {
    const schedule = movieSchedule(
      slug,
      new URLSearchParams(`theaters=${theater.id}&date=${date}&page=2`),
    )
    assert.equal(schedule.movie.slug, movie.slug)
    assert.equal(schedule.movie.french_release_date, statisticsRelease)
    assert.equal(schedule.movie.release_date, '2001-01-01')
    assert.equal(schedule.release_status, 'showing')
    assert.equal(schedule.currently_screened, true)
    assert.deepEqual(schedule.available_dates, [date])
    assert.deepEqual(
      schedule.theaters.map((venue) => venue.id),
      [theater.id],
    )
    assert.equal(schedule.theaters[0].showtimes[0].movie, schedule.movie)
    assert.equal(schedule.pagination.page, 2)
    assert.deepEqual(schedule.discovery.cities, discoveryCities)
  }
  const noRelease = movieSchedule('film-no-release', new URLSearchParams())
  assert.equal(noRelease.movie.slug, 'film-no-release')
  assert.equal(noRelease.movie.french_release_date, null)
  assert.equal(noRelease.movie.release_date, '2001-01-01')
  assert.equal(noRelease.currently_screened, false)
  assert.deepEqual(noRelease.theaters, [])
  assert.deepEqual(noRelease.discovery.cities, [])
  assert.equal(movieSchedule('missing', new URLSearchParams()), null)
})
