export interface CompleteSearchShareState {
  theaterIds: readonly string[]
  date: string
  startAfter: string
  finishBefore: string
  language: string
  format: string
  includeAds: boolean
  bufferAds: number
  grouping: 'movie' | 'chronological'
  layout: 'lines' | 'boxes'
}

export function buildCompleteSearchShareTarget(search: CompleteSearchShareState): string {
  const query = new URLSearchParams([
    ['theaters', search.theaterIds.join(',')],
    ['date', search.date],
    ['start_after', search.startAfter],
    ['finish_before', search.finishBefore],
    ['language', search.language],
    ['format', search.format],
    ['include_ads', search.includeAds ? '1' : '0'],
    ['buffer_ads', String(search.bufferAds)],
    ['grouping', search.grouping],
    ['layout', search.layout]
  ])

  return `/recherche?${query.toString()}`
}
