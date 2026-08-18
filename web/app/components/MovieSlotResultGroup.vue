<script setup lang="ts">
import { Film, MapPin } from '@lucide/vue'
import type { SlotResult } from '~/types/api'
import { formatParisTime } from '~/utils/date'

const props = defineProps<{ results: SlotResult[] }>()
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
  <article v-if="movie" class="overflow-hidden rounded-lg border border-line bg-surface">
    <header class="relative overflow-hidden border-b border-line bg-subtle px-4 py-4 sm:px-5">
      <img
        v-if="backdropUrl && !backdropFailed"
        :src="backdropUrl"
        alt=""
        width="960"
        height="240"
        loading="lazy"
        decoding="async"
        class="absolute inset-0 h-full w-full object-cover"
        aria-hidden="true"
        @error="backdropFailed = true"
      >
      <div class="absolute inset-0 bg-gradient-to-r from-surface via-surface/95 to-surface/65" aria-hidden="true" />

      <div class="relative flex items-center gap-4">
        <div class="flex aspect-[2/3] w-16 shrink-0 items-center justify-center overflow-hidden rounded border border-line bg-subtle shadow-sm sm:w-[4.5rem]">
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
            <h3 class="text-lg font-semibold leading-snug text-ink">
              <NuxtLink
                :to="`/film/${movie.slug}`"
                class="rounded-sm underline-offset-4 transition-colors hover:text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
              >
                {{ movie.title }}
              </NuxtLink>
            </h3>
            <p class="mt-0.5 text-sm text-muted">{{ movie.runtime_minutes }} min</p>
          </div>
          <p class="shrink-0 text-sm font-medium text-muted">{{ results.length }} séance{{ results.length > 1 ? 's' : '' }}</p>
        </div>
      </div>
    </header>

    <ul class="divide-y divide-line" aria-label="Séances compatibles">
      <li v-for="result in results" :key="result.showtime.id" class="grid gap-x-4 gap-y-2 p-4 transition hover:bg-subtle/40 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
        <div class="min-w-0">
          <template v-if="hasDelayedArrival(result)">
            <p class="text-sm font-semibold tabular-nums text-ink">Arrivée conseillée {{ formatParisTime(result.effective_start_time) }} → fin {{ formatParisTime(result.effective_end_time) }}</p>
            <p class="mt-0.5 text-xs tabular-nums text-muted">Séance annoncée à {{ formatParisTime(result.showtime.start_time) }}</p>
          </template>
          <p v-else class="text-lg font-semibold tabular-nums text-ink">{{ formatParisTime(result.effective_start_time) }} → {{ formatParisTime(result.effective_end_time) }}</p>
          <div class="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
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
          class="inline-flex min-h-10 items-center justify-end text-sm font-semibold"
          available-class="text-accent underline-offset-4 hover:underline focus-visible:rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2"
          unavailable-class="text-muted"
        >
          <template #default="{ available }">{{ available ? 'Réserver' : 'Indisponible' }}</template>
        </BookingLink>
      </li>
    </ul>
  </article>
</template>
