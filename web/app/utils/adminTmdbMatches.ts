import type { AdminPendingMatch, AdminPendingMatchesFilter } from '../types/api.ts'

export function adminPendingMatchesForFilter<T extends Pick<AdminPendingMatch, 'status'>>(
  items: readonly T[],
  filter: AdminPendingMatchesFilter
): T[] {
  return items.filter(match => filter === 'rejected' ? match.status === 'rejected' : match.status !== 'rejected')
}
