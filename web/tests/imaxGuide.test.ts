import assert from 'node:assert/strict'
import test from 'node:test'
import type { Theater } from '../app/types/api'
import {
  guideCinemaTarget,
  imaxExtraImage,
  imaxLandmarks,
  imaxRatios,
  imaxGuideMovies,
  guideMovieTarget,
} from '../app/utils/imaxGuide.ts'

test('guide film links match the exact film identity and use the current public slug', () => {
  const odyssey = imaxGuideMovies.find(
    (movie) => movie.id === 'odyssee-voilier',
  )!
  assert.equal(
    guideMovieTarget(
      [
        { slug: 'another-odyssey', tmdb_id: 62 },
        { slug: 'public-film', tmdb_id: odyssey.tmdbId },
      ],
      odyssey,
    ),
    '/film/public-film',
  )
  assert.equal(
    guideMovieTarget([{ slug: 'another-odyssey', tmdb_id: 62 }], odyssey),
    '/films?search=Odyss%C3%A9e',
  )
  assert.equal(
    guideMovieTarget(
      [{ slug: 'film/écran', tmdb_id: odyssey.tmdbId }],
      odyssey,
    ),
    '/film/film%2F%C3%A9cran',
  )
})

test('ratio comparison calculates image area at equal width, without confusing it with resolution', () => {
  assert.deepEqual(
    imaxRatios.map((format) => imaxExtraImage(format.ratio)),
    [0, 26, 67],
  )
  assert.equal(imaxExtraImage(2.39), 0)
})

test('guide cinema links use the registered public slug, even when no IMAX screening is currently bookable', () => {
  const theater: Theater = {
    provider: 'pathe',
    id: imaxLandmarks.odysseum.id,
    slug: 'a-public-slug',
    name: 'Pathé Odysseum',
    city: 'Montpellier',
    city_slug: 'montpellier',
    address: '',
    postal_code: '34000',
    available_dates: [],
    available_formats: ['2D'],
    technologies: ['IMAX'],
    accepted_passes: [],
  }
  assert.equal(
    guideCinemaTarget([theater], imaxLandmarks.odysseum),
    '/cinema/a-public-slug',
  )
  assert.equal(
    guideCinemaTarget(
      [{ ...theater, slug: 'cinéma/écran' }],
      imaxLandmarks.odysseum,
    ),
    '/cinema/cin%C3%A9ma%2F%C3%A9cran',
  )
  assert.equal(
    guideCinemaTarget(
      [{ ...theater, id: 'different-id' }],
      imaxLandmarks.odysseum,
    ),
    '/cinemas?technologies=IMAX&q=Odysseum',
  )
  const cineum = {
    ...theater,
    id: 'cineum-id',
    slug: 'cineum-public',
    name: 'Cineum',
    city: 'Cannes',
  }
  assert.equal(
    guideCinemaTarget([cineum], imaxLandmarks.cannes),
    '/cinema/cineum-public',
  )
  assert.equal(
    guideCinemaTarget(
      [cineum, { ...cineum, id: 'duplicate-id' }],
      imaxLandmarks.cannes,
    ),
    '/cinemas?technologies=IMAX&q=Cannes',
  )
  assert.equal(
    guideCinemaTarget(
      [{ ...cineum, name: 'Pathé La Geode', slug: 'geode-public' }],
      imaxLandmarks.geode,
    ),
    '/cinema/geode-public',
  )
})

test('unknown or unavailable cinema records fall back to a named search retaining the IMAX filter', () => {
  assert.equal(
    guideCinemaTarget([], imaxLandmarks.geode),
    '/cinemas?technologies=IMAX&q=G%C3%A9ode',
  )
  assert.equal(
    guideCinemaTarget([], imaxLandmarks.cannes),
    '/cinemas?technologies=IMAX&q=Cannes',
  )
})
