<script setup lang="ts">
import type { Theater } from '~/types/api'

defineProps<{ theaters: readonly Theater[]; label: string }>()
</script>

<template>
  <ul
    class="-mx-1.5 mt-4 flex snap-x snap-proximity scroll-pl-1.5 gap-4 overflow-x-auto overscroll-x-contain px-1.5 pt-1.5 pb-3"
    :aria-label="label"
    tabindex="0"
  >
    <li
      v-for="theater in theaters"
      :key="theater.id"
      class="w-[85%] min-w-0 shrink-0 snap-start sm:w-[calc((100%-1rem)/2)] lg:w-[calc((100%-2rem)/3)]"
    >
      <HomeCinemaCard :theater="theater" />
    </li>
  </ul>
</template>
