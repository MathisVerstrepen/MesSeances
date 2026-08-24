<script setup lang="ts">
import type { VNode } from 'vue'
import type { Provider } from '~/types/api'

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
  const value = props.url?.trim()
  if (!value) return null

  try {
    const parsed = new URL(value)
    const hostname = parsed.hostname.toLowerCase()
    const hostProvider: Provider | null = hostname === 'www.ugc.fr'
      ? 'ugc'
      : hostname === 'kinepolis.fr'
        ? 'kinepolis'
        : hostname === 's.pathe.fr' ? 'pathe' : null
    const isSafePatheBooking = hostProvider !== 'pathe' || (
      !parsed.search
      && !parsed.hash
      && parsed.href === value
      && /^\/fr\/[A-Za-z0-9_-]*S[1-9][0-9]*\/booking$/.test(parsed.pathname)
    )
    if (
      parsed.protocol !== 'https:'
      || !hostProvider
      || (props.provider && props.provider !== hostProvider)
      || parsed.username
      || parsed.password
      || parsed.port
      || !isSafePatheBooking
    ) return null

    const labels = {
      ugc: 'Réserver sur UGC.fr',
      kinepolis: 'Réserver sur Kinepolis.fr',
      pathe: 'Réserver sur Pathé.fr'
    } satisfies Record<Provider, string>
    return {
      url: parsed.href,
      label: labels[hostProvider]
    }
  } catch {
    return null
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
