import assert from 'node:assert/strict'
import test from 'node:test'
import type { TimelineShowtime } from '../app/types/api.ts'
import {
  CITY_DAY_PAGE_SIZE,
  cityAvailableDates,
  compactCityShowtime,
  orderCityTheaters,
  cityDayPageCount,
  cityDayTheaters,
  groupShowtimesByMovie,
  resolveCityDate,
  titlesByShowtimeCount,
} from '../app/utils/cityDay.ts'

function session(id: string, slug: string, start: string): TimelineShowtime {
  return {
    provider: 'ugc',
    id,
    movie: {
      slug,
      title: slug === 'film-1' ? 'Alpha' : 'Beta',
      original_language: null,
      runtime_minutes: 90,
      updated_at: '2026-01-01T00:00:00Z',
    },
    start_time: start,
    end_time: start,
    estimated_end_time: null,
    estimated_end_ads_minutes: null,
    language: 'VF',
    format: '2D',
    room: '',
    booking_url: null,
    start_offset_minutes: 0,
    duration_minutes: 90,
    poster_url: null,
    backdrop_url: null,
  }
}

test('city dates merge every cinema and open on today or the next screening day', () => {
  const dates = cityAvailableDates(
    [
      { available_dates: ['2026-11-30', '2026-11-28', '2026-11-25'] },
      { available_dates: ['2026-11-28', '2026-11-29'] },
    ],
    '2026-11-28',
  )
  assert.deepEqual(dates, ['2026-11-28', '2026-11-29', '2026-11-30'])
  assert.equal(resolveCityDate(undefined, dates, '2026-11-28'), '2026-11-28')
  assert.equal(
    resolveCityDate(undefined, ['2026-11-30'], '2026-11-28'),
    '2026-11-30',
  )
  assert.equal(
    resolveCityDate(undefined, ['2026-11-20'], '2026-11-28'),
    '2026-11-28',
  )
  assert.equal(resolveCityDate('2026-12-25', dates, '2026-11-28'), '2026-12-25')
})

test('city pages paginate cinemas by a fixed page size', () => {
  const theaters = Array.from({ length: CITY_DAY_PAGE_SIZE + 3 }, (_, i) => i)
  assert.equal(cityDayPageCount(0), 1)
  assert.equal(cityDayPageCount(theaters.length), 2)
  assert.deepEqual(cityDayTheaters(theaters, 2), [
    CITY_DAY_PAGE_SIZE,
    CITY_DAY_PAGE_SIZE + 1,
    CITY_DAY_PAGE_SIZE + 2,
  ])
  assert.equal(cityDayTheaters(theaters, 1).length, CITY_DAY_PAGE_SIZE)
})

test('sessions group by film in first-screening order and titles rank by count', () => {
  const showtimes = [
    session('c', 'film-2', '2026-11-28T18:00:00Z'),
    session('a', 'film-1', '2026-11-28T14:00:00Z'),
    session('b', 'film-2', '2026-11-28T15:00:00Z'),
  ]
  const groups = groupShowtimesByMovie(showtimes)
  assert.deepEqual(
    groups.map((group) => [
      group.movie.slug,
      group.showtimes.map((item) => item.id),
    ]),
    [
      ['film-1', ['a']],
      ['film-2', ['b', 'c']],
    ],
  )
  assert.deepEqual(titlesByShowtimeCount([{ showtimes }]), ['Beta', 'Alpha'])
})

test('city cinemas order by programme size and drop unused fields', () => {
  const theater = (id: string, name: string) => ({
    provider: 'ugc' as const,
    id,
    slug: id,
    name,
    address: '',
    city: 'Paris',
    city_slug: 'paris',
    postal_code: '75001',
    available_dates: [],
    accepted_passes: ['UGC Illimité'],
  })
  const ordered = orderCityTheaters({
    theaters: [
      theater('a', 'Zeta'),
      theater('b', 'Alpha'),
      theater('c', 'Beta'),
    ],
    discovery: {
      window: null,
      theaters: [
        { id: 'c', movie_count: 1, showtime_count: 5 },
        { id: 'a', movie_count: 9, showtime_count: 90 },
      ],
    },
  })
  assert.deepEqual(
    ordered.map((item) => item.id),
    ['a', 'c', 'b'],
  )
  assert.equal(Object.hasOwn(ordered[0]!, 'accepted_passes'), false)
  const compact = compactCityShowtime(
    session('x', 'film-1', '2026-11-28T14:00:00Z'),
  )
  assert.deepEqual(Object.keys(compact.movie).sort(), [
    'original_language',
    'runtime_minutes',
    'slug',
    'title',
  ])
  assert.equal(Object.hasOwn(compact, 'poster_url'), false)
})
