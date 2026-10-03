import type { AccountTheaterFollows } from '~/types/cinemaFollows'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

declare module '#app' {
  interface NuxtApp {
    _cinemaFollows?: ReturnType<typeof createCinemaFollows>
  }
}

// App-owned private memory. Never serialized or persisted on this device.
export function useCinemaFollows() {
  const app = useNuxtApp()
  return (app._cinemaFollows ??= createCinemaFollows())
}

function createCinemaFollows() {
  const app = useNuxtApp()
  const account = useAccountSession()
  const api = useAccountApi()
  const snapshot = ref<AccountTheaterFollows | null>(null)
  const error = ref('')
  const loading = ref(false)
  const saving = ref(false)
  const uncertain = ref(false)
  let generation = 0
  let request = 0
  let controller: AbortController | undefined
  let savingPromise: Promise<string> | undefined
  let started = false
  let admissionRevision = account.revision.value
  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.enabled &&
    account.session.value.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const ready = computed(
    () =>
      !!owner.value && !!snapshot.value && !loading.value && !uncertain.value,
  )
  const writesBlocked = computed(
    () => account.writesBlocked.value || !ready.value || saving.value,
  )
  const ids = computed(() => new Set(snapshot.value?.theater_ids ?? []))
  const revision = computed(() => snapshot.value?.revision ?? null)

  function capture() {
    const expectedOwner = owner.value
    const current = generation
    const admitted = account.revision.value
    const inScope = () =>
      !!expectedOwner && expectedOwner === owner.value && current === generation
    return {
      expectedOwner,
      inScope,
      valid: () => inScope() && admitted === account.revision.value,
    }
  }
  function checkOwner(value: AccountTheaterFollows, expected: string) {
    if (value.username !== expected)
      throw new AccountApiError(401, 'authentication_required')
  }
  function apply(value: AccountTheaterFollows) {
    if (
      snapshot.value &&
      BigInt(value.revision) < BigInt(snapshot.value.revision)
    )
      return
    snapshot.value = value
    uncertain.value = false
    error.value = ''
  }
  function fail(cause: unknown) {
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    ) {
      account.clear()
      account.errorMessage.value = accountErrorMessage(cause)
      account.status.value = 'error'
      return
    }
    uncertain.value = true
    error.value = accountErrorMessage(cause)
  }
  function reset() {
    admissionRevision = account.revision.value
    generation++
    request++
    controller?.abort()
    snapshot.value = null
    error.value = ''
    loading.value = false
    saving.value = false
    savingPromise = undefined
    uncertain.value = false
  }
  async function readSnapshot() {
    const token = capture()
    if (!import.meta.client || !token.expectedOwner) return
    const current = ++request
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    try {
      const value = await api.theaterFollows(controller.signal)
      if (!token.valid() || current !== request) return
      checkOwner(value, token.expectedOwner)
      apply(value)
    } catch (cause) {
      if (token.valid() && current === request) fail(cause)
    } finally {
      if (token.inScope() && current === request) loading.value = false
    }
  }
  async function save(theaterId: string, followed: boolean): Promise<string> {
    if (!import.meta.client || writesBlocked.value || !snapshot.value) return ''
    const token = capture()
    const expectedRevision = snapshot.value.revision
    saving.value = true
    request++
    controller?.abort()
    controller = new AbortController()
    savingPromise = (async () => {
      try {
        const value = await api.saveTheaterFollow(
          {
            expected_username: token.expectedOwner,
            expected_revision: expectedRevision,
            theater_id: theaterId,
            followed: followed ? 'true' : 'false',
          },
          controller?.signal,
        )
        if (!token.valid()) return ''
        checkOwner(value, token.expectedOwner)
        apply(value)
        app._accountChannel?.postMessage('theater-follows-changed')
        return ''
      } catch (cause) {
        if (!token.valid()) return ''
        if (
          cause instanceof AccountApiError &&
          (cause.status === 401 || cause.status === 403)
        ) {
          fail(cause)
          return ''
        }
        uncertain.value = true
        // Uncertain commit or stale CAS: reconcile once, never replay POST.
        await readSnapshot()
        return token.valid() ? accountErrorMessage(cause) : ''
      } finally {
        if (token.inScope()) saving.value = false
      }
    })()
    const pending = savingPromise
    try {
      return await pending
    } finally {
      if (savingPromise === pending) savingPromise = undefined
    }
  }
  async function revalidate() {
    const token = capture()
    await savingPromise
    if (!token.valid()) return () => {}
    const current = ++request
    controller?.abort()
    controller = new AbortController()
    try {
      const value = await api.theaterFollows(controller.signal)
      if (token.valid()) checkOwner(value, token.expectedOwner)
      return () => {
        if (token.valid() && current === request) {
          apply(value)
          loading.value = false
        }
      }
    } catch (cause) {
      if (
        token.valid() &&
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.status === 403)
      )
        throw cause
      return () => {
        if (token.valid() && current === request) {
          fail(cause)
          loading.value = false
        }
      }
    }
  }
  function startSynchronization() {
    if (!import.meta.client || started) return
    started = true
    effectScope(true).run(() => {
      account.onRevalidate(revalidate)
      watch([owner, () => account.status.value], reset, { flush: 'sync' })
      watch(account.revision, () => {
        if (admissionRevision === account.revision.value) return
        admissionRevision = account.revision.value
        if (account.revalidating.value) return
        reset()
        if (owner.value) void readSnapshot()
      })
      watch(
        [owner, account.writesBlocked],
        () => {
          if (
            owner.value &&
            !account.writesBlocked.value &&
            !snapshot.value &&
            !loading.value &&
            !uncertain.value
          )
            void readSnapshot()
        },
        { immediate: true },
      )
    })
  }
  async function retry() {
    if (!saving.value && !account.revalidating.value) await account.revalidate()
  }
  return {
    owner,
    ready,
    writesBlocked,
    ids,
    revision,
    error: readonly(error),
    loading: readonly(loading),
    saving: readonly(saving),
    startSynchronization,
    retry,
    save,
  }
}
