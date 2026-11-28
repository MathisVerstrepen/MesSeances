import type { H3Event } from 'h3'

// Seconds a client should wait when the API gave no usable Retry-After.
export const DEFAULT_RETRY_AFTER_SECONDS = 30
const MAX_RETRY_AFTER_SECONDS = 3600
const CONTEXT_KEY = 'messeancesUpstreamRetryAfter'

export interface RenderedResponse {
  statusCode?: number
  statusMessage?: string
  headers?: Record<string, string>
}

// A failed API call means "come back later" for 429, 5xx and network
// failures (no status). Other statuses describe the request itself.
export function upstreamRetryAfterSeconds(
  status: number | undefined,
  retryAfter: string | null | undefined,
): number | null {
  if (status !== undefined && status !== 429 && status < 500) return null
  const seconds = /^\d{1,5}$/.test(retryAfter ?? '')
    ? Number(retryAfter)
    : DEFAULT_RETRY_AFTER_SECONDS
  return Math.min(Math.max(seconds, 1), MAX_RETRY_AFTER_SECONDS)
}

export function recordUpstreamFailure(
  event: H3Event,
  status: number | undefined,
  retryAfter: string | null | undefined,
): void {
  const seconds = upstreamRetryAfterSeconds(status, retryAfter)
  if (seconds === null) return
  event.context[CONTEXT_KEY] = Math.max(
    seconds,
    upstreamFailureForEvent(event) ?? 0,
  )
}

export function upstreamFailureForEvent(event: H3Event): number | null {
  const value = Number(event.context[CONTEXT_KEY])
  return Number.isInteger(value) && value > 0 ? value : null
}

// Pages still render their friendly error, but crawlers and caches must see
// a temporary failure instead of a successful page without showtimes.
export function markResponseUnavailable(
  response: RenderedResponse,
  currentStatus: number,
  retryAfterSeconds: number,
): boolean {
  if (currentStatus >= 300 && currentStatus < 500) return false
  const headers = Object.fromEntries(
    Object.entries(response.headers ?? {}).filter(
      ([name]) =>
        !['cache-control', 'retry-after'].includes(name.toLowerCase()),
    ),
  )
  response.statusCode = 503
  response.statusMessage = 'Service Unavailable'
  response.headers = {
    ...headers,
    'Cache-Control': 'no-store',
    'Retry-After': String(retryAfterSeconds),
  }
  return true
}
