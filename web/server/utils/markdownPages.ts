import type {
  CityDetailResponse,
  MovieShowtimesResponse,
  Showtime,
  TheaterShowtimesResponse,
  TimelineResponse,
} from '../../app/types/api.ts'
import { safeBookingUrl } from '../../app/utils/bookingUrl.ts'
import {
  type CityDayShowtime,
  cityDayTheaters,
  groupShowtimesByMovie,
  orderCityTheaters,
} from '../../app/utils/cityDay.ts'
import {
  formatLongDateWithYear,
  formatParisTime,
  formatParisUpdate,
} from '../../app/utils/date.ts'
import { formatLabel, formatRuntime } from '../../app/utils/formats.ts'
import { safePosterUrl } from '../../app/utils/safeImageUrl.ts'
import { resolveShowtimeEnd } from '../../app/utils/showtimeEnd.ts'
import { languageLabel } from '../../app/utils/showtimeFilters.ts'
import { absoluteSiteUrl } from '../../app/utils/siteUrl.ts'
import { markdownText, markdownUrl } from './markdown.ts'

export const MARKDOWN_CONTENT_TYPE = 'text/markdown; charset=utf-8'
export const MARKDOWN_DATE_LINKS = 14

export const MARKDOWN_PAGE_PATTERN =
  /^\/(film|cinema|ville)\/([A-Za-z0-9_.-]{1,120})\.md$/

export type MarkdownPageKind = 'film' | 'cinema' | 'ville'

export interface MarkdownPageRequest {
  kind: MarkdownPageKind
  slug: string
  date?: string
  city?: string
  page?: number
}

const SLUG_PATTERN = /^[A-Za-z0-9_.-]{1,120}$/
const DATE_PATTERN = /^\d{4}-\d{2}-\d{2}$/

export class MarkdownQueryError extends Error {}

function validDate(value: string): boolean {
  if (!DATE_PATTERN.test(value)) return false
  const [year, month, day] = value.split('-').map(Number)
  const parsed = new Date(Date.UTC(year!, month! - 1, day!, 12))
  return parsed.toISOString().slice(0, 10) === value
}

// Query values arrive untrusted; only documented, well-formed ones pass.
export function parseMarkdownPageRequest(
  pathname: string,
  query: URLSearchParams,
): MarkdownPageRequest | null {
  const match = MARKDOWN_PAGE_PATTERN.exec(pathname)
  if (!match) return null
  const request: MarkdownPageRequest = {
    kind: match[1] === 'film' || match[1] === 'ville' ? match[1] : 'cinema',
    slug: match[2]!,
  }
  const date = query.get('date')
  if (date !== null) {
    if (!validDate(date))
      throw new MarkdownQueryError('Le paramètre date doit être AAAA-MM-JJ.')
    request.date = date
  }
  if (request.kind === 'film') {
    const city = query.get('city')
    if (city !== null) {
      if (!SLUG_PATTERN.test(city))
        throw new MarkdownQueryError('Le paramètre city est invalide.')
      request.city = city
    }
  }
  if (request.kind !== 'cinema') {
    const page = query.get('page')
    if (page !== null) {
      const value = Number(page)
      if (!/^[1-9]\d{0,3}$/.test(page) || !Number.isSafeInteger(value))
        throw new MarkdownQueryError(
          'Le paramètre page doit être un entier positif.',
        )
      request.page = value
    }
  }
  return request
}

function link(text: string, url: string): string {
  return `[${markdownText(text)}](${markdownUrl(url)})`
}

function markdownPageUrl(
  siteUrl: string,
  request: MarkdownPageRequest,
): string {
  const url = new URL(
    absoluteSiteUrl(
      siteUrl,
      `/${request.kind}/${encodeURIComponent(request.slug)}.md`,
    ),
  )
  if (request.date) url.searchParams.set('date', request.date)
  if (request.city) url.searchParams.set('city', request.city)
  if (request.page && request.page > 1)
    url.searchParams.set('page', String(request.page))
  return url.href
}

function dateHeading(date: string): string {
  return `${formatLongDateWithYear(date)} (${date})`
}

function otherDates(
  siteUrl: string,
  current: string,
  dates: readonly string[],
  request: (date: string) => MarkdownPageRequest,
): string[] {
  const others = dates.filter((date) => date !== current)
  if (others.length === 0) return []
  const shown = others.slice(0, MARKDOWN_DATE_LINKS)
  const more =
    others.length > shown.length
      ? ` (et ${others.length - shown.length} autres)`
      : ''
  return [
    '',
    `Autres dates : ${shown.map((date) => link(date, markdownPageUrl(siteUrl, request(date)))).join(', ')}${more}`,
  ]
}

function showtimeLine(
  showtime: Omit<CityDayShowtime, 'movie'>,
  theaterId: string,
  originalLanguage: string | null | undefined,
): string {
  const end = resolveShowtimeEnd(showtime)
  const time = end
    ? `${formatParisTime(showtime.start_time)} → ${end.estimated ? '~' : ''}${formatParisTime(end.time)}`
    : formatParisTime(showtime.start_time)
  const parts = [time]
  if (showtime.language)
    parts.push(languageLabel(showtime.language, originalLanguage))
  if (showtime.format) parts.push(formatLabel(showtime.format))
  if (showtime.room.trim()) parts.push(markdownText(showtime.room))
  const booking = safeBookingUrl(
    showtime.booking_url,
    showtime.provider,
    showtime.id,
    theaterId,
  )
  if (booking)
    parts.push(
      link(
        booking.kind === 'booking' ? 'Réserver' : 'Site du cinéma',
        booking.url,
      ),
    )
  return `- ${parts.join(' · ')}`
}

function footer(siteUrl: string, htmlPath: string, apiUrl: string): string[] {
  return [
    '',
    '---',
    '',
    `Page complète : ${link(absoluteSiteUrl(siteUrl, htmlPath), absoluteSiteUrl(siteUrl, htmlPath))}`,
    `Données JSON : ${link(apiUrl, apiUrl)} (documentation : ${absoluteSiteUrl(siteUrl, '/api/v1/openapi.json')})`,
    `Heures à Paris. « ~ » signale une fin estimée, publicité comprise. Source : MesSeances, ${absoluteSiteUrl(siteUrl, '/')}`,
    '',
  ]
}

export function renderFilmMarkdown(
  siteUrl: string,
  schedule: MovieShowtimesResponse,
  request: MarkdownPageRequest,
): string {
  const movie = schedule.movie
  const htmlPath = `/film/${encodeURIComponent(movie.slug)}`
  const lines = [`# ${markdownText(movie.title)}`]
  if (movie.overview?.trim())
    lines.push('', `> ${markdownText(movie.overview)}`)
  const facts: string[] = []
  if (
    movie.original_title?.trim() &&
    movie.original_title.trim() !== movie.title.trim()
  )
    facts.push(`Titre original : ${markdownText(movie.original_title)}`)
  if (movie.runtime_minutes > 0)
    facts.push(`Durée : ${formatRuntime(movie.runtime_minutes)}`)
  if (movie.genres.length)
    facts.push(`Genres : ${movie.genres.map(markdownText).join(', ')}`)
  if (movie.french_release_date && validDate(movie.french_release_date))
    facts.push(`Sortie en France : ${movie.french_release_date}`)
  else if (movie.release_date && validDate(movie.release_date))
    facts.push(`Sortie : ${movie.release_date}`)
  const poster = safePosterUrl(movie.poster_url)
  if (poster) facts.push(`Affiche : ${markdownUrl(poster)}`)
  facts.push(`Slug : \`${movie.slug}\``)
  lines.push('', ...facts.map((fact) => `- ${fact}`))

  const scope = request.city
    ? `à ${markdownText(schedule.theaters[0]?.city ?? request.city)}`
    : 'en France'
  lines.push('', `## Séances du ${dateHeading(schedule.date)} ${scope}`)
  lines.push(
    ...otherDates(siteUrl, schedule.date, schedule.available_dates, (date) => ({
      ...request,
      slug: movie.slug,
      date,
      page: undefined,
    })),
  )

  const theaters = schedule.theaters.filter(
    (theater) => theater.showtimes.length > 0,
  )
  if (theaters.length === 0) {
    lines.push(
      '',
      schedule.available_dates.length
        ? 'Aucune séance à cette date.'
        : schedule.release_status === 'upcoming'
          ? 'Pas encore de séance programmée : le film sort prochainement.'
          : 'Aucune séance programmée actuellement.',
    )
  }
  for (const theater of theaters) {
    lines.push(
      '',
      `### ${link(theater.name, absoluteSiteUrl(siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`))} · ${markdownText(theater.city)}`,
      '',
      ...[...theater.showtimes]
        .sort((left, right) => left.start_time.localeCompare(right.start_time))
        .map((showtime) =>
          showtimeLine(showtime, theater.id, movie.original_language),
        ),
    )
  }

  const pagination = schedule.pagination
  if (pagination && pagination.total > pagination.page_size) {
    const pages = Math.ceil(pagination.total / pagination.page_size)
    const navigation = [
      `Page ${pagination.page} sur ${pages} (${pagination.total} cinémas)`,
    ]
    if (pagination.page > 1)
      navigation.push(
        link(
          'cinémas précédents',
          markdownPageUrl(siteUrl, {
            ...request,
            slug: movie.slug,
            date: schedule.date,
            page: pagination.page - 1,
          }),
        ),
      )
    if (pagination.has_more)
      navigation.push(
        link(
          'cinémas suivants',
          markdownPageUrl(siteUrl, {
            ...request,
            slug: movie.slug,
            date: schedule.date,
            page: pagination.page + 1,
          }),
        ),
      )
    lines.push('', navigation.join(' · '))
  }

  const api = new URL(
    absoluteSiteUrl(
      siteUrl,
      `/api/v1/movies/${encodeURIComponent(movie.slug)}/showtimes`,
    ),
  )
  api.searchParams.set('date', schedule.date)
  if (request.city) api.searchParams.set('city', request.city)
  lines.push(...footer(siteUrl, htmlPath, api.href))
  return lines.join('\n')
}

export function renderCinemaMarkdown(
  siteUrl: string,
  response: TheaterShowtimesResponse,
): string {
  const theater = response.theater
  const htmlPath = `/cinema/${encodeURIComponent(theater.slug)}`
  const lines = [`# ${markdownText(theater.name)}`, '']
  const address = [
    theater.address,
    [theater.postal_code, theater.city].filter((part) => part.trim()).join(' '),
  ]
    .map((part) => part.trim())
    .filter(Boolean)
    .join(', ')
  if (address) lines.push(`- Adresse : ${markdownText(address)}`)
  lines.push(
    `- Ville : ${link(theater.city, absoluteSiteUrl(siteUrl, `/ville/${encodeURIComponent(theater.city_slug)}/cinemas`))}`,
  )
  lines.push(`- Exploitant : \`${theater.provider}\``)
  const technologies = response.technologies ?? []
  if (technologies.length)
    lines.push(`- Technologies : ${technologies.map(formatLabel).join(', ')}`)
  if (theater.accepted_passes.length)
    lines.push(
      `- Cartes acceptées : ${theater.accepted_passes.map(markdownText).join(', ')}`,
    )
  lines.push(`- Slug : \`${theater.slug}\`, id : \`${theater.id}\``)

  const date = response.date
  if (!date) {
    lines.push('', 'Aucune séance programmée actuellement.')
  } else {
    lines.push('', `## Séances du ${dateHeading(date)}`)
    lines.push(
      ...otherDates(siteUrl, date, theater.available_dates, (other) => ({
        kind: 'cinema',
        slug: theater.slug,
        date: other,
      })),
    )
    const groups = new Map<string, Showtime[]>()
    for (const showtime of [...response.showtimes].sort((left, right) =>
      left.start_time.localeCompare(right.start_time),
    )) {
      const group = groups.get(showtime.movie.slug) ?? []
      group.push(showtime)
      groups.set(showtime.movie.slug, group)
    }
    if (groups.size === 0) lines.push('', 'Aucune séance à cette date.')
    for (const [slug, showtimes] of groups) {
      const movie = showtimes[0]!.movie
      const runtime =
        movie.runtime_minutes > 0
          ? ` · ${formatRuntime(movie.runtime_minutes)}`
          : ''
      lines.push(
        '',
        `### ${link(movie.title, absoluteSiteUrl(siteUrl, `/film/${encodeURIComponent(slug)}`))}${runtime}`,
        '',
        ...showtimes.map((showtime) =>
          showtimeLine(showtime, theater.id, movie.original_language),
        ),
      )
    }
  }

  const api = new URL(
    absoluteSiteUrl(
      siteUrl,
      `/api/v1/theaters/${encodeURIComponent(theater.slug)}/showtimes`,
    ),
  )
  if (date) api.searchParams.set('date', date)
  lines.push(...footer(siteUrl, htmlPath, api.href))
  return lines.join('\n')
}

export interface CityMarkdownView {
  date: string
  dates: readonly string[]
  page: number
  pageCount: number
}

export function renderCityMarkdown(
  siteUrl: string,
  detail: CityDetailResponse,
  timeline: TimelineResponse | null,
  view: CityMarkdownView,
): string {
  const city = detail.city
  const htmlPath = `/ville/${encodeURIComponent(city.slug)}`
  const request = (date: string, page?: number): MarkdownPageRequest => ({
    kind: 'ville',
    slug: city.slug,
    date,
    page,
  })
  const updated = formatParisUpdate(detail.generated_at)
  const lines = [
    `# Séances de cinéma à ${markdownText(city.name)}`,
    '',
    `- Cinémas : ${detail.theaters.length} (${link('annuaire', absoluteSiteUrl(siteUrl, `${htmlPath}/cinemas`))})`,
    `- Slug de ville : \`${city.slug}\``,
  ]
  if (updated) lines.push(`- Mise à jour : ${updated}`)
  lines.push('', `## Séances du ${dateHeading(view.date)}`)
  lines.push(
    ...otherDates(siteUrl, view.date, view.dates, (date) => request(date)),
  )

  const sessions = new Map(
    timeline?.theaters.map((theater) => [theater.id, theater.showtimes]) ?? [],
  )
  const theaters = cityDayTheaters(orderCityTheaters(detail), view.page)
  if (theaters.length === 0)
    lines.push('', 'Aucun cinéma programmé dans cette ville.')
  for (const theater of theaters) {
    const address = [theater.address, theater.postal_code]
      .map((part) => part.trim())
      .filter(Boolean)
      .filter((part, index, parts) => index === 0 || !parts[0]!.includes(part))
      .join(', ')
    lines.push(
      '',
      `### ${link(theater.name, absoluteSiteUrl(siteUrl, `/cinema/${encodeURIComponent(theater.slug)}`))}`,
    )
    if (address) lines.push('', markdownText(address))
    const groups = groupShowtimesByMovie(sessions.get(theater.id) ?? [])
    if (groups.length === 0) lines.push('', 'Aucune séance ce jour.')
    for (const group of groups) {
      const runtime =
        group.movie.runtime_minutes > 0
          ? ` · ${formatRuntime(group.movie.runtime_minutes)}`
          : ''
      lines.push(
        '',
        `#### ${link(group.movie.title, absoluteSiteUrl(siteUrl, `/film/${encodeURIComponent(group.movie.slug)}`))}${runtime}`,
        '',
        ...group.showtimes.map((showtime) =>
          showtimeLine(showtime, theater.id, group.movie.original_language),
        ),
      )
    }
  }

  if (view.pageCount > 1) {
    const navigation = [
      `Page ${view.page} sur ${view.pageCount} (${detail.theaters.length} cinémas)`,
    ]
    if (view.page > 1)
      navigation.push(
        link(
          'cinémas précédents',
          markdownPageUrl(siteUrl, request(view.date, view.page - 1)),
        ),
      )
    if (view.page < view.pageCount)
      navigation.push(
        link(
          'cinémas suivants',
          markdownPageUrl(siteUrl, request(view.date, view.page + 1)),
        ),
      )
    lines.push('', navigation.join(' · '))
  }

  const api = new URL(absoluteSiteUrl(siteUrl, '/api/v1/timeline'))
  api.searchParams.set('date', view.date)
  api.searchParams.set(
    'theaters',
    theaters.map((theater) => theater.id).join(','),
  )
  lines.push(
    '',
    'Légende : VF version française, VOF version originale française, VOSTFR version originale sous-titrée en français, VO version originale.',
    ...footer(siteUrl, htmlPath, api.href),
  )
  return lines.join('\n')
}
