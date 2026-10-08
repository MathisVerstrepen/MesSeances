// Synthetic public inventory, shared by SSR mocks, browser tests and CLI sessions.
export const date = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Europe/Paris',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
}).format(new Date())
export const generatedAt = `${date}T08:00:00Z`
export const theater = {
  provider: 'ugc',
  id: 'fixture-cinema',
  slug: 'cinema-playwright',
  name: 'Cinéma Playwright',
  address: '12 rue du Test',
  city: 'Lille',
  city_slug: 'lille',
  postal_code: '59000',
  available_dates: [date],
  accepted_passes: [],
}
export const movie = {
  slug: 'film-playwright',
  title: 'Film Playwright',
  original_language: 'fr',
  runtime_minutes: 90,
  updated_at: generatedAt,
  poster_url: null,
  tmdb_id: null,
  imdb_id: null,
  metacritic_id: null,
  overview: 'Un film de fixture.',
  release_date: date,
  french_release_date: date,
  genres: [],
  showtime_count: 1,
}
export function showtimes(selectedDate = date) {
  return {
    generated_at: generatedAt,
    timezone: 'Europe/Paris',
    theater,
    date: selectedDate,
    showtimes:
      selectedDate === date
        ? [
            {
              provider: 'ugc',
              id: 'fixture-showtime',
              movie,
              start_time: `${date}T14:00:00+02:00`,
              end_time: `${date}T15:30:00+02:00`,
              estimated_end_time: null,
              estimated_end_ads_minutes: null,
              language: 'VF',
              format: '2D',
              room: '1',
              booking_url: null,
              start_offset_minutes: 840,
              duration_minutes: 90,
              poster_url: null,
              backdrop_url: null,
            },
          ]
        : [],
  }
}

export const secondTheater = {
  ...theater,
  id: 'fixture-second',
  slug: 'cinema-second',
  name: 'Cinéma Seconde Salle',
  city: 'Roubaix',
}

export const watchlistMovies = [
  {
    ...movie,
    title:
      'Un voyage au cinéma avec les histoires et les lumières de toutes les salles du quartier',
    tag_ids: ['1', '2'],
  },
  {
    ...movie,
    slug: 'film-imported',
    title: 'Film importé sans séances',
    tag_ids: ['1'],
  },
  {
    ...movie,
    slug: 'film-grace',
    title: 'La séance vient de commencer',
    tag_ids: [],
  },
  {
    ...movie,
    slug: 'film-distant',
    title: 'Une séance dans deux mois',
    tag_ids: [],
  },
  {
    ...movie,
    slug: 'film-after-tuesday',
    title: 'Une séance après mardi, dans les sept jours',
    tag_ids: [],
  },
]

export function syntheticWatchlist(username, populated = false) {
  return {
    username,
    revision: populated ? '1' : '0',
    sort_order: 'added_desc',
    view_mode: 'list',
    filter_tag_id: null,
    tags: populated
      ? [
          { id: '1', name: 'À voir', color: 'blue' },
          { id: '2', name: 'En famille', color: 'green' },
        ]
      : [],
    items: populated
      ? watchlistMovies.map((item, index) => ({
          ...item,
          added_at: `${date}T08:00:${String(watchlistMovies.length - index).padStart(2, '0')}Z`,
        }))
      : [],
    external_search_available: populated,
  }
}

export function screeningCatalog(query) {
  const from = new Date(`${date}T00:00:00Z`)
  const dayCount = ((2 - from.getUTCDay() + 7) % 7) + 1
  const through = new Date(from)
  through.setUTCDate(through.getUTCDate() + dayCount - 1)
  const secondOnly = query.get('theaters') === secondTheater.id
  const items = secondOnly
    ? []
    : [
        {
          ...watchlistMovies[0],
          showtime_count: 30,
          remaining_showtime_count: dayCount * 2 + 1,
          next_7_days_showtime_count: dayCount * 2 + 2,
        },
        {
          ...watchlistMovies[2],
          showtime_count: 1,
          remaining_showtime_count: 0,
          next_7_days_showtime_count: 0,
        },
        {
          ...watchlistMovies[3],
          showtime_count: 4,
          remaining_showtime_count: 0,
          next_7_days_showtime_count: 0,
        },
        {
          ...watchlistMovies[4],
          showtime_count: 1,
          remaining_showtime_count: 0,
          next_7_days_showtime_count: 1,
        },
      ]
  const page = Number(query.get('page') || 1)
  return {
    items: items.slice((page - 1) * 100, page * 100),
    available_genres: [],
    page,
    page_size: 100,
    total: items.length,
    generated_at: generatedAt,
    catalog_revision: 'screenings-fixture-1',
    screening_window: {
      as_of: `${date}T10:00:00+02:00`,
      timezone: 'Europe/Paris',
      from: date,
      through: through.toISOString().slice(0, 10),
      day_count: dayCount,
    },
  }
}
export function accountActivityItem(id, cinema = theater) {
  return {
    event_id: String(id),
    type: id === 103 ? 'return_to_program' : 'added_to_program',
    detected_at: `${date}T08:00:00.${String(id).padStart(6, '0')}Z`,
    first_screening_date: date,
    previous_program_end_date: id === 103 ? '2026-08-01' : null,
    movie: {
      slug: movie.slug,
      title: movie.title,
      poster_url: null,
      updated_at: generatedAt,
    },
    has_upcoming_showtimes: true,
    next_showtime_date: date,
    theater: {
      id: cinema.id,
      slug: cinema.slug,
      name: cinema.name,
      city: cinema.city,
      provider: cinema.provider,
    },
  }
}

export const publicActivityItems = [
  {
    ...accountActivityItem(203),
    theater: undefined,
    type: 'return_to_program',
    detected_at: '2026-10-01T22:30:00Z',
    first_screening_date: '2026-10-09',
    previous_program_end_date: '2026-08-01',
    movie: {
      ...accountActivityItem(203).movie,
      title: 'Les lumières reviennent',
    },
  },
  {
    ...accountActivityItem(202),
    theater: undefined,
    detected_at: '2026-10-01T22:00:00Z',
    first_screening_date: '2026-10-16',
    movie: {
      ...accountActivityItem(202).movie,
      title: 'Une nouvelle histoire',
    },
    has_upcoming_showtimes: false,
    next_showtime_date: null,
  },
  {
    ...accountActivityItem(201),
    theater: undefined,
    detected_at: '2026-10-01T21:59:59Z',
    first_screening_date: '2026-10-23',
    movie: {
      ...accountActivityItem(201).movie,
      title: `Un voyage à travers les salles et les histoires du cinéma ${'International'.repeat(8)}`,
    },
  },
]

export function publicActivityPage(
  cursor,
  cinema = theater,
  mode = 'populated',
) {
  const visible = mode === 'populated' ? publicActivityItems : []
  return {
    generated_at: generatedAt,
    timezone: 'Europe/Paris',
    theater: cinema,
    coverage: {
      history_started_at:
        mode === 'initializing' ? null : '2026-09-28T08:00:00Z',
      last_publication_at: generatedAt,
      source_generated_at: generatedAt,
      completeness: mode === 'initializing' ? 'unknown' : 'partial',
      bootstrap: 'baseline',
      return_minimum_break_days: 28,
    },
    items: cursor ? visible.slice(1) : visible.slice(0, 2),
    limit: 20,
    next_cursor: !cursor && visible.length > 2 ? 'public-page-2' : null,
  }
}
