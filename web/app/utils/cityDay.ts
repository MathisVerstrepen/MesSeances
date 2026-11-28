import type {
  CityDetailResponse,
  Movie,
  Theater,
  TimelineShowtime,
} from '../types/api.ts'

export const CITY_DAY_PAGE_SIZE = 6

// Only what the city page renders, to keep its hydration payload small.
export type CityDayShowtime = Pick<
  TimelineShowtime,
  | 'id'
  | 'provider'
  | 'start_time'
  | 'end_time'
  | 'estimated_end_time'
  | 'estimated_end_ads_minutes'
  | 'language'
  | 'format'
  | 'room'
  | 'booking_url'
> & {
  movie: Pick<Movie, 'slug' | 'title' | 'runtime_minutes' | 'original_language'>
}

export type CityDayTheater = Pick<
  Theater,
  | 'provider'
  | 'id'
  | 'slug'
  | 'name'
  | 'address'
  | 'postal_code'
  | 'city'
  | 'available_dates'
>

export interface CityDayMovieGroup {
  movie: CityDayShowtime['movie']
  showtimes: CityDayShowtime[]
}

export function compactCityShowtime(
  showtime: CityDayShowtime,
): CityDayShowtime {
  return {
    id: showtime.id,
    provider: showtime.provider,
    start_time: showtime.start_time,
    end_time: showtime.end_time,
    estimated_end_time: showtime.estimated_end_time,
    estimated_end_ads_minutes: showtime.estimated_end_ads_minutes,
    language: showtime.language,
    format: showtime.format,
    room: showtime.room,
    booking_url: showtime.booking_url,
    movie: {
      slug: showtime.movie.slug,
      title: showtime.movie.title,
      runtime_minutes: showtime.movie.runtime_minutes,
      original_language: showtime.movie.original_language,
    },
  }
}

// Busiest cinemas first, so the first page answers most questions.
export function orderCityTheaters(
  detail: Pick<CityDetailResponse, 'theaters' | 'discovery'>,
): CityDayTheater[] {
  const counts = new Map(
    detail.discovery.theaters.map((item) => [item.id, item.showtime_count]),
  )
  return [...detail.theaters]
    .sort(
      (left, right) =>
        (counts.get(right.id) ?? 0) - (counts.get(left.id) ?? 0) ||
        left.name.localeCompare(right.name, 'fr'),
    )
    .map((theater) => ({
      provider: theater.provider,
      id: theater.id,
      slug: theater.slug,
      name: theater.name,
      address: theater.address,
      postal_code: theater.postal_code,
      city: theater.city,
      available_dates: theater.available_dates,
    }))
}

// Upcoming dates on which at least one cinema of the city screens a film.
export function cityAvailableDates(
  theaters: readonly { available_dates: readonly string[] }[],
  today: string,
): string[] {
  return [...new Set(theaters.flatMap((theater) => theater.available_dates))]
    .filter((date) => date >= today)
    .sort()
}

// Explicit dates are honoured even when empty; otherwise open on today or
// the next screening day, like the film page.
export function resolveCityDate(
  requested: string | undefined,
  availableDates: readonly string[],
  today: string,
): string {
  if (requested) return requested
  if (availableDates.includes(today)) return today
  return availableDates.find((date) => date > today) ?? today
}

export function cityDayPageCount(theaterCount: number): number {
  return Math.max(1, Math.ceil(theaterCount / CITY_DAY_PAGE_SIZE))
}

export function cityDayTheaters<Theater>(
  theaters: readonly Theater[],
  page: number,
): Theater[] {
  const start = (page - 1) * CITY_DAY_PAGE_SIZE
  return theaters.slice(start, start + CITY_DAY_PAGE_SIZE)
}

// Films in order of their first screening, each with its sorted sessions.
export function groupShowtimesByMovie(
  showtimes: readonly CityDayShowtime[],
): CityDayMovieGroup[] {
  const groups = new Map<string, CityDayMovieGroup>()
  for (const showtime of [...showtimes].sort((left, right) =>
    left.start_time.localeCompare(right.start_time),
  )) {
    const group = groups.get(showtime.movie.slug)
    if (group) group.showtimes.push(showtime)
    else
      groups.set(showtime.movie.slug, {
        movie: showtime.movie,
        showtimes: [showtime],
      })
  }
  return [...groups.values()]
}

// Most screened films first, for titles and search snippets.
export function titlesByShowtimeCount(
  theaters: readonly {
    showtimes: readonly { movie: { title: string } }[]
  }[],
): string[] {
  const counts = new Map<string, number>()
  for (const theater of theaters)
    for (const showtime of theater.showtimes)
      counts.set(
        showtime.movie.title,
        (counts.get(showtime.movie.title) ?? 0) + 1,
      )
  return [...counts.entries()]
    .sort(
      (left, right) =>
        right[1] - left[1] || left[0].localeCompare(right[0], 'fr'),
    )
    .map(([title]) => title)
}
