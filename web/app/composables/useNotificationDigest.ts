import type { DigestFrequency, NotificationDigest } from '~/types/notifications'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

// Page-local email digest setting. Writes are never replayed automatically.
export function useNotificationDigest() {
  const account = useAccountSession()
  const api = useAccountApi()
  const snapshot = ref<NotificationDigest | null>(null)
  const saving = ref(false)
  const error = ref('')
  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const digest = computed(() =>
    snapshot.value?.username === owner.value ? snapshot.value : null,
  )

  function fail(cause: unknown) {
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    )
      account.clear()
    else error.value = accountErrorMessage(cause)
  }

  async function load() {
    const expectedOwner = owner.value
    if (!import.meta.client || !expectedOwner) return
    error.value = ''
    try {
      const value = await api.notificationDigest()
      if (expectedOwner === owner.value) snapshot.value = value
    } catch (cause) {
      if (expectedOwner === owner.value) fail(cause)
    }
  }

  async function save(frequency: DigestFrequency) {
    const expectedOwner = owner.value
    if (!expectedOwner || saving.value || account.writesBlocked.value) return
    saving.value = true
    error.value = ''
    try {
      const value = await api.saveNotificationDigest({
        expected_username: expectedOwner,
        frequency,
      })
      if (expectedOwner === owner.value) snapshot.value = value
    } catch (cause) {
      if (expectedOwner !== owner.value) return
      fail(cause)
      void load()
    } finally {
      saving.value = false
    }
  }

  watch(owner, (value, previous) => {
    if (value === previous) return
    snapshot.value = null
    if (value) void load()
  })

  return {
    digest,
    saving: readonly(saving),
    error: readonly(error),
    load,
    save,
  }
}
