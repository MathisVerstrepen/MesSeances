<script setup lang="ts">
const props = defineProps<{
  url?: string | null
}>()

const safeUrl = computed(() => {
  const value = props.url?.trim()
  if (!value) return null

  try {
    const parsed = new URL(value)
    const hostname = parsed.hostname.toLowerCase()
    const isUgcHost = hostname === 'ugc.fr' || hostname.endsWith('.ugc.fr')
    if (parsed.protocol !== 'https:' || !isUgcHost || parsed.username || parsed.password || parsed.port) return null
    return parsed.href
  } catch {
    return null
  }
})
</script>

<template>
  <a
    v-if="safeUrl"
    :href="safeUrl"
    target="_blank"
    rel="noopener noreferrer"
    class="button-primary"
    aria-label="Réserver sur UGC.fr, ouverture dans un nouvel onglet"
  >
    Réserver sur UGC.fr
  </a>
  <span v-else class="inline-flex h-10 items-center text-sm font-medium text-muted">
    Réservation indisponible
  </span>
</template>
