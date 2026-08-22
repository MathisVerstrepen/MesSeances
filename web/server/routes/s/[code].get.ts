import type { ShortLinkResponse } from '../../../app/types/api'
import { isValidShortLinkCode, isValidShortLinkTarget } from '../../../app/utils/shortLinkTarget'

interface UpstreamShortLinkPayload {
  code?: string
  target?: string
}

function upstreamStatus(cause: unknown): number | undefined {
  if (cause === null || Object(cause) !== cause) return undefined
  // SAFETY: Object identity above establishes a non-null object; every field remains optional.
  const failure = cause as { status?: number; statusCode?: number; response?: { status?: number } }
  return failure.response?.status ?? failure.statusCode ?? failure.status
}

function parseShortLinkResponse(response: UpstreamShortLinkPayload, requestedCode: string): ShortLinkResponse | null {
  if (response.code !== requestedCode || Object.prototype.toString.call(response.target) !== '[object String]') return null
  const target = response.target ?? ''
  if (!isValidShortLinkTarget(target)) return null
  return { code: response.code, target }
}

export default defineEventHandler(async (event) => {
  const code = getRouterParam(event, 'code') ?? ''
  if (!isValidShortLinkCode(code)) {
    throw createError({ statusCode: 404, statusMessage: 'Lien introuvable', message: 'Ce lien de partage est introuvable.' })
  }

  const apiBase = useRuntimeConfig(event).apiBase.replace(/\/$/, '')
  let response: unknown
  try {
    response = await $fetch<unknown>(`${apiBase}/api/v1/shortlinks/${encodeURIComponent(code)}`)
  } catch (error) {
    if (upstreamStatus(error) === 404) {
      throw createError({ statusCode: 404, statusMessage: 'Lien introuvable', message: 'Ce lien de partage est introuvable.' })
    }
    throw createError({ statusCode: 503, statusMessage: 'Service indisponible', message: 'Le service de partage est temporairement indisponible.' })
  }

  // SAFETY: Object coercion creates a field-readable candidate; parser validates every used field.
  const payload = Object(response) as UpstreamShortLinkPayload
  const shortLink = parseShortLinkResponse(payload, code)
  if (!shortLink) {
    throw createError({ statusCode: 502, statusMessage: 'Réponse invalide', message: 'Le service de partage a renvoyé une réponse invalide.' })
  }

  setResponseHeader(event, 'Cache-Control', 'public, max-age=31536000, immutable')
  return sendRedirect(event, shortLink.target, 308)
})
