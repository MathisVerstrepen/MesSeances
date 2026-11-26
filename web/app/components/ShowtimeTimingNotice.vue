<script setup lang="ts">
import { AlertTriangle } from '@lucide/vue'
import type { ShowtimeTimingState } from '~/utils/showtimeTiming'

defineProps<{ state: ShowtimeTimingState }>()
</script>

<template>
  <span
    v-if="state === 'warning'"
    class="mt-2 inline-flex items-center gap-1 text-xs font-black text-amber-800"
  >
    <AlertTriangle :size="14" aria-hidden="true" />
    Séance commencée
  </span>
  <span v-else-if="state === 'past'" class="sr-only">Séance passée</span>
</template>
