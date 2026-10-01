import { computed, ref } from 'vue'
import type {
  AdminAccountItem,
  AdminAccountState,
  AdminAccountsQuery,
  AdminAccountsResponse,
} from '../types/api.ts'
import {
  getApiErrorCode,
  getApiErrorStatus,
  getFrenchAdminApiError,
} from './useMesSeancesApi.ts'

export const ADMIN_ACCOUNTS_PAGE_SIZE = 50

export const adminAccountStateLabels = {
  complete: 'Complet',
  pending_email: 'E-mail à vérifier',
  pending_username: 'Pseudo à choisir',
  expired: 'Inscription expirée',
} satisfies Record<AdminAccountState, string>

const dateFormatter = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Europe/Paris',
})

export function formatAdminAccountDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime())
    ? 'Date indisponible'
    : dateFormatter.format(date)
}

export function adminAccountMethodLabels(
  account: Pick<AdminAccountItem, 'has_password' | 'google_linked'>,
): string {
  const methods = []
  if (account.has_password) methods.push('Mot de passe')
  if (account.google_linked) methods.push('Google')
  return methods.length ? methods.join(', ') : 'Aucune'
}

interface AdminAccountsApi {
  adminAccounts: (
    query: AdminAccountsQuery,
    signal?: AbortSignal,
  ) => Promise<AdminAccountsResponse>
}

export function useAdminAccounts(api: AdminAccountsApi) {
  const items = ref<AdminAccountItem[]>([])
  const total = ref(0)
  const page = ref(1)
  const loading = ref(true)
  const loaded = ref(false)
  const error = ref('')
  const pageCount = computed(() =>
    Math.max(1, Math.ceil(total.value / ADMIN_ACCOUNTS_PAGE_SIZE)),
  )
  let request: AbortController | undefined
  let disposed = false

  function clear() {
    items.value = []
    total.value = 0
    loaded.value = false
    error.value = ''
  }

  async function load(nextPage = page.value) {
    if (disposed) return
    request?.abort()
    clear()
    loading.value = true
    page.value = nextPage
    const controller = new AbortController()
    request = controller
    try {
      const result = await api.adminAccounts(
        {
          limit: ADMIN_ACCOUNTS_PAGE_SIZE,
          offset: (nextPage - 1) * ADMIN_ACCOUNTS_PAGE_SIZE,
        },
        controller.signal,
      )
      if (disposed || controller.signal.aborted || request !== controller)
        return
      const lastPage = Math.max(
        1,
        Math.ceil(result.total / ADMIN_ACCOUNTS_PAGE_SIZE),
      )
      if (nextPage > lastPage) {
        await load(lastPage)
        return
      }
      items.value = result.items
      total.value = result.total
      loaded.value = true
    } catch (cause) {
      if (disposed || controller.signal.aborted || request !== controller)
        return
      error.value =
        getApiErrorStatus(cause) === 401
          ? 'Session expirée. Reconnectez-vous à l’administration.'
          : getApiErrorCode(cause) === 'admin_accounts_unavailable' ||
              getApiErrorCode(cause) === 'admin_unavailable'
            ? getFrenchAdminApiError(cause)
            : 'Impossible de charger les comptes : le service n’a pas répondu correctement. Réessayez.'
    } finally {
      if (!disposed && !controller.signal.aborted && request === controller)
        loading.value = false
    }
  }

  function changePage(nextPage: number) {
    if (
      loading.value ||
      !loaded.value ||
      !Number.isInteger(nextPage) ||
      nextPage < 1 ||
      nextPage > pageCount.value ||
      nextPage === page.value
    )
      return
    return load(nextPage)
  }

  function dispose() {
    disposed = true
    request?.abort()
    request = undefined
    clear()
    page.value = 1
    loading.value = false
  }

  return {
    items,
    total,
    page,
    pageCount,
    loading,
    loaded,
    error,
    load,
    changePage,
    dispose,
  }
}
