import assert from 'node:assert/strict'
import test from 'node:test'
import type { MovieShowtimesResponse, Showtime } from '../app/types/api.ts'
import { buildFilmJsonLd } from '../app/utils/filmJsonLd.ts'
import { serializeJsonLd } from '../app/utils/jsonLd.ts'

const movie = {
  slug: 'film-42',
  title: 'Film </script> &\u2028 séance',
  runtime_minutes: 118,
  updated_at: '2026-08-29T00:00:00Z',
  poster_url: 'https://image.tmdb.org/t/p/w500/poster.jpg',
  tmdb_id: 42,
  imdb_id: 'tt0000042',
  overview: '  Une histoire de cinéma.  ',
  release_date: '2026-08-20',
  genres: ['Drame']
}

function showtime(id: string, start: string): Showtime {
  return {
    provider: 'ugc',
    id,
    movie,
    start_time: start,
    end_time: start.replace(':00:00+02:00', ':58:00+02:00'),
    language: 'VOSTFR',
    format: '2D',
    room: '1',
    booking_url: null
  }
}

function fixture(): MovieShowtimesResponse {
  return {
    movie,
    backdrop_url: 'https://image.tmdb.org/t/p/w780/backdrop.jpg',
    date: '2026-08-29',
    currently_screened: true,
    available_dates: ['2026-08-29'],
    theaters: [
      {
        provider: 'ugc',
        id: 'ugc-1',
        slug: 'ugc-les-halles',
        name: 'UGC Les Halles',
        city: 'Paris',
        city_slug: 'paris',
        showtimes: [
          showtime('shared-id', '2026-08-29T18:00:00+02:00'),
          showtime('ugc-2', '2026-08-29T20:00:00+02:00')
        ]
      },
      {
        provider: 'pathe',
        id: 'pathe-1',
        slug: 'pathe-wepler',
        name: 'Pathé Wepler',
        city: 'Paris',
        city_slug: 'paris',
        showtimes: [showtime('shared-id', '2026-08-29T19:00:00+02:00')]
      },
      {
        provider: 'cgr',
        id: 'cgr-empty',
        slug: 'cgr-empty',
        name: 'CGR vide',
        city: 'Paris',
        city_slug: 'paris',
        showtimes: []
      }
    ]
  }
}

test('builds compact shared-reference graph and retains every showtime without mutation or ID deduplication', () => {
  const schedule = fixture()
  const before = structuredClone(schedule)
  const movieUrl = 'https://messeances.fr/film/film-42'
  const document = buildFilmJsonLd(schedule, {
    movieUrl,
    siteUrl: 'https://messeances.fr',
    datePublished: '2026-08-20',
    tmdbUrl: 'https://www.themoviedb.org/movie/42'
  })

  assert.deepEqual(schedule, before)
  assert.equal(document['@context'], 'https://schema.org')

  const movieNode = document['@graph'].find((node) => node['@type'] === 'Movie')
  assert.deepEqual(movieNode, {
    '@type': 'Movie',
    '@id': `${movieUrl}#movie`,
    name: movie.title,
    url: movieUrl,
    duration: 'PT118M',
    description: 'Une histoire de cinéma.',
    datePublished: '2026-08-20',
    genre: ['Drame'],
    image: [
      'https://image.tmdb.org/t/p/w500/poster.jpg',
      'https://image.tmdb.org/t/p/w780/backdrop.jpg'
    ],
    sameAs: 'https://www.themoviedb.org/movie/42'
  })

  const theaters = document['@graph'].filter((node) => node['@type'] === 'MovieTheater')
  assert.deepEqual(theaters, [
    { '@type': 'MovieTheater', '@id': 'https://messeances.fr/cinema/ugc-les-halles#cinema', name: 'UGC Les Halles', url: 'https://messeances.fr/cinema/ugc-les-halles' },
    { '@type': 'MovieTheater', '@id': 'https://messeances.fr/cinema/pathe-wepler#cinema', name: 'Pathé Wepler', url: 'https://messeances.fr/cinema/pathe-wepler' }
  ])
  assert.equal(new Set(theaters.map((theater) => theater['@id'])).size, theaters.length)

  const events = document['@graph'].filter((node) => node['@type'] === 'ScreeningEvent')
  assert.equal(events.length, 3)
  for (const event of events) {
    assert.deepEqual(Object.keys(event).sort(), ['@type', 'endDate', 'location', 'startDate', 'workPresented'].sort())
    assert.deepEqual(event.workPresented, { '@id': `${movieUrl}#movie` })
  }
  assert.deepEqual(events.map((event) => event.location?.['@id']), [
    'https://messeances.fr/cinema/ugc-les-halles#cinema',
    'https://messeances.fr/cinema/ugc-les-halles#cinema',
    'https://messeances.fr/cinema/pathe-wepler#cinema'
  ])
  assert.deepEqual(events.map((event) => event.startDate), [
    '2026-08-29T18:00:00+02:00',
    '2026-08-29T20:00:00+02:00',
    '2026-08-29T19:00:00+02:00'
  ])
})

test('keeps breadcrumb identities and serializes script-breaking characters safely', () => {
  const document = buildFilmJsonLd(fixture(), {
    movieUrl: 'https://messeances.fr/film/film-42',
    siteUrl: 'https://messeances.fr/base-path'
  })
  const breadcrumb = document['@graph'].find((node) => node['@type'] === 'BreadcrumbList')

  assert.deepEqual(breadcrumb, {
    '@type': 'BreadcrumbList',
    '@id': 'https://messeances.fr/film/film-42#breadcrumb',
    itemListElement: [
      { '@type': 'ListItem', position: 1, name: 'Accueil', item: 'https://messeances.fr/' },
      { '@type': 'ListItem', position: 2, name: 'Films', item: 'https://messeances.fr/films' },
      { '@type': 'ListItem', position: 3, name: movie.title, item: 'https://messeances.fr/film/film-42' }
    ]
  })

  const serialized = serializeJsonLd(document)
  assert.doesNotMatch(serialized, /[<>&\u2028\u2029]/u)
  assert.match(serialized, /\\u003C\/script\\u003E/u)
  assert.match(serialized, /\\u0026/u)
  assert.match(serialized, /\\u2028/u)
})
