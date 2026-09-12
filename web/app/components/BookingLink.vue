<script setup lang="ts">
import type { VNode } from 'vue'
import type { Provider } from '~/types/api'
import { safeBookingUrl } from '~/utils/bookingUrl'

defineOptions({ inheritAttrs: false })

const props = defineProps<{
  url?: string | null
  provider?: Provider | null
  ariaLabel?: string
  availableClass?: string
  unavailableClass?: string
  unstyled?: boolean
}>()

defineSlots<{
  default?: (props: { available: boolean; kind: 'booking' | 'website' | null; label: string }) => VNode[]
}>()

const reservation = computed(() => {
  const booking = safeBookingUrl(props.url, props.provider)
  if (!booking) return null

  const labels = {
    ugc: 'Réserver sur UGC.fr',
    kinepolis: 'Réserver sur Kinepolis.fr',
    pathe: 'Réserver sur Pathé.fr',
    cgr: 'Réserver sur CGR Cinémas',
    megarama: 'Réserver sur Megarama'
  } satisfies Record<Provider, string>
  return {
    url: booking.url,
    kind: booking.kind,
    label: booking.kind === 'website' ? 'Site du cinéma Megarama' : labels[booking.provider]
  }
})
</script>

<template>
  <a
    v-if="reservation"
    v-bind="$attrs"
    :href="reservation.url"
    target="_blank"
    rel="noopener noreferrer"
    :class="[unstyled ? '' : 'button-primary', availableClass]"
    :aria-label="`${reservation.kind === 'website' ? reservation.label : ariaLabel || reservation.label}, ouverture dans un nouvel onglet`"
  >
    <slot :available="true" :kind="reservation.kind" :label="reservation.label"><BrandedText :text="reservation.label" decorative /></slot>
  </a>
  <span v-else v-bind="$attrs" :class="[unstyled ? '' : 'inline-flex h-10 items-center text-sm font-medium text-muted', unavailableClass]" aria-disabled="true">
    <slot :available="false" :kind="null" label="Réservation indisponible">Réservation indisponible</slot>
  </span>
</template>
