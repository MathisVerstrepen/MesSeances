import type { PushDevices } from '~/types/notifications'
import { AccountApiError, accountErrorMessage } from '~/utils/accountState'
import {
  base64UrlToBytes,
  bytesToBase64Url,
  pushDeviceLabel,
  pushSupport,
  type PushSupport,
} from '~/utils/webPush'

type Permission = NotificationPermission | 'unknown'

async function readyRegistration(timeout = 4000) {
  if (!('serviceWorker' in navigator)) return undefined
  const registration = await navigator.serviceWorker.getRegistration()
  if (registration?.active) return registration
  return Promise.race([
    navigator.serviceWorker.ready,
    new Promise<undefined>((resolve) =>
      setTimeout(() => resolve(undefined), timeout),
    ),
  ])
}

function environment(): PushSupport {
  return pushSupport({
    serviceWorker: 'serviceWorker' in navigator,
    pushManager: 'PushManager' in window,
    notification: 'Notification' in window,
    userAgent: navigator.userAgent,
    maxTouchPoints: navigator.maxTouchPoints,
    // iOS exposes the non-standard `navigator.standalone` for home screen apps.
    standalone:
      ('standalone' in navigator && navigator.standalone === true) ||
      window.matchMedia('(display-mode: standalone)').matches,
  })
}

// Best effort before logout: this browser stops receiving the departing
// account's notifications. Never blocks logout for long.
export async function forgetPushDevice(
  api: ReturnType<typeof useAccountApi>,
  username: string,
) {
  if (!import.meta.client || !username || !('serviceWorker' in navigator))
    return
  const work = (async () => {
    const registration = await navigator.serviceWorker.getRegistration()
    const subscription = await registration?.pushManager?.getSubscription()
    if (!subscription) return
    const endpoint = subscription.endpoint
    await subscription.unsubscribe().catch(() => false)
    await api
      .deletePushDevice({ expected_username: username, endpoint })
      .catch(() => undefined)
  })()
  await Promise.race([
    work.catch(() => undefined),
    new Promise((resolve) => setTimeout(resolve, 2500)),
  ])
}

// Page-local device management for browser push.
export function useWebPush() {
  const account = useAccountSession()
  const api = useAccountApi()
  const snapshot = ref<PushDevices | null>(null)
  const support = ref<PushSupport>('unsupported')
  const permission = ref<Permission>('unknown')
  const subscribed = ref(false)
  const currentId = ref<string | null>(null)
  const loading = ref(false)
  const busy = ref<string | null>(null)
  const error = ref('')
  const notice = ref('')

  const owner = computed(() =>
    account.status.value === 'ready' &&
    account.session.value?.state === 'complete'
      ? (account.session.value.account?.username ?? '')
      : '',
  )
  const available = computed(
    () =>
      !!snapshot.value?.available && snapshot.value.username === owner.value,
  )
  const devices = computed(() =>
    snapshot.value?.username === owner.value ? snapshot.value.devices : [],
  )

  function accept(value: PushDevices, expectedOwner: string) {
    if (value.username !== expectedOwner) {
      account.clear()
      return false
    }
    snapshot.value = value
    if (value.current_id) currentId.value = value.current_id
    else if (!value.devices.some((device) => device.id === currentId.value))
      currentId.value = null
    return true
  }

  function fail(cause: unknown) {
    if (
      cause instanceof AccountApiError &&
      (cause.status === 401 || cause.status === 403)
    )
      account.clear()
    else error.value = accountErrorMessage(cause)
  }

  async function register(
    subscription: PushSubscription,
    expectedOwner: string,
  ) {
    const value = await api.savePushSubscription({
      expected_username: expectedOwner,
      endpoint: subscription.endpoint,
      p256dh: bytesToBase64Url(subscription.getKey('p256dh')),
      auth: bytesToBase64Url(subscription.getKey('auth')),
      label: pushDeviceLabel(navigator.userAgent, navigator.maxTouchPoints),
    })
    if (expectedOwner === owner.value && accept(value, expectedOwner))
      subscribed.value = true
  }

  async function forgetLocalSubscription() {
    const registration = await readyRegistration(1500).catch(() => undefined)
    const subscription = await registration?.pushManager
      .getSubscription()
      .catch(() => null)
    await subscription?.unsubscribe().catch(() => false)
    subscribed.value = false
    currentId.value = null
  }

  // Refresh the list without reconciling or clearing the visible message.
  async function refreshDevices(expectedOwner: string) {
    try {
      const value = await api.pushDevices()
      if (expectedOwner === owner.value) accept(value, expectedOwner)
    } catch {
      // Keep the error already shown for the original action.
    }
  }

  async function load() {
    const expectedOwner = owner.value
    if (!import.meta.client || !expectedOwner) return
    support.value = environment()
    permission.value =
      'Notification' in window ? Notification.permission : 'unknown'
    loading.value = true
    error.value = ''
    try {
      const value = await api.pushDevices()
      if (expectedOwner !== owner.value || !accept(value, expectedOwner)) return
      if (!value.available || support.value !== 'supported') return
      const registration = await readyRegistration(1500)
      const subscription = await registration?.pushManager.getSubscription()
      subscribed.value = !!subscription
      // Reconcile this browser with the server: heals lost rows, moves the
      // device after an account switch and identifies "this device".
      if (subscription && permission.value === 'granted')
        await register(subscription, expectedOwner)
    } catch (cause) {
      if (expectedOwner === owner.value) fail(cause)
    } finally {
      loading.value = false
    }
  }

  async function enable() {
    const expectedOwner = owner.value
    const key = snapshot.value?.public_key
    if (!expectedOwner || !key || busy.value) return
    busy.value = 'enable'
    error.value = ''
    notice.value = ''
    try {
      // Ask only from this explicit click.
      permission.value = await Notification.requestPermission()
      if (permission.value !== 'granted') {
        error.value =
          permission.value === 'denied'
            ? 'Les notifications sont bloquées pour ce site. Autorisez-les dans les réglages du navigateur.'
            : 'Autorisation non accordée.'
        return
      }
      const registration = await readyRegistration()
      if (!registration) {
        error.value =
          'Le service de l’application n’est pas prêt. Rechargez la page puis réessayez.'
        return
      }
      let subscription = await registration.pushManager.getSubscription()
      if (
        subscription &&
        bytesToBase64Url(subscription.options.applicationServerKey) !== key
      ) {
        await subscription.unsubscribe()
        subscription = null
      }
      subscription ??= await registration.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: base64UrlToBytes(key),
      })
      await register(subscription, expectedOwner)
      notice.value = 'Notifications activées sur cet appareil.'
    } catch (cause) {
      if (expectedOwner === owner.value) fail(cause)
    } finally {
      busy.value = null
    }
  }

  async function disableThisDevice() {
    const expectedOwner = owner.value
    if (!expectedOwner || busy.value) return
    busy.value = 'disable'
    error.value = ''
    notice.value = ''
    try {
      const registration = await readyRegistration(1500)
      const subscription = await registration?.pushManager.getSubscription()
      if (subscription) {
        const endpoint = subscription.endpoint
        await subscription.unsubscribe()
        const value = await api.deletePushDevice({
          expected_username: expectedOwner,
          endpoint,
        })
        if (expectedOwner === owner.value) accept(value, expectedOwner)
      }
      subscribed.value = false
      currentId.value = null
    } catch (cause) {
      if (expectedOwner === owner.value) fail(cause)
    } finally {
      busy.value = null
    }
  }

  async function remove(id: string) {
    if (id === currentId.value) return disableThisDevice()
    const expectedOwner = owner.value
    if (!expectedOwner || busy.value) return
    busy.value = `remove:${id}`
    error.value = ''
    notice.value = ''
    try {
      const value = await api.deletePushDevice({
        expected_username: expectedOwner,
        id,
      })
      if (expectedOwner === owner.value) accept(value, expectedOwner)
    } catch (cause) {
      if (expectedOwner === owner.value) {
        fail(cause)
        await refreshDevices(expectedOwner)
      }
    } finally {
      busy.value = null
    }
  }

  async function test(id: string) {
    const expectedOwner = owner.value
    if (!expectedOwner || busy.value) return
    busy.value = `test:${id}`
    error.value = ''
    notice.value = ''
    try {
      await api.testPushDevice({ expected_username: expectedOwner, id })
      notice.value = 'Notification de test envoyée.'
    } catch (cause) {
      if (expectedOwner !== owner.value) return
      fail(cause)
      if (
        cause instanceof AccountApiError &&
        (cause.code === 'push_device_expired' ||
          cause.code === 'push_device_not_found')
      ) {
        // The server dropped this device. When it is this browser, drop the
        // dead local subscription too instead of silently re-registering it.
        if (id === currentId.value) await forgetLocalSubscription()
        await refreshDevices(expectedOwner)
      }
    } finally {
      busy.value = null
    }
  }

  watch(owner, (value, previous) => {
    if (value === previous) return
    snapshot.value = null
    subscribed.value = false
    currentId.value = null
    if (value) void load()
  })

  return {
    snapshot: computed(() => snapshot.value),
    available,
    devices,
    support: readonly(support),
    permission: readonly(permission),
    subscribed: readonly(subscribed),
    currentId: readonly(currentId),
    loading: readonly(loading),
    busy: readonly(busy),
    error: readonly(error),
    notice: readonly(notice),
    load,
    enable,
    disableThisDevice,
    remove,
    test,
  }
}
