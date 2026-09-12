import type { Provider } from '../types/api.ts'

// Only Megarama uses this public sentinel. Effective known ends come from the API.
export function hasKnownShowtimeEnd(provider: Provider, startTime: string, endTime: string): boolean {
  if (provider !== 'megarama') return true
  const start = Date.parse(startTime)
  const end = Date.parse(endTime)
  return Number.isFinite(start) && Number.isFinite(end) && end > start
}
