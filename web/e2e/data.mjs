// Synthetic public inventory, shared by SSR mocks, browser tests and CLI sessions.
export const date = new Intl.DateTimeFormat('en-CA', {
  timeZone: 'Europe/Paris',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
}).format(new Date())
export const generatedAt = `${date}T08:00:00Z`
const windowEnd = new Date(`${date}T12:00:00Z`)
windowEnd.setUTCDate(windowEnd.getUTCDate() + 6)
export const discoveryWindow = {
  from: date,
  through: windowEnd.toISOString().slice(0, 10),
  timezone: 'Europe/Paris',
}
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
    discovery: cinemaDiscovery(),
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
  city_slug: 'roubaix',
}

export const alternativeTheater = {
  ...theater,
  id: 'fixture-alternative',
  slug: 'cinéma & lumière',
  name: 'Cinéma des Lumières et des Rencontres Internationales',
  address: '8 rue des Lumières',
}
export const quietTheater = {
  ...theater,
  id: 'fixture-quiet',
  slug: 'cinema-sans-seances',
  name: 'Cinéma sans séances',
}
export const discoveryMovies = [
  { ...movie, showtime_count: 12 },
  {
    ...movie,
    slug: 'film & inconnu',
    title: 'Les histoires de toutes les salles et les lumières du quartier',
    runtime_minutes: 0,
    showtime_count: 9,
  },
  ...['Alpha', 'Écho', 'Écho bis', 'Zèbre'].map((title, index) => ({
    ...movie,
    slug: `film-${index}`,
    title,
    showtime_count: 2,
  })),
]
export const discoveryCities = [
  { name: 'Lille', slug: 'lille', theater_count: 2, showtime_count: 20 },
  { name: 'Roubaix', slug: 'roubaix', theater_count: 1, showtime_count: 4 },
  ...['Amiens', 'Arras', 'Paris & proche', 'Tourcoing'].map((name) => ({
    name,
    slug: name.toLowerCase(),
    theater_count: 1,
    showtime_count: 2,
  })),
]
export const publicCities = discoveryCities.map((city) => ({
  name: city.name,
  slug: city.slug,
  theaters:
    city.slug === 'lille'
      ? [theater, alternativeTheater, quietTheater]
      : city.slug === 'roubaix'
        ? [secondTheater]
        : [
            {
              ...theater,
              id: `fixture-${city.slug}`,
              slug: `cinema-${city.slug}`,
              name: `Cinéma ${city.name}`,
              city: city.name,
              city_slug: city.slug,
            },
          ],
}))
export function cinemaDiscovery(mode = 'populated', venue = theater) {
  if (mode === 'null-window')
    return { window: null, movies: [], other_theaters: [] }
  const empty = mode === 'empty' || venue.id === quietTheater.id
  const others =
    mode === 'single-cinema' || empty
      ? []
      : (publicCities
          .find((city) => city.slug === venue.city_slug)
          ?.theaters.filter(
            (item) => item.id !== venue.id && item.id !== quietTheater.id,
          ) ?? [])
  return {
    window: discoveryWindow,
    movies: empty ? [] : discoveryMovies,
    other_theaters: others.map((item) => ({
      provider: item.provider,
      id: item.id,
      slug: item.slug,
      name: item.name,
      city: item.city,
      city_slug: item.city_slug,
      movie_count: 3,
      showtime_count: 8,
    })),
  }
}
export function cityDetail(slug, mode = 'populated') {
  const city = publicCities.find((item) => item.slug === slug)
  if (!city) return null
  const venues =
    mode === 'single-cinema' ? city.theaters.slice(0, 1) : city.theaters
  return {
    generated_at: generatedAt,
    city: { ...city, theaters: venues },
    theaters: venues,
    movies: [movie],
    discovery: {
      window: mode === 'null-window' ? null : discoveryWindow,
      theaters: venues.map((venue) => ({
        id: venue.id,
        movie_count:
          mode === 'empty' || venue.id === quietTheater.id
            ? 0
            : venue.id === theater.id
              ? 6
              : 3,
        showtime_count:
          mode === 'empty' || venue.id === quietTheater.id
            ? 0
            : venue.id === theater.id
              ? 29
              : 8,
      })),
    },
  }
}
export function movieSchedule(slug, query, mode = 'populated') {
  const canonicalSlug = slug === 'merged-film' ? movie.slug : slug
  const entry =
    discoveryMovies.find((item) => item.slug === canonicalSlug) ??
    (['film-ended', 'film-upcoming'].includes(slug)
      ? {
          ...movie,
          slug,
          title: slug === 'film-ended' ? 'Film terminé' : 'Film à venir',
          french_release_date: slug === 'film-upcoming' ? '2099-01-01' : date,
        }
      : null)
  if (!entry) return null
  const inactive = ['film-ended', 'film-upcoming'].includes(slug)
  const selectedDate = query.get('date') || date
  const venues = [theater, alternativeTheater, secondTheater]
  const selected = query.get('theaters')?.split(',')
  const empty =
    inactive ||
    mode === 'empty' ||
    selectedDate !== date ||
    query.get('language') === 'VOF'
  return {
    release_status: inactive
      ? slug === 'film-ended'
        ? 'ended'
        : 'upcoming'
      : 'showing',
    movie: entry,
    backdrop_url: null,
    date: selectedDate,
    currently_screened: !inactive,
    available_dates: inactive ? [] : [date],
    theaters: empty
      ? []
      : venues
          .filter((venue) => !selected || selected.includes(venue.id))
          .map((venue) => ({
            ...venue,
            showtimes: showtimes().showtimes.map((session) => ({
              ...session,
              id: `${session.id}-${venue.id}`,
              movie: entry,
            })),
          })),
    catalog_revision: 'fixture-1',
    available_languages: inactive ? [] : ['VF'],
    available_formats: inactive ? [] : ['2D'],
    pagination: {
      page: Number(query.get('page') || 1),
      page_size: 10,
      total: empty ? 0 : venues.length,
      has_more: false,
    },
    discovery: {
      window: mode === 'null-window' ? null : discoveryWindow,
      cities:
        inactive || mode === 'empty' || mode === 'null-window'
          ? []
          : discoveryCities,
    },
  }
}
export function sitemapData() {
  const movies = [
    ...discoveryMovies,
    {
      ...movie,
      slug: 'film-ended',
      showtime_count: 0,
      imdb_id: 'tt1234567',
      genres: ['Drame'],
    },
    { ...movie, slug: 'film-thin', showtime_count: 0, overview: null },
  ]
  // Existing legacy browser identities are synthetic aliases, never sitemap candidates.
  const candidates = movies.map((entry, index) => ({
    ...entry,
    slug: `film-${100 + index}`,
  }))
  const paths = [
    ...candidates.map((item) => `/film/${encodeURIComponent(item.slug)}`),
    ...publicCities.map(
      (city) => `/ville/${encodeURIComponent(city.slug)}/cinemas`,
    ),
    ...publicCities.flatMap((city) =>
      city.theaters.map((venue) => `/cinema/${encodeURIComponent(venue.slug)}`),
    ),
  ]
  return {
    as_of: generatedAt,
    revision: 'schedule:9007199254740993;enrichment:2;location:1',
    movies: candidates,
    movie_total: movies.length,
    cities: { generated_at: generatedAt, items: publicCities },
    upcoming_available: true,
    lastmod_by_path: Object.fromEntries(
      paths.map((path, index) => [path, index % 2 ? generatedAt : null]),
    ),
  }
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

// Fixed release calendar intentionally ends in 2025, not the browser's current year.
// Sparse weeks, multi-film boundary weeks, two retained years and more than one page.
function releaseMovie(id, title, frenchDate) {
  return {
    ...movie,
    slug: `film-${id}`,
    title,
    release_date: '2000-01-01',
    french_release_date: frenchDate,
    showtime_count: 0,
  }
}
export const releaseHistory = [
  releaseMovie(201, 'Le premier novembre', '2025-11-01'),
  releaseMovie(202, 'La dernière nuit d’octobre', '2025-10-31'),
  releaseMovie(203, 'Les lumières du mercredi', '2025-10-29'),
  releaseMovie(204, 'Le mercredi des histoires', '2025-10-22'),
  releaseMovie(205, 'Les salles du quartier', '2025-10-15'),
  releaseMovie(206, 'Un écran en automne', '2025-10-08'),
  releaseMovie(207, 'Le début d’octobre', '2025-10-01'),
  releaseMovie(208, 'La dernière séance de septembre', '2025-09-30'),
  releaseMovie(209, 'Un mercredi de septembre', '2025-09-17'),
  releaseMovie(210, 'Le premier jour de 2025', '2025-01-01'),
  releaseMovie(211, 'Le dernier jour de 2024', '2024-12-31'),
  releaseMovie(212, 'Le premier jour de 2024', '2024-01-01'),
]
export const upcomingReleases = [
  releaseMovie(301, 'Le prochain mercredi', '2026-10-14'),
  releaseMovie(302, 'Une autre sortie à venir', '2026-10-28'),
  releaseMovie(303, 'Les films de novembre', '2026-11-11'),
  releaseMovie(304, 'Les films de décembre', '2026-12-02'),
  releaseMovie(305, 'Une sortie plus lointaine', '2026-12-16'),
]
function releaseWeek(value) {
  const day = new Date(`${value}T12:00:00Z`)
  day.setUTCDate(day.getUTCDate() - ((day.getUTCDay() + 4) % 7))
  return day.toISOString().slice(0, 10)
}
export function releaseCatalog(query, mode = 'populated') {
  const history = query.get('view') === 'history'
  const eligible = mode === 'empty' ? [] : releaseHistory
  const availableYears = [
    ...new Set(
      eligible.map((item) => Number(item.french_release_date.slice(0, 4))),
    ),
  ].sort((left, right) => right - left)
  const year = history
    ? Number(query.get('year')) || availableYears[0] || null
    : null
  const month = history && year ? Number(query.get('month')) || null : null
  const inYear = eligible.filter(
    (item) => Number(item.french_release_date.slice(0, 4)) === year,
  )
  const availableMonths = [
    ...new Set(
      inYear.map((item) => Number(item.french_release_date.slice(5, 7))),
    ),
  ].sort((left, right) => left - right)
  const items = history
    ? inYear.filter(
        (item) =>
          !month || Number(item.french_release_date.slice(5, 7)) === month,
      )
    : mode === 'empty'
      ? []
      : upcomingReleases
  const weeks = [
    ...new Set(items.map((item) => releaseWeek(item.french_release_date))),
  ]
  const page = Number(query.get('page') || 1)
  const pageWeeks = new Set(weeks.slice((page - 1) * 4, page * 4))
  return {
    generated_at: '2026-10-09T08:00:00Z',
    catalog_revision: 'release-fixture-1',
    timezone: 'Europe/Paris',
    view: history ? 'history' : 'upcoming',
    year,
    month,
    available_years: history ? availableYears : [],
    available_months: history ? availableMonths : [],
    window: history ? null : { from: '2026-10-14', through: '2027-10-09' },
    items: items.filter((item) =>
      pageWeeks.has(releaseWeek(item.french_release_date)),
    ),
    page,
    total: items.length,
    total_weeks: weeks.length,
    total_pages: Math.ceil(weeks.length / 4),
  }
}
