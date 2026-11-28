import type { H3Event } from 'h3'
import type {
  MovieShowtimesResponse,
  TheaterShowtimesResponse,
} from '../../app/types/api'
import { todayInParis } from '../../app/utils/date'
import { absoluteSiteUrl } from '../../app/utils/siteUrl'
import {
  buildInternalApiHeaders,
  requestIdForEvent,
} from '../utils/internalApi'
import {
  MARKDOWN_CONTENT_TYPE,
  MarkdownQueryError,
  parseMarkdownPageRequest,
  renderCinemaMarkdown,
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

// Agent traffic uses the public API budget, not the internal one that
// serves HTML pages to visitors.
async function render(
  event: H3Event,
  request: MarkdownPageRequest,
): Promise<string> {
  const config = useRuntimeConfig(event)
  const apiBase = config.apiBase.replace(/\/$/, '')
  const headers = buildInternalApiHeaders('', requestIdForEvent(event))
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
        request.kind === 'film'
          ? '# Film introuvable\n'
          : '# Cinéma introuvable\n',
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
