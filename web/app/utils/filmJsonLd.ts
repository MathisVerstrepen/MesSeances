import type { MovieShowtimesResponse } from '../types/api.ts'
import type { JsonLdDocument, JsonLdNode } from './jsonLd.ts'
import { safeBackdropUrl, safePosterUrl } from './safeImageUrl.ts'
import { absoluteSiteUrl } from './siteUrl.ts'

export interface FilmJsonLdOptions {
  movieUrl: string
  siteUrl: string
  datePublished?: string
  tmdbUrl?: string
}

export function buildFilmJsonLd(schedule: MovieShowtimesResponse, options: FilmJsonLdOptions): JsonLdDocument {
  const movieId = `${options.movieUrl}#movie`
  const images = [safePosterUrl(schedule.movie.poster_url), safeBackdropUrl(schedule.backdrop_url)]
    .filter((value): value is string => Boolean(value))
  const movie: JsonLdNode = {
    '@type': 'Movie',
    '@id': movieId,
    name: schedule.movie.title,
    url: options.movieUrl
  }
  if (schedule.movie.runtime_minutes > 0) movie.duration = `PT${schedule.movie.runtime_minutes}M`
  if (schedule.movie.overview?.trim()) movie.description = schedule.movie.overview.trim()
  if (options.datePublished) movie.datePublished = options.datePublished
  if (schedule.movie.genres.length) movie.genre = schedule.movie.genres
  if (images.length === 1) movie.image = images[0]
  else if (images.length > 1) movie.image = images
  if (options.tmdbUrl) movie.sameAs = options.tmdbUrl

  const graph: JsonLdNode[] = [
    movie,
    {
      '@type': 'BreadcrumbList',
      '@id': `${options.movieUrl}#breadcrumb`,
      itemListElement: [
        { '@type': 'ListItem', position: 1, name: 'Accueil', item: absoluteSiteUrl(options.siteUrl, '/') },
        { '@type': 'ListItem', position: 2, name: 'Films', item: absoluteSiteUrl(options.siteUrl, '/films') },
        { '@type': 'ListItem', position: 3, name: schedule.movie.title, item: options.movieUrl }
      ]
    }
  ]

  for (const theater of schedule.theaters) {
    if (theater.showtimes.length === 0) continue
    const theaterUrl = absoluteSiteUrl(options.siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`)
    const theaterId = `${theaterUrl}#cinema`
    graph.push({ '@type': 'MovieTheater', '@id': theaterId, name: theater.name, url: theaterUrl })
    for (const showtime of theater.showtimes) {
      graph.push({
        '@type': 'ScreeningEvent',
        startDate: showtime.start_time,
        endDate: showtime.end_time,
        location: { '@id': theaterId },
        workPresented: { '@id': movieId }
      })
    }
  }

  return { '@context': 'https://schema.org', '@graph': graph }
}
