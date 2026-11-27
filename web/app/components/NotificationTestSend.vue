<script setup lang="ts">
import { BellRing, Mail } from '@lucide/vue'
import type { NotificationTestChannel } from '~/types/notifications'
import { accountErrorMessage } from '~/utils/accountState'
import { notificationTestMessage } from '~/utils/notificationTest'

const props = defineProps<{
  push: ReturnType<typeof useWebPush>
  digest: ReturnType<typeof useNotificationDigest>
}>()
const account = useAccountSession()
const api = useAccountApi()
const pending = ref<NotificationTestChannel | null>(null)
const result = ref<{
  channel: NotificationTestChannel
  ok: boolean
  text: string
} | null>(null)

const email = computed(() => account.session.value?.account?.email ?? '')
const pushAvailable = computed(() => props.push.available.value)
const deviceCount = computed(() => props.push.devices.value.length)
const emailAvailable = computed(() => !!props.digest.digest.value?.available)
const blocked = computed(() => account.writesBlocked.value || !!pending.value)

async function send(channel: NotificationTestChannel) {
  const owner = account.session.value?.account?.username
  if (!owner || blocked.value) return
  pending.value = channel
  result.value = null
  try {
    const value = await api.sendTestNotification({
      expected_username: owner,
      channel,
    })
    result.value = {
      channel,
      ok: true,
      text: notificationTestMessage(value, email.value),
    }
    if (value.expired) void props.push.load()
  } catch (cause) {
    result.value = { channel, ok: false, text: accountErrorMessage(cause) }
    if (channel === 'push') void props.push.load()
  } finally {
    pending.value = null
  }
}
</script>

<template>
  <section
    v-if="pushAvailable || emailAvailable"
    aria-labelledby="notification-test-heading"
    class="space-y-4 border-t-2 border-ink pt-6"
    :aria-busy="!!pending"
  >
    <h2 id="notification-test-heading" class="account-heading">
      Tester les notifications
    </h2>
    <p class="max-w-prose text-sm leading-relaxed">
      Envoyez-vous un message de test pour vérifier que tout arrive bien.
    </p>
    <ul class="divide-y divide-ink/15 border-y-2 border-ink">
      <li
        v-if="pushAvailable"
        class="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="flex min-w-0 items-start gap-3">
          <BellRing :size="20" class="mt-0.5 shrink-0" aria-hidden="true" />
          <div>
            <p class="font-semibold">Notification navigateur</p>
            <p class="text-sm text-muted">
              {{
                deviceCount
                  ? `Sur ${deviceCount === 1 ? 'votre appareil enregistré' : `vos ${deviceCount} appareils enregistrés`}.`
                  : 'Activez d’abord les notifications sur un appareil, plus haut.'
              }}
            </p>
          </div>
        </div>
        <button
          type="button"
          class="account-secondary shrink-0"
          :disabled="blocked || !deviceCount"
          @click="send('push')"
        >
          {{ pending === 'push' ? 'Envoi…' : 'Envoyer un test' }}
        </button>
      </li>
      <li
        v-if="emailAvailable"
        class="flex flex-col gap-3 py-4 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="flex min-w-0 items-start gap-3">
          <Mail :size="20" class="mt-0.5 shrink-0" aria-hidden="true" />
          <div class="min-w-0">
            <p class="font-semibold">E-mail</p>
            <p class="text-sm text-muted [overflow-wrap:anywhere]">
              Un aperçu du résumé avec vos dernières notifications{{
                email ? `, envoyé à ${email}` : ''
              }}.
            </p>
          </div>
        </div>
        <button
          type="button"
          class="account-secondary shrink-0"
          :disabled="blocked"
          @click="send('email')"
        >
          {{ pending === 'email' ? 'Envoi…' : 'Envoyer un test' }}
        </button>
      </li>
    </ul>
    <p
      v-if="result"
      :role="result.ok ? 'status' : 'alert'"
      :class="result.ok ? undefined : 'account-alert'"
    >
      {{ result.text }}
    </p>
  </section>
</template>
