import type { H3Event } from 'h3'
import type {
  CityDetailResponse,
  MovieShowtimesResponse,
  TheaterShowtimesResponse,
  TimelineResponse,
} from '../../app/types/api'
import {
  cityAvailableDates,
  cityDayPageCount,
  cityDayTheaters,
  orderCityTheaters,
  resolveCityDate,
} from '../../app/utils/cityDay'
import { todayInParis } from '../../app/utils/date'
import { absoluteSiteUrl } from '../../app/utils/siteUrl'
import {
  buildInternalApiHeaders,
  initializeClientIp,
  requestIdForEvent,
} from '../utils/internalApi'
import {
  MARKDOWN_CONTENT_TYPE,
  MarkdownQueryError,
  parseMarkdownPageRequest,
  renderCinemaMarkdown,
  renderCityMarkdown,
  renderFilmMarkdown,
  type MarkdownPageRequest,
} from '../utils/markdownPages'

const CACHE_SECONDS = 300

interface MovieShowtimesQuery {
  date: string
  city?: string
  page?: number
}

interface UpstreamFailure {
  statusCode?: number
  response?: { headers?: Headers }
}

function cacheKey(request: MarkdownPageRequest): string {
  return [
    request.kind,
    request.slug,
    request.date ?? `today-${todayInParis()}`,
    request.city ?? '',
    request.page ?? 1,
  ].join(':')
}

// Like HTML pages, each agent spends its own server-rendering budget: the
// API keys it on the forwarded visitor address under a global ceiling.
async function render(
  event: H3Event,
  request: MarkdownPageRequest,
): Promise<string> {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')
  // This middleware runs before the request-id one, which records it too.
  const headers = buildInternalApiHeaders(
    config.internalApiSharedSecret,
    requestIdForEvent(event),
    initializeClientIp(event, config.clientIpHeader),
  )
  const siteUrl = config.public.siteUrl
  const slug = encodeURIComponent(request.slug)
  if (request.kind === 'cinema') {
    const response = await $fetch<TheaterShowtimesResponse>(
      `${apiBase}/api/v1/theaters/${slug}/showtimes`,
      {
        headers,
        retry: false,
        query: request.date ? { date: request.date } : {},
      },
    )
    return renderCinemaMarkdown(siteUrl, response)
  }
  if (request.kind === 'ville') {
    const detail = await $fetch<CityDetailResponse>(
      `${apiBase}/api/v1/cities/${slug}`,
      { headers, retry: false },
    )
    const today = todayInParis()
    const dates = cityAvailableDates(detail.theaters, today)
    const date = resolveCityDate(request.date, dates, today)
    const pageCount = cityDayPageCount(detail.theaters.length)
    const page = Math.min(request.page ?? 1, pageCount)
    const theaters = cityDayTheaters(orderCityTheaters(detail), page)
    const timeline = theaters.length
      ? await $fetch<TimelineResponse>(`${apiBase}/api/v1/timeline`, {
          headers,
          retry: false,
          query: {
            date,
            theaters: theaters.map((theater) => theater.id).join(','),
          },
        })
      : null
    return renderCityMarkdown(siteUrl, detail, timeline, {
      date,
      dates,
      page,
      pageCount,
    })
  }
  const fetchSchedule = (date: string) => {
    const query: MovieShowtimesQuery = { date }
    if (request.city) query.city = request.city
    if (request.page) query.page = request.page
    return $fetch<MovieShowtimesResponse>(
      `${apiBase}/api/v1/movies/${slug}/showtimes`,
      { headers, retry: false, query },
    )
  }
  const today = todayInParis()
  let schedule = await fetchSchedule(request.date ?? today)
  const firstDate = schedule.available_dates[0]
  // Like the HTML page, an undated request opens on the next screening day.
  if (
    !request.date &&
    firstDate &&
    firstDate !== schedule.date &&
    !schedule.available_dates.includes(today)
  )
    schedule = await fetchSchedule(firstDate)
  return renderFilmMarkdown(siteUrl, schedule, request)
}

const renderCached = cachedFunction(render, {
  name: 'markdown-pages',
  maxAge: CACHE_SECONDS,
  swr: true,
  getKey: (_event: H3Event, request: MarkdownPageRequest) => cacheKey(request),
})

function sendText(event: H3Event, status: number, body: string): string {
  setResponseStatus(event, status)
  setResponseHeader(event, 'Content-Type', MARKDOWN_CONTENT_TYPE)
  setResponseHeader(event, 'Cache-Control', 'no-store')
  return body
}

export default defineEventHandler(async (event) => {
  const url = getRequestURL(event)
  let request: MarkdownPageRequest | null
  try {
    request = parseMarkdownPageRequest(url.pathname, url.searchParams)
  } catch (error) {
    if (error instanceof MarkdownQueryError)
      return sendText(event, 400, `# Requête invalide\n\n${error.message}\n`)
    throw error
  }
  if (!request) return
  if (event.method !== 'GET' && event.method !== 'HEAD')
    return sendText(event, 405, '# Méthode non autorisée\n')

  try {
    const body = await renderCached(event, request)
    const config = useRuntimeConfig(event)
    const htmlUrl = absoluteSiteUrl(
      config.public.siteUrl,
      `/${request.kind}/${encodeURIComponent(request.slug)}`,
    )
    setResponseHeader(event, 'Content-Type', MARKDOWN_CONTENT_TYPE)
    setResponseHeader(
      event,
      'Cache-Control',
      `public, max-age=${CACHE_SECONDS}`,
    )
    setResponseHeader(event, 'Link', `<${htmlUrl}>; rel="canonical"`)
    return body
  } catch (error) {
    // SAFETY: ofetch failures expose the upstream status and headers; anything else falls through to 503.
    const failure = Object(error) as UpstreamFailure
    if (failure.statusCode === 404)
      return sendText(
        event,
        404,
        {
          film: '# Film introuvable\n',
          cinema: '# Cinéma introuvable\n',
          ville: '# Ville introuvable\n',
        }[request.kind],
      )
    if (failure.statusCode === 400)
      return sendText(event, 400, '# Requête invalide\n')
    if (failure.statusCode === 429) {
      const retryAfter = failure.response?.headers?.get('retry-after')
      if (retryAfter && /^\d{1,5}$/.test(retryAfter))
        setResponseHeader(event, 'Retry-After', retryAfter)
      return sendText(
        event,
        429,
        '# Trop de requêtes\n\nRéessayez plus tard.\n',
      )
    }
    return sendText(event, 503, '# Horaires momentanément indisponibles\n')
  }
})
