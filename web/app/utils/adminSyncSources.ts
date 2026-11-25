import type { AdminSyncSource, Provider } from '../types/api'

export type AdminSyncSourceStatusFilter = 'all' | 'enabled' | 'disabled'

export function adminSyncSourceProviderLabel(provider: Provider): string {
  switch (provider) {
    case 'ticketingcine':
      return 'TicketingCiné'
    case 'scare':
      return 'SCARE (open data)'
    case 'webedia':
      return 'Webedia Movies Pro'
    case 'cotecine':
      return 'Cotecine (Cinegroup)'
    default:
      return provider
  }
}

export interface AdminSyncSourceGroup {
  provider: Provider
  label: string
  total: number
  enabled: number
  sources: AdminSyncSource[]
}

function normalized(value: string): string {
  return value
    .normalize('NFD')
    .replace(/\p{M}/gu, '')
    .toLocaleLowerCase('fr-FR')
}

/** Groups sources by provider, counting before search and status filters. */
export function groupAdminSyncSources(
  sources: readonly AdminSyncSource[],
  search: string,
  status: AdminSyncSourceStatusFilter,
): AdminSyncSourceGroup[] {
  const query = normalized(search.trim())
  const groups = new Map<Provider, AdminSyncSourceGroup>()
  for (const source of sources) {
    let group = groups.get(source.provider)
    if (!group) {
      group = {
        provider: source.provider,
        label: adminSyncSourceProviderLabel(source.provider),
        total: 0,
        enabled: 0,
        sources: [],
      }
      groups.set(source.provider, group)
    }
    group.total++
    if (source.enabled) group.enabled++
    if (status === 'enabled' && !source.enabled) continue
    if (status === 'disabled' && source.enabled) continue
    if (
      query &&
      !normalized(
        `${source.name} ${source.key} ${source.cnc_id ?? ''}`,
      ).includes(query)
    )
      continue
    group.sources.push(source)
  }
  return [...groups.values()]
}
