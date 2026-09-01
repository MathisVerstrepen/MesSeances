<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'
import type { CatalogMovie } from '~/types/api'
import { formatRuntime, formatShowtimeCount } from '~/utils/formats'

const props = withDefaults(defineProps<{
  movie: CatalogMovie
  to: RouteLocationRaw
  posterResetKey?: string | number
}>(), {
  posterResetKey: undefined
})
</script>

<template>
  <NuxtLink :to="props.to" class="group block text-ink transition-transform hover:-translate-y-1 focus-visible:ring-offset-4 motion-reduce:transition-none motion-reduce:hover:translate-y-0">
    <div class="relative aspect-[2/3] overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[5px_5px_0_#27272a]">
      <PosterImage
        :src="movie.poster_url"
        :alt="`Affiche de ${movie.title}`"
        sizes="(min-width: 1280px) calc((min(100vw, 1440px) - 12.5rem) / 6), (min-width: 1024px) calc((100vw - 9.5rem) / 4), (min-width: 640px) calc((100vw - 6rem) / 3), calc((100vw - 3rem) / 2)"
        :reset-key="posterResetKey"
        :data-poster-slug="movie.slug"
        class="h-full w-full"
        image-class="h-full w-full object-cover"
        fallback-class="gap-2 bg-[#e8e6de] px-3 text-center text-xs font-bold text-muted"
        :fallback-icon-size="32"
      />
    </div>
    <div class="border-x-2 border-b-2 border-ink bg-surface px-3 py-3">
      <h3 class="line-clamp-2 min-h-[2.5rem] text-sm font-black leading-snug tracking-[-0.02em] group-hover:text-primary">{{ movie.title }}</h3>
      <span class="inline-block font-mono text-[9px] font-bold uppercase tracking-[0.14em]">
        {{ formatRuntime(movie.runtime_minutes) }}<template v-if="movie.showtime_count !== undefined"> · {{ formatShowtimeCount(movie.showtime_count) }}</template>
      </span>
    </div>
  </NuxtLink>
</template>
