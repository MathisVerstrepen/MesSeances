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
      : hostname === 'kinepolis.fr' ? 'kinepolis' : null
    if (
      parsed.protocol !== 'https:'
      || !hostProvider
      || (props.provider && props.provider !== hostProvider)
      || parsed.username
      || parsed.password
      || parsed.port
    ) return null

    return {
      url: parsed.href,
      label: hostProvider === 'ugc' ? 'Réserver sur UGC.fr' : 'Réserver sur Kinepolis.fr'
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
