<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'

const props = defineProps<{
  page: number
  totalPages: number
  previousTo: RouteLocationRaw | null
  nextTo: RouteLocationRaw | null
  pending?: boolean
}>()

const emit = defineEmits<{
  navigate: [event: MouseEvent, page: number]
}>()
</script>

<template>
  <nav v-if="totalPages > 1" class="pagination mt-14 flex flex-col items-stretch justify-between gap-4 border-2 border-ink bg-surface p-3 shadow-[6px_6px_0_#27272a] sm:flex-row sm:items-center" aria-label="Pagination des films">
    <span v-if="!previousTo" class="inline-flex min-h-11 cursor-not-allowed items-center justify-center border-2 border-ink bg-[#ffcf3f] px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] opacity-55 transition-[background-color,color] duration-150 motion-reduce:transition-none" aria-disabled="true">← Précédent</span>
    <NuxtLink v-else :to="previousTo" class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-[#ffcf3f] px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] transition-[background-color,color] duration-150 not-aria-disabled:hover:bg-ink not-aria-disabled:hover:text-white aria-disabled:cursor-not-allowed aria-disabled:opacity-55 motion-reduce:transition-none" :aria-disabled="pending || undefined" @click="emit('navigate', $event, page - 1)">← Précédent</NuxtLink>
    <span class="order-first text-center font-mono text-[11px] font-bold uppercase tracking-[0.14em] sm:order-none" aria-live="polite">Page {{ page }} / {{ totalPages }}</span>
    <span v-if="!nextTo" class="inline-flex min-h-11 cursor-not-allowed items-center justify-center border-2 border-ink bg-[#ffcf3f] px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] opacity-55 transition-[background-color,color] duration-150 motion-reduce:transition-none" aria-disabled="true">Suivant →</span>
    <NuxtLink v-else :to="nextTo" class="inline-flex min-h-11 items-center justify-center border-2 border-ink bg-[#ffcf3f] px-4 py-[0.65rem] font-mono text-[0.68rem] font-black uppercase tracking-[0.08em] transition-[background-color,color] duration-150 not-aria-disabled:hover:bg-ink not-aria-disabled:hover:text-white aria-disabled:cursor-not-allowed aria-disabled:opacity-55 motion-reduce:transition-none" :aria-disabled="pending || undefined" @click="emit('navigate', $event, page + 1)">Suivant →</NuxtLink>
  </nav>
</template>
