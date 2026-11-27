import type {
  NotificationRuleDraft,
  NotificationRules,
} from '~/types/notifications'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

// Page-local alert configuration. Uncertain writes are reconciled with a read,
// never replayed automatically.
export function useNotificationRules() {
  const account = useAccountSession()
  const api = useAccountApi()
  const snapshot = ref<NotificationRules | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const error = ref('')
  const actionError = ref('')
  let request = 0

  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const rules = computed(() =>
    snapshot.value?.username === owner.value ? snapshot.value.rules : [],
  )
  const atLimit = computed(
    () => !!snapshot.value && rules.value.length >= snapshot.value.max_rules,
  )

  function unauthorized(cause: unknown) {
    return (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    )
  }

  async function load() {
    const expectedOwner = owner.value
    if (!import.meta.client || !expectedOwner) return
    const current = ++request
    loading.value = true
    error.value = ''
    try {
      const value = await api.notificationRules()
      if (current !== request || expectedOwner !== owner.value) return
      if (value.username !== expectedOwner) {
        account.clear()
        return
      }
      snapshot.value = value
    } catch (cause) {
      if (current !== request || expectedOwner !== owner.value) return
      if (unauthorized(cause)) account.clear()
      else error.value = accountErrorMessage(cause)
    } finally {
      if (current === request) loading.value = false
    }
  }

  async function write(
    run: (username: string) => Promise<NotificationRules>,
  ): Promise<boolean> {
    const expectedOwner = owner.value
    if (!expectedOwner || saving.value || account.writesBlocked.value)
      return false
    saving.value = true
    actionError.value = ''
    const current = ++request
    try {
      const value = await run(expectedOwner)
      if (expectedOwner !== owner.value) return false
      if (value.username !== expectedOwner) {
        account.clear()
        return false
      }
      if (current === request) snapshot.value = value
      return true
    } catch (cause) {
      if (expectedOwner !== owner.value) return false
      if (unauthorized(cause)) {
        account.clear()
        return false
      }
      actionError.value = accountErrorMessage(cause)
      // A lost response may still have committed: show the server's state.
      if (
        cause instanceof AccountApiError &&
        (cause.status === 0 ||
          cause.status >= 500 ||
          cause.code === 'notification_rule_not_found')
      )
        void load()
      return false
    } finally {
      saving.value = false
    }
  }

  function save(rule: NotificationRuleDraft, id?: string) {
    return write((username) =>
      api.saveNotificationRule({ expected_username: username, id, rule }),
    )
  }

  function remove(id: string) {
    return write((username) =>
      api.deleteNotificationRule({ expected_username: username, id }),
    )
  }

  watch(owner, (value, previous) => {
    if (value === previous) return
    snapshot.value = null
    if (value) void load()
  })

  return {
    snapshot: computed(() => snapshot.value),
    rules,
    atLimit,
    loading: readonly(loading),
    saving: readonly(saving),
    error: readonly(error),
    actionError: readonly(actionError),
    load,
    save,
    remove,
  }
}
