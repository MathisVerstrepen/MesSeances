import type {
  AdminPendingMatch,
  AdminPendingMatchesFilter,
  AdminTMDBMetadataRefreshJob,
  AdminTMDBMetadataRefreshSummary
} from '../types/api.ts'

export interface AdminTMDBMetadataRefreshPresentation {
  running: boolean
  summary: AdminTMDBMetadataRefreshSummary | null
  error: string
}

export const ADMIN_TMDB_MATCH_SEARCH_DEBOUNCE_MS = 350

export function adminTMDBMatchedSearch(value: string | undefined): string {
  return value?.trim() ?? ''
}

export function adminTMDBMatchesTab(value: string | undefined): AdminPendingMatchesFilter {
  if (value === 'rejected' || value === 'matched') return value
  return 'unresolved'
}

export function adminReplacementTMDBId(value: string | number | undefined, currentTMDBId: number): number | null {
  const normalized = String(value ?? '').trim()
  if (!/^\d+$/.test(normalized)) return null
  const tmdbId = Number(normalized)
  return Number.isSafeInteger(tmdbId) && tmdbId > 0 && tmdbId !== currentTMDBId ? tmdbId : null
}

export function adminPendingMatchesForFilter<T extends Pick<AdminPendingMatch, 'status'>>(
  items: readonly T[],
  filter: AdminPendingMatchesFilter
): T[] {
  return items.filter((match) => {
    if (filter === 'rejected') return match.status === 'rejected'
    if (filter === 'matched') return match.status === 'matched'
    return match.status !== 'rejected' && match.status !== 'matched'
  })
}

export function adminTMDBMetadataRefreshPresentation(
  job: AdminTMDBMetadataRefreshJob | null
): AdminTMDBMetadataRefreshPresentation {
  if (job?.state === 'running') return { running: true, summary: null, error: '' }
  if (job?.state === 'succeeded') return { running: false, summary: job.summary, error: '' }
  if (job?.state === 'failed') {
    return {
      running: false,
      summary: null,
      error: 'L’actualisation des métadonnées TMDB a échoué. Réessayez plus tard.'
    }
  }
  return { running: false, summary: null, error: '' }
}

export function shouldRefreshAdminTMDBMatchLists(
  observedRunningStartedAt: string | null,
  job: AdminTMDBMetadataRefreshJob | null,
  refreshedStartedAts: ReadonlySet<string>
): boolean {
  return job?.state === 'succeeded'
    && job.started_at === observedRunningStartedAt
    && !refreshedStartedAts.has(job.started_at)
}
