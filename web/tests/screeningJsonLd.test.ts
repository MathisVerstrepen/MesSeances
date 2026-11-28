import assert from 'node:assert/strict'
import test from 'node:test'
import type { Showtime } from '../app/types/api.ts'
import {
  screeningEventDetails,
  theaterPostalAddress,
} from '../app/utils/screeningJsonLd.ts'

function showtime(overrides: Partial<Showtime> = {}): Showtime {
  return {
    provider: 'cgr',
    id: 'cgr-showing-1',
    movie: {
      slug: 'film-7',
      title: '  Le Film  ',
      original_language: 'en',
      runtime_minutes: 100,
      updated_at: '2026-10-01T00:00:00Z',
    },
    start_time: '2026-10-02T18:00:00Z',
    end_time: '2026-10-02T19:40:00Z',
    estimated_end_time: null,
    estimated_end_ads_minutes: null,
    language: 'VOSTFR',
    format: 'IMAX',
    room: 'Salle 1',
    booking_url:
      'https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456',
    ...overrides,
  }
}

const context = {
  theaterId: 'cgr-W8010',
  theaterName: ' CGR Abbeville ',
  originalLanguage: 'en',
  image: 'https://image.tmdb.org/t/p/w500/poster.jpg',
}

test('screening details describe a scheduled in-person session with its booking offer', () => {
  assert.deepEqual(screeningEventDetails(showtime(), context), {
    name: 'Le Film à CGR Abbeville',
    eventStatus: 'https://schema.org/EventScheduled',
    eventAttendanceMode: 'https://schema.org/OfflineEventAttendanceMode',
    inLanguage: 'en',
    subtitleLanguage: 'fr',
    videoFormat: 'IMAX',
    offers: {
      '@type': 'Offer',
      url: 'https://achat.cgrcinemas.fr/cgr-abbeville-la-sucrerie/r/123456',
    },
    image: 'https://image.tmdb.org/t/p/w500/poster.jpg',
  })
})

test('screening languages separate audio from French subtitles', () => {
  const cases: Array<
    [Showtime['language'], string | null, Record<string, string>]
  > = [
    ['VF', 'en', { inLanguage: 'fr' }],
    ['VF', null, { inLanguage: 'fr' }],
    ['VF_SME', 'en', { inLanguage: 'fr', subtitleLanguage: 'fr' }],
    ['VFSTF', 'ja', { inLanguage: 'fr', subtitleLanguage: 'fr' }],
    ['VOSTFR', 'KO', { inLanguage: 'ko', subtitleLanguage: 'fr' }],
    ['VOSTFR', null, { subtitleLanguage: 'fr' }],
    ['VOSTFR', 'xx-invalid', { subtitleLanguage: 'fr' }],
    ['VO', 'it', { inLanguage: 'it' }],
    ['VO', '', {}],
    ['', 'en', {}],
  ]
  for (const [language, originalLanguage, expected] of cases) {
    const details = screeningEventDetails(showtime({ language }), {
      ...context,
      originalLanguage,
    })
    const languages: Record<string, string> = {}
    if (details.inLanguage) languages.inLanguage = details.inLanguage
    if (details.subtitleLanguage)
      languages.subtitleLanguage = details.subtitleLanguage
    assert.deepEqual(languages, expected, `${language} / ${originalLanguage}`)
  }
})

test('offers only verified booking links, never generic websites or unsafe URLs', () => {
  const website = screeningEventDetails(
    showtime({
      provider: 'megarama',
      booking_url: 'https://bordeaux.megarama.fr/',
    }),
    context,
  )
  assert.equal(Object.hasOwn(website, 'offers'), false)
  for (const booking_url of [
    null,
    'javascript:alert(1)',
    'https://evil.example/r/123456',
  ])
    assert.equal(
      Object.hasOwn(
        screeningEventDetails(showtime({ booking_url }), context),
        'offers',
      ),
      false,
      String(booking_url),
    )
  const megarama = screeningEventDetails(
    showtime({
      provider: 'megarama',
      booking_url:
        'https://bordeaux.megarama.fr/#showsession?id=emsx056500123456',
    }),
    context,
  )
  assert.deepEqual(megarama.offers, {
    '@type': 'Offer',
    url: 'https://bordeaux.megarama.fr/#showsession?id=emsx056500123456',
  })
})

test('screening details omit missing images and use readable format labels', () => {
  const details = screeningEventDetails(showtime({ format: 'LASER_ULTRA' }), {
    ...context,
    image: null,
  })
  assert.equal(Object.hasOwn(details, 'image'), false)
  assert.equal(details.videoFormat, 'Laser ULTRA by Kinepolis')
})

test('postal addresses require a city and keep only known parts', () => {
  assert.deepEqual(
    theaterPostalAddress({
      address: ' Place du Pin, 9 Rue de la Prune ',
      postal_code: '47000',
      city: 'Agen',
    }),
    {
      '@type': 'PostalAddress',
      streetAddress: 'Place du Pin, 9 Rue de la Prune',
      postalCode: '47000',
      addressLocality: 'Agen',
      addressCountry: 'FR',
    },
  )
  assert.deepEqual(theaterPostalAddress({ city: 'Paris' }), {
    '@type': 'PostalAddress',
    addressLocality: 'Paris',
    addressCountry: 'FR',
  })
  assert.equal(
    theaterPostalAddress({
      address: '1 rue X',
      postal_code: '75001',
      city: ' ',
    }),
    undefined,
  )
})
