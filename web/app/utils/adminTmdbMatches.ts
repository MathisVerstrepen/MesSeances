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

export function adminPendingMatchesForFilter<T extends Pick<AdminPendingMatch, 'status'>>(
  items: readonly T[],
  filter: AdminPendingMatchesFilter
): T[] {
  return items.filter(match => filter === 'rejected' ? match.status === 'rejected' : match.status !== 'rejected')
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
