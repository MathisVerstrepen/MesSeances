import type { Provider } from '../types/api.ts'

// Cineville never publishes ends, even with runtime metadata. Megarama can acquire a known end.
export function hasKnownShowtimeEnd(provider: Provider, startTime: string, endTime: string): boolean {
  if (provider === 'cineville') return false
  if (provider !== 'megarama') return true
  const start = Date.parse(startTime)
  const end = Date.parse(endTime)
  return Number.isFinite(start) && Number.isFinite(end) && end > start
}
