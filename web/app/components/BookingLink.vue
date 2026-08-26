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
  default?: (props: { available: boolean }) => VNode[]
}>()

const reservation = computed(() => {
  const booking = safeBookingUrl(props.url, props.provider)
  if (!booking) return null

  const labels = {
    ugc: 'Réserver sur UGC.fr',
    kinepolis: 'Réserver sur Kinepolis.fr',
    pathe: 'Réserver sur Pathé.fr',
    cgr: 'Réserver sur CGR Cinémas'
  } satisfies Record<Provider, string>
  return {
    url: booking.url,
    label: labels[booking.provider]
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
    :aria-label="`${ariaLabel || reservation.label}, ouverture dans un nouvel onglet`"
  >
    <slot :available="true"><BrandedText :text="reservation.label" decorative /></slot>
  </a>
  <span v-else v-bind="$attrs" :class="[unstyled ? '' : 'inline-flex h-10 items-center text-sm font-medium text-muted', unavailableClass]" aria-disabled="true">
    <slot :available="false">Réservation indisponible</slot>
  </span>
</template>
