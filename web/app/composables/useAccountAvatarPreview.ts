import type { Ref } from 'vue'
import { fetchAccountAvatar } from '~/utils/accountAvatar'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'

export function useAccountAvatarPreview(path: Ref<string | null>) {
  const account = useAccountSession()
  const image = ref('')
  const loading = ref(false)
  const error = ref('')
  let request: AbortController | undefined
  let generation = 0
  let mounted = false
  let departed = false
  const owner = computed(() => {
    const session = account.session.value
    return session?.state === 'complete' && session.account
      ? `${session.account.username}:${session.account.email}`
      : ''
  })
  function clear() {
    generation++
    request?.abort()
    request = undefined
    if (image.value) URL.revokeObjectURL(image.value)
    image.value = ''
    loading.value = false
    error.value = ''
  }
  const lifetime = useAccountLifetime(() => {
    departed = true
    clear()
  })
  async function refresh() {
    clear()
    if (!mounted || departed || !owner.value || !path.value) return
    const identity = owner.value
    const version = path.value
    const current = generation
    const active = lifetime.capture(false)
    const controller = new AbortController()
    request = controller
    loading.value = true
    const timer = setTimeout(() => controller.abort(), 12000)
    try {
      const blob = await fetchAccountAvatar(version, controller.signal)
      if (
        !active() ||
        controller.signal.aborted ||
        current !== generation ||
        identity !== owner.value ||
        version !== path.value
      )
        return
      image.value = URL.createObjectURL(blob)
    } catch (cause) {
      if (!active() || current !== generation) return
      error.value = accountErrorMessage(cause)
      if (
        cause instanceof AccountApiError &&
        (cause.status === 401 || cause.code === 'onboarding_required')
      )
        account.clear()
    } finally {
      clearTimeout(timer)
      if (current === generation) loading.value = false
    }
  }
  function failed() {
    clear()
    error.value =
      'L’aperçu ne peut pas être affiché. Réessayez de charger la photo.'
  }
  watch([owner, path], () => void refresh(), { flush: 'sync' })
  // An ambiguous write clears the image before rechecking details. The confirmed
  // revision can remain unchanged, so its path watcher alone cannot restore it.
  watch(account.revalidating, (pending) => {
    if (!pending && !image.value && !loading.value) void refresh()
  })
  onMounted(() => {
    mounted = true
    void refresh()
  })
  return { image, loading, error, refresh, clear, failed }
}
