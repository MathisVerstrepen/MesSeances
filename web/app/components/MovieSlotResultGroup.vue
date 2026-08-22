<script setup lang="ts">
import { Film, MapPin } from '@lucide/vue'
import type { SlotResult } from '~/types/api'
import { formatParisTime } from '~/utils/date'

const props = withDefaults(defineProps<{
  results: SlotResult[]
  layout?: 'lines' | 'boxes'
}>(), {
  layout: 'lines'
})
const movie = computed(() => props.results[0]?.showtime.movie)
const posterUrl = computed(() => props.results[0]?.poster_url ?? null)
const backdropUrl = computed(() => props.results[0]?.backdrop_url ?? null)
const posterFailed = ref(false)
const backdropFailed = ref(false)

function hasDelayedArrival(result: SlotResult): boolean {
  return Date.parse(result.effective_start_time) !== Date.parse(result.showtime.start_time)
}

function bookingLabel(result: SlotResult): string {
  return `Réserver ${result.showtime.movie.title}, séance annoncée à ${formatParisTime(result.showtime.start_time)} au cinéma ${result.theater.name}`
}
</script>

<template>
  <article v-if="movie" class="overflow-hidden border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]">
    <header class="relative overflow-hidden border-b-2 border-ink bg-ink px-4 py-4 text-white sm:px-5 sm:py-5">
      <img
        v-if="backdropUrl && !backdropFailed"
        :src="backdropUrl"
        alt=""
        width="960"
        height="240"
        loading="lazy"
        decoding="async"
        class="absolute inset-0 h-full w-full object-cover opacity-100"
        aria-hidden="true"
        @error="backdropFailed = true"
      >
      <div class="absolute inset-0 bg-black/80" aria-hidden="true" />

      <div class="relative flex items-center gap-4">
        <div class="flex aspect-[2/3] w-16 shrink-0 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] shadow-[4px_4px_0_#27272a] sm:w-[4.5rem]">
          <img
            v-if="posterUrl && !posterFailed"
            :src="posterUrl"
            :alt="`Affiche de ${movie.title}`"
            width="96"
            height="144"
            loading="lazy"
            decoding="async"
            class="h-full w-full object-cover"
            @error="posterFailed = true"
          >
          <Film v-else :size="24" class="text-muted/60" aria-hidden="true" />
        </div>
        <div class="flex min-w-0 flex-1 flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
          <div class="min-w-0">
            <h3 class="text-xl font-black leading-snug tracking-[-0.03em] text-white sm:text-2xl">
              <NuxtLink
                :to="`/film/${movie.slug}`"
                class="underline-offset-4 hover:text-highlight hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-ink"
              >
                {{ movie.title }}
              </NuxtLink>
            </h3>
            <p class="mt-1 font-mono text-[10px] font-bold uppercase tracking-[0.1em] text-white">{{ movie.runtime_minutes }} min</p>
          </div>
          <p class="shrink-0 font-mono text-[10px] font-bold uppercase tracking-[0.1em] text-white">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</p>
        </div>
      </div>
    </header>

    <ul v-if="layout === 'lines'" class="divide-y-2 divide-ink" aria-label="Séances compatibles">
      <li v-for="result in results" :key="result.showtime.id" class="grid gap-x-4 gap-y-2 p-4 hover:bg-[#f1efe8] sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
        <div class="min-w-0">
          <template v-if="hasDelayedArrival(result)">
            <p class="text-sm font-black tabular-nums text-ink">Arrivée conseillée {{ formatParisTime(result.effective_start_time) }} → fin {{ formatParisTime(result.effective_end_time) }}</p>
            <p class="mt-1 font-mono text-[9px] font-bold uppercase tabular-nums tracking-[0.08em] text-muted">Séance annoncée à {{ formatParisTime(result.showtime.start_time) }}</p>
          </template>
          <p v-else class="text-xl font-black tabular-nums tracking-[-0.035em] text-ink">{{ formatParisTime(result.effective_start_time) }} → {{ formatParisTime(result.effective_end_time) }}</p>
          <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
            <span class="flex min-w-0 items-center gap-1.5"><MapPin :size="14" class="shrink-0" aria-hidden="true" /> <BrandedText :text="result.theater.name" /></span>
            <span>{{ result.showtime.room }}</span>
            <span class="font-medium text-muted">{{ result.showtime.language }}</span>
            <ShowtimeFormat :format="result.showtime.format" class="font-medium text-muted" />
          </div>
        </div>
        <BookingLink
          :url="result.showtime.booking_url"
          :provider="result.showtime.provider"
          :aria-label="bookingLabel(result)"
          unstyled
          class="inline-flex min-h-10 items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em]"
          available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
          unavailable-class="text-muted"
        >
          <template #default="{ available }">{{ available ? 'Réserver' : 'Indisponible' }}</template>
        </BookingLink>
      </li>
    </ul>
    <ul v-else class="grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 p-4 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4 sm:p-5" aria-label="Séances compatibles">
      <li v-for="result in results" :key="result.showtime.id" class="min-w-0">
        <SlotResultBox :result="result" />
      </li>
    </ul>
  </article>
</template>
