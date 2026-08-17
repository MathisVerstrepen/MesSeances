<script setup lang="ts">
import type { Provider } from '~/types/api'

const props = defineProps<{
  url?: string | null
  provider?: Provider | null
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
    :href="reservation.url"
    target="_blank"
    rel="noopener noreferrer"
    class="button-primary"
    :aria-label="`${reservation.label}, ouverture dans un nouvel onglet`"
  >
    <BrandedText :text="reservation.label" decorative />
  </a>
  <span v-else class="inline-flex h-10 items-center text-sm font-medium text-muted">
    Réservation indisponible
  </span>
</template>
