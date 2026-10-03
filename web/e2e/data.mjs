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
