<script setup lang="ts">
import { Eye } from '@lucide/vue'
const props = defineProps<{ slug?: string }>()
const watched = useWatched()
const shown = computed(
  () =>
    import.meta.client &&
    !!watched.owner.value &&
    !!props.slug &&
    watched.slugs.value.has(props.slug),
)
</script>

<template>
  <span
    v-if="shown"
    class="pointer-events-none absolute top-1 left-1 z-10 inline-flex size-7 items-center justify-center border border-ink bg-surface text-ink shadow-sm"
    role="img"
    aria-label="Film déjà vu"
    title="Film déjà vu"
  >
    <Eye :size="18" aria-hidden="true" />
  </span>
</template>
