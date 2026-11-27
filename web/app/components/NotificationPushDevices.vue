<script setup lang="ts">
import { BellRing, LoaderCircle, Send, Trash2 } from '@lucide/vue'
import { formatNotificationTime } from '~/utils/notifications'

const props = defineProps<{
  state: ReturnType<typeof useWebPush>
}>()
const push = props.state
const account = useAccountSession()
const now = new Date()
const disabled = computed(
  () => account.writesBlocked.value || !!push.busy.value,
)
</script>

<template>
  <section
    v-if="push.available.value"
    aria-labelledby="push-devices-heading"
    class="space-y-4 border-t-2 border-ink pt-6"
    :aria-busy="push.loading.value || !!push.busy.value"
  >
    <h2 id="push-devices-heading" class="account-heading">
      Notifications push
    </h2>
    <p class="max-w-prose text-sm leading-relaxed">
      Recevez les alertes marquées « Push » même quand MesSeances est fermé.
      L’activation se fait séparément sur chaque appareil.
    </p>

    <p v-if="push.support.value === 'install-required'" role="status">
      Sur iPhone et iPad, ajoutez d’abord MesSeances à l’écran d’accueil
      (Partager, puis « Sur l’écran d’accueil »), ouvrez l’application depuis
      cette icône et revenez ici.
    </p>
    <p v-else-if="push.support.value === 'unsupported'" role="status">
      Ce navigateur ne prend pas en charge les notifications push.
    </p>
    <template v-else>
      <p
        v-if="push.permission.value === 'denied' && !push.subscribed.value"
        role="status"
      >
        Les notifications sont bloquées pour ce site. Autorisez-les dans les
        réglages du navigateur, puis réessayez.
      </p>
      <div class="flex flex-wrap items-center gap-3">
        <button
          v-if="!push.subscribed.value"
          type="button"
          class="account-primary"
          :disabled="disabled || push.loading.value"
          @click="push.enable()"
        >
          <LoaderCircle
            v-if="push.busy.value === 'enable'"
            :size="17"
            class="motion-safe:animate-spin"
            aria-hidden="true"
          />
          <BellRing v-else :size="17" aria-hidden="true" />
          Activer sur cet appareil
        </button>
        <template v-else>
          <p class="text-sm font-semibold">Activées sur cet appareil.</p>
          <button
            type="button"
            class="account-secondary"
            :disabled="disabled"
            @click="push.disableThisDevice()"
          >
            Désactiver sur cet appareil
          </button>
        </template>
      </div>
    </template>

    <p v-if="push.notice.value" role="status">{{ push.notice.value }}</p>
    <p v-if="push.error.value" role="alert" class="account-alert">
      {{ push.error.value }}
    </p>

    <ul
      v-if="push.devices.value.length"
      aria-label="Appareils"
      class="divide-y divide-ink/15 border-y-2 border-ink"
    >
      <li
        v-for="device in push.devices.value"
        :key="device.id"
        class="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="min-w-0">
          <p class="font-semibold [overflow-wrap:anywhere]">
            {{ device.label }}
            <span
              v-if="device.id === push.currentId.value"
              class="ml-1 border-2 border-ink bg-highlight px-1.5 font-mono text-[0.65rem] font-bold uppercase"
              >Cet appareil</span
            >
          </p>
          <p class="text-xs text-muted">
            {{
              device.last_success_at
                ? `Dernier envoi ${formatNotificationTime(device.last_success_at, now)}`
                : `Ajouté ${formatNotificationTime(device.created_at, now)}`
            }}
          </p>
        </div>
        <div class="flex shrink-0 gap-2">
          <button
            type="button"
            class="inline-flex min-h-11 items-center gap-2 border-2 border-ink px-3 text-sm font-semibold hover:bg-highlight disabled:opacity-50"
            :disabled="disabled"
            @click="push.test(device.id)"
          >
            <Send :size="15" aria-hidden="true" />Tester
          </button>
          <button
            type="button"
            class="grid size-11 place-items-center border-2 border-ink hover:bg-primary-soft disabled:opacity-50"
            :aria-label="`Retirer ${device.label}`"
            :disabled="disabled"
            @click="push.remove(device.id)"
          >
            <Trash2 :size="17" aria-hidden="true" />
          </button>
        </div>
      </li>
    </ul>
  </section>
</template>
