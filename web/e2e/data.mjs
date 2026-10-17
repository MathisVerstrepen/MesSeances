// Synthetic public inventory, shared by SSR mocks, browser tests and CLI sessions.
import { createHash } from 'node:crypto'
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

const statisticsWednesdayTime =
  Date.parse(`${date}T00:00:00Z`) -
  ((new Date(`${date}T00:00:00Z`).getUTCDay() + 4) % 7) * 86_400_000
const statisticsDay = (offset) =>
  new Date(statisticsWednesdayTime + offset * 86_400_000)
    .toISOString()
    .slice(0, 10)
export const statisticsWednesday = statisticsDay(0)
export const statisticsRelease = statisticsDay(-14)
export const statisticsFrom = statisticsDay(-7)
export const statisticsThrough = statisticsDay(21)
export function historyStatistics(query) {
  const from = query.get('date') || statisticsFrom
  const through = query.get('date_to') || statisticsThrough
  const first = Date.parse(`${from}T00:00:00Z`)
  const last = Date.parse(`${through}T00:00:00Z`)
  const rows = Array.from(
    { length: (last - first) / 86_400_000 + 1 },
    (_, index) => ({
      date: new Date(first + index * 86_400_000).toISOString().slice(0, 10),
      showtime_count: index === 1 ? 0 : (index + 1) * 37,
    }),
  )
  const count = rows.reduce((sum, row) => sum + row.showtime_count, 0)
  return {
    mode: 'history',
    generated_at: generatedAt,
    timezone: 'Europe/Paris',
    range: { from, through },
    daily_showtimes: rows,
    coverage: {
      collection_started_at: generatedAt,
      last_publication_at: generatedAt,
      recorded_window: { from, through },
      completeness: 'unknown',
      bootstrap: 'none',
      providers: [],
    },
    options: {
      cities: [],
      theaters: [],
      chains: [],
      languages: [],
      formats: [],
      genres: [],
      passes: [],
    },
    totals: { showtimes: count, movies: 1, theaters: 1, cities: 1 },
    limits: {
      options: { cities: false, theaters: false, genres: false, passes: false },
      genres: false,
      local: { cities: false, theaters: false },
    },
    top_movies: { by_showtimes: [], by_theaters: [] },
    heatmap: [],
    versions: [],
    formats: [],
    genres: [],
    runtimes: [],
    chains: [
      {
        chain: 'ugc',
        showtime_count: count - 12,
        movie_count: 1,
        theater_count: 1,
      },
      { chain: 'mk2', showtime_count: 8, movie_count: 1, theater_count: 1 },
      {
        chain: 'kinepolis',
        showtime_count: 4,
        movie_count: 1,
        theater_count: 1,
      },
    ],
    local: {
      cities: [
        {
          slug: 'lille',
          name: 'LILLE',
          movie_count: 1,
          showtime_count: 12,
          theater_count: 1,
        },
        {
          slug: 'roubaix',
          name: 'ROUBAIX',
          movie_count: 1,
          showtime_count: 8,
          theater_count: 1,
        },
      ],
      theaters: [theater, secondTheater].map((item, index) => ({
        id: item.id,
        slug: item.slug,
        name: item.name,
        city: item.city,
        city_slug: index === 0 ? 'lille' : 'roubaix',
        chain: item.provider,
        movie_count: 1,
        showtime_count: index === 0 ? 12 : 8,
      })),
    },
    concentration: {
      top_movie_count: 1,
      top_showtime_count: count,
      other_showtime_count: 0,
    },
  }
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

// Opt-in Véo inventory: existing provider scenarios retain their original counts.
export const veoTheaters = ['P1234', 'EMS1361'].map((id, index) => ({
  ...theater,
  provider: 'veo',
  id: `veo-${id}`,
  slug: `veo-${id}`,
  name: index ? 'Véo Le Castelia' : 'Véo Cinémas Muret',
  city: index ? 'Castelginest' : 'Muret',
  city_slug: index ? 'castelginest' : 'muret',
  available_dates: [
    date,
    new Date(Date.parse(`${date}T12:00:00Z`) + 86_400_000)
      .toISOString()
      .slice(0, 10),
  ],
}))
export function veoShowtimes(venue, selectedDate = date) {
  const castelia = venue.id === 'veo-EMS1361'
  const rawTheater = venue.id.slice(4)
  const id = (session) =>
    `veo-showing-${rawTheater}-${createHash('sha256').update(`${rawTheater}\0${session}`).digest('hex')}`
  const payload = showtimes(date)
  return {
    ...payload,
    date: selectedDate,
    theater: venue,
    showtimes: [0, 1, 2].map((index) => ({
      ...payload.showtimes[0],
      provider: 'veo',
      id: id(
        castelia ? `emsx13610000000${index + 1}` : `Source_00${index + 1}`,
      ),
      movie: {
        ...movie,
        slug: `veo-film-${castelia ? 'ticketingcine-' : ''}Event_One`,
        title: 'Événement Véo',
        runtime_minutes: 0,
      },
      start_time: `${selectedDate}T${14 + index * 2}:00:00+02:00`,
      end_time: `${selectedDate}T${14 + index * 2}:00:00+02:00`,
      duration_minutes: 0,
      language: 'VOSTFR',
      room: '',
      booking_url:
        index === 2
          ? 'https://achat.veocinemas.fr.evil.test/muret/r/1'
          : castelia
            ? index === 1
              ? 'https://castelginest.miracinemas.fr/'
              : 'https://castelginest.miracinemas.fr/#showsession?id=emsx136100000001'
            : `https://achat.veocinemas.fr/muret-veo/r/${19 + index}`,
    })),
  }
}

// Opt-in manifest-bound Cinéode inventory. Never contacts provider websites.
export const cineodeTheaters = [
  {
    id: '991',
    name: 'Le Don Camillo',
    city: 'Montmirail',
    city_slug: 'montmirail',
  },
  {
    id: '3955',
    name: 'La Grange',
    city: 'Vaux-le-Pénil',
    city_slug: 'vaux-le-penil',
  },
].map((venue, index) => ({
  ...theater,
  ...venue,
  provider: 'cineode',
  id: `cineode-${venue.id}`,
  slug: `cineode-${venue.id}`,
  address: '',
  postal_code: '',
  accepted_passes: [],
  available_dates: index
    ? []
    : [
        date,
        new Date(Date.parse(`${date}T12:00:00Z`) + 86_400_000)
          .toISOString()
          .slice(0, 10),
      ],
}))
export function cineodeShowtimes(venue, selectedDate = date) {
  const payload = showtimes(date)
  return {
    ...payload,
    discovery: { window: null, movies: [], other_theaters: [] },
    date: selectedDate,
    theater: venue,
    showtimes:
      venue.id === 'cineode-3955'
        ? []
        : [0, 1, 2].map((index) => {
            const start = `${selectedDate}T${14 + index * 2}:00:00+02:00`
            const film = index ? '631242' : 'atelier-local'
            return {
              ...payload.showtimes[0],
              provider: 'cineode',
              id: `cineode-showing-991-${film}-${Date.parse(start) / 1000}`,
              movie: {
                ...movie,
                slug: `cineode-film-${film}`,
                title: index ? 'Programme Cinéode' : "L'été",
                runtime_minutes: index ? 100 : 0,
              },
              start_time: start,
              end_time: start,
              estimated_end_time:
                index === 1 ? `${selectedDate}T17:55:00+02:00` : null,
              estimated_end_ads_minutes: index === 1 ? 15 : null,
              duration_minutes: 0,
              start_offset_minutes: (14 + index * 2) * 60,
              language: index ? 'VF' : 'VOSTFR',
              format: index === 1 ? '3D' : '2D',
              room: '',
              booking_url:
                index === 2
                  ? 'https://www.cineode.fr/la-grange/seances/'
                  : 'https://www.cineode.fr/le-don-camillo/seances/',
              poster_url: null,
              backdrop_url: null,
            }
          }),
  }
}

export function cineodeMovieSchedule(slug, query) {
  if (!['cineode-film-atelier-local', 'cineode-film-631242'].includes(slug))
    return null
  const payload = cineodeShowtimes(
    cineodeTheaters[0],
    query.get('date') || date,
  )
  const sessions = payload.showtimes.filter((item) => item.movie.slug === slug)
  return {
    ...movieSchedule(movie.slug, query),
    movie: sessions[0].movie,
    theaters: [{ ...payload.theater, showtimes: sessions }],
    available_dates: payload.theater.available_dates,
    date: payload.date,
    available_languages: [...new Set(sessions.map((item) => item.language))],
    available_formats: [...new Set(sessions.map((item) => item.format))],
    pagination: { page: 1, page_size: 10, total: 1, has_more: false },
    discovery: { window: null, cities: [] },
  }
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
  const candidate =
    discoveryMovies.find((item) => item.slug === canonicalSlug) ??
    (['film-ended', 'film-upcoming', 'film-no-release'].includes(slug)
      ? {
          ...movie,
          slug,
          title:
            slug === 'film-ended'
              ? 'Film terminé'
              : slug === 'film-upcoming'
                ? 'Film à venir'
                : 'Film sans sortie française',
          french_release_date:
            slug === 'film-upcoming'
              ? '2099-01-01'
              : slug === 'film-no-release'
                ? null
                : date,
          release_date: slug === 'film-no-release' ? '2001-01-01' : date,
        }
      : null)
  if (!candidate) return null
  // Statistics and detail pages share one complete, canonical film contract.
  const entry =
    canonicalSlug === movie.slug
      ? {
          ...candidate,
          french_release_date: statisticsRelease,
          release_date: '2001-01-01',
        }
      : candidate
  const inactive = ['film-ended', 'film-upcoming', 'film-no-release'].includes(
    slug,
  )
  const selectedDate = query.get('date') || date
  const venues =
    mode === 'no-local-programme'
      ? [theater, alternativeTheater]
      : [theater, alternativeTheater, secondTheater]
  const selected = query.get('theaters')?.split(',')
  const scopedVenues = venues.filter(
    (venue) => !selected || selected.includes(venue.id),
  )
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
    available_dates: inactive || scopedVenues.length === 0 ? [] : [date],
    theaters: empty
      ? []
      : scopedVenues.map((venue) => ({
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
      total: empty ? 0 : scopedVenues.length,
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

// Scroll scenarios use valid, distinct rows rather than artificial document height.
export function scrollInventory(query, mode = 'long') {
  const selectedDate = query.get('date') || date
  const count = mode === 'empty' ? 0 : mode === 'short' ? 1 : 10
  const venues = Array.from({ length: count }, (_, index) => ({
    ...theater,
    id: `scroll-theater-${index}`,
    name: `Cinéma de défilement ${index + 1}`,
    available_dates: [selectedDate],
  }))
  const sessions = venues.map((venue, index) => ({
    ...showtimes().showtimes[0],
    id: `scroll-session-${index}`,
    movie: {
      ...movie,
      slug: `scroll-movie-${index}`,
      title: `Film de défilement ${index + 1}`,
    },
    start_time: `${selectedDate}T14:00:00+02:00`,
    end_time: `${selectedDate}T15:30:00+02:00`,
  }))
  return {
    venues,
    timeline: {
      date: selectedDate,
      timezone: 'Europe/Paris',
      window_start_time: `${selectedDate}T08:00:00+02:00`,
      window_end_time: `${selectedDate}T23:59:00+02:00`,
      theaters: venues.map((venue, index) => ({
        ...venue,
        showtimes: [sessions[index]],
      })),
    },
    slots: sessions.map((session, index) => ({
      showtime: session,
      theater: venues[index],
      poster_url: null,
      backdrop_url: null,
      effective_start_time: session.start_time,
      effective_end_time: session.end_time,
      buffer_ads_minutes: 0,
      slack_before_minutes: 120,
      slack_after_minutes: 120,
    })),
    film: {
      ...movieSchedule(movie.slug, query),
      date: selectedDate,
      available_dates: [selectedDate],
      theaters: venues.map((venue, index) => ({
        ...venue,
        showtimes: [{ ...sessions[index], movie }],
      })),
      pagination: {
        page: Number(query.get('page') || 1),
        page_size: 10,
        total: count,
        has_more: false,
      },
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

export const watchedDefaults = {
  default_visibility: 'hide',
  search_visibility: 'inherit',
  planning_visibility: 'inherit',
  films_visibility: 'inherit',
  upcoming_visibility: 'inherit',
  home_visibility: 'inherit',
  cinema_city_visibility: 'inherit',
  auto_remove_watchlist: false,
}
export const watchedMovies = Array.from({ length: 230 }, (_, index) => ({
  ...movie,
  slug: index === 0 ? movie.slug : `film-watched-${index}`,
  title: index < 100 || index >= 130 ? `Vu ${index}` : `Inédit ${index - 99}`,
}))
export const hiddenWatchedSlugs = watchedMovies
  .filter((_, index) => index < 100 || index >= 130)
  .map((item) => item.slug)
export function syntheticWatched(username, slugs = [], preferences = {}) {
  const inventory = [
    ...watchedMovies,
    ...discoveryMovies,
    ...releaseHistory,
    ...upcomingReleases,
  ]
  return {
    username,
    revision: slugs.length || Object.keys(preferences).length ? '1' : '0',
    items: slugs.map((slug) => ({
      ...(inventory.find((item) => item.slug === slug) ?? { ...movie, slug }),
      watched_at: generatedAt,
    })),
    preferences: { ...watchedDefaults, ...preferences },
  }
}
export function watchedCatalog(query, size = watchedMovies.length) {
  const page = Number(query.get('page') || 1)
  const pageSize = Math.min(100, Number(query.get('page_size') || 24))
  const search = (query.get('search') || '').toLocaleLowerCase('fr')
  const inventory = Array.from(
    { length: size },
    (_, index) =>
      watchedMovies[index] ?? {
        ...movie,
        slug: `film-watched-${index}`,
        title: `Vu ${index}`,
      },
  )
  const items = inventory.filter((item) =>
    item.title.toLocaleLowerCase('fr').includes(search),
  )
  return {
    items: items.slice((page - 1) * pageSize, page * pageSize),
    total: items.length,
    page,
    page_size: pageSize,
    available_genres: [],
    generated_at: generatedAt,
    catalog_revision: 'watched-fixture-1',
  }
}
export const watchedVenues = Array.from({ length: 12 }, (_, index) => ({
  ...theater,
  id: index === 0 ? theater.id : `fixture-watched-${index}`,
  slug: `cinema-watched-${index}`,
  name: `Salle ${index + 1}`,
}))
export function watchedShowtimes(query, timeline = false) {
  const requested =
    query.get('theaters')?.split(',') ?? watchedVenues.map((item) => item.id)
  const venues = watchedVenues.filter((item) => requested.includes(item.id))
  const rows = venues.map((venue) => {
    const visible =
      watchedVenues.findIndex((item) => item.id === venue.id) >= 10
    const film = visible ? watchedMovies[100] : movie
    const base = showtimes().showtimes[0]
    return {
      theater: venue,
      showtime: {
        ...base,
        id: `ugc-showing-${800000 + watchedVenues.findIndex((item) => item.id === venue.id)}`,
        movie: film,
        start_offset_minutes: 120,
        poster_url: null,
      },
      poster_url: null,
      backdrop_url: null,
      effective_start_time: base.start_time,
      effective_end_time: base.end_time,
      buffer_ads_minutes: 15,
      slack_before_minutes: 120,
      slack_after_minutes: 150,
    }
  })
  if (!timeline) return rows
  return {
    generated_at: generatedAt,
    timezone: 'Europe/Paris',
    date: query.get('date') || date,
    window_start_time: `${date}T12:00:00+02:00`,
    window_end_time: `${date}T23:59:00+02:00`,
    window_minutes: 719,
    theaters: rows.map((row) => ({
      ...row.theater,
      showtimes: [row.showtime],
    })),
  }
}
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
