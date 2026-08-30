<script setup lang="ts">
import type { ResultLayout, ShowtimeResultScope, ShowtimeResultViewModel } from '~/types/showtimeResults'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'

const props = withDefaults(defineProps<{
  results: ShowtimeResultViewModel[]
  layout: ResultLayout
  scope: ShowtimeResultScope
  selectedKeys?: readonly string[]
}>(), {
  selectedKeys: () => []
})

const emit = defineEmits<{
  toggleSelection: [key: string]
}>()

const movie = computed(() => props.results[0] ?? null)
const posterUrl = computed(() => {
  for (const result of props.results) {
    const url = safePosterUrl(result.posterUrl)
    if (url) return url
  }
  return null
})
const backdropUrl = computed(() => {
  for (const result of props.results) {
    const url = safeBackdropUrl(result.backdropUrl)
    if (url) return url
  }
  return null
})
const backdropFailed = ref(false)
const backdropImage = ref<HTMLImageElement | null>(null)
const selectedKeySet = computed(() => new Set(props.selectedKeys))

watch([posterUrl, backdropUrl], () => {
  backdropFailed.value = false
})

onMounted(() => nextTick(() => {
  if (backdropImage.value?.complete && backdropImage.value.naturalWidth === 0) backdropFailed.value = true
}))
</script>

<template>
  <article v-if="movie" :data-movie-slug="scope === 'single-theater' ? movie.movieSlug : undefined" class="border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]" :class="scope === 'multi-theater' ? 'overflow-hidden' : undefined">
    <header
      v-if="scope === 'single-theater'"
      class="relative isolate grid min-h-48 grid-cols-[96px_minmax(0,1fr)] items-end gap-5 overflow-hidden border-b-2 border-ink p-4 sm:min-h-56 sm:grid-cols-[120px_minmax(0,1fr)] sm:p-5"
      :class="backdropUrl && !backdropFailed ? 'text-white' : 'bg-[#f1efe8] text-ink'"
    >
      <img
        v-if="backdropUrl && !backdropFailed"
        ref="backdropImage"
        :src="backdropUrl"
        alt=""
        aria-hidden="true"
        :data-media-url="backdropUrl"
        :data-movie-slug="movie.movieSlug"
        data-media-kind="backdrop"
        class="absolute inset-0 -z-20 size-full object-cover"
        @error="backdropFailed = true"
      >
      <div v-if="backdropUrl && !backdropFailed" class="absolute inset-0 -z-10 bg-black/80" aria-hidden="true" />
      <div class="aspect-[2/3] w-24 overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[5px_5px_0_#27272a] sm:w-[120px]">
        <PosterImage
          :src="posterUrl"
          :alt="`Affiche de ${movie.movieTitle}`"
          sizes="(min-width: 640px) 120px, 96px"
          :data-media-url="posterUrl"
          :data-movie-slug="movie.movieSlug"
          data-media-kind="poster"
          class="size-full"
          image-class="size-full object-cover"
          fallback-class="gap-2 px-2 text-center text-[10px] font-bold text-muted"
          :fallback-icon-size="28"
          :fallback-marker="movie.movieSlug"
        />
      </div>
      <div class="min-w-0 pb-1">
        <h3 class="break-words text-2xl font-black tracking-[-0.04em] sm:text-3xl"><NuxtLink :to="`/film/${encodeURIComponent(movie.movieSlug)}`" class="inline-flex min-h-11 items-center underline decoration-2 underline-offset-4 hover:text-primary">{{ movie.movieTitle }}</NuxtLink></h3>
        <p class="mt-1 font-mono text-[0.68rem] font-black uppercase tracking-[0.1em]" :class="backdropUrl && !backdropFailed ? 'text-white' : undefined">{{ movie.movieRuntimeMinutes }} min</p>
      </div>
    </header>

    <header v-else class="relative overflow-hidden border-b-2 border-ink bg-ink px-4 py-4 text-white sm:px-5 sm:py-5">
      <img v-if="backdropUrl && !backdropFailed" ref="backdropImage" :src="backdropUrl" alt="" width="960" height="240" loading="lazy" decoding="async" class="absolute inset-0 h-full w-full object-cover opacity-100" aria-hidden="true" @error="backdropFailed = true">
      <div class="absolute inset-0 bg-black/80" aria-hidden="true" />
      <div class="relative flex items-center gap-4">
        <div class="flex aspect-[2/3] w-16 shrink-0 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[4px_4px_0_#27272a] sm:w-[4.5rem]">
          <PosterImage :src="posterUrl" :alt="`Affiche de ${movie.movieTitle}`" sizes="(min-width: 640px) 72px, 64px" class="h-full w-full" image-class="h-full w-full object-cover" fallback-variant="icon-only" fallback-class="text-muted/60" :fallback-icon-size="24" :fallback-text="null" />
        </div>
        <div class="flex min-w-0 flex-1 flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
          <div class="min-w-0">
            <h3 class="text-xl font-black leading-snug tracking-[-0.03em] text-white sm:text-2xl"><NuxtLink :to="`/film/${encodeURIComponent(movie.movieSlug)}`" class="underline-offset-4 hover:text-highlight hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-ink">{{ movie.movieTitle }}</NuxtLink></h3>
            <p class="mt-1 font-mono text-[10px] font-bold uppercase tracking-[0.1em] text-white">{{ movie.movieRuntimeMinutes }} min</p>
          </div>
          <p class="shrink-0 font-mono text-[10px] font-bold uppercase tracking-[0.1em] text-white">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</p>
        </div>
      </div>
    </header>

    <ul v-if="layout === 'lines'" class="divide-y-2 divide-ink" :aria-label="scope === 'single-theater' ? `Séances de ${movie.movieTitle}` : 'Séances compatibles'">
      <ShowtimeResultLine v-for="result in results" :key="result.key" :result="result" :scope="scope" :show-movie="false" :selected="selectedKeySet.has(result.key)" @toggle-selection="emit('toggleSelection', $event)" />
    </ul>
    <ul v-else class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 p-4 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4 sm:p-5 lg:grid-cols-[repeat(auto-fill,minmax(210px,1fr))]" :aria-label="scope === 'single-theater' ? `Séances de ${movie.movieTitle}` : 'Séances compatibles'">
      <li v-for="result in results" :key="result.key" class="min-w-0"><ShowtimeResultBox :result="result" :scope="scope" :show-movie="false" :selected="selectedKeySet.has(result.key)" @toggle-selection="emit('toggleSelection', $event)" /></li>
    </ul>
  </article>
</template>
