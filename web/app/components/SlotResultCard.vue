<script setup lang="ts">
import { Film, MapPin } from '@lucide/vue'
import type { SlotResult } from '~/types/api'
import { formatParisTime } from '~/utils/date'

defineProps<{ result: SlotResult }>()
const posterFailed = ref(false)
const backdropFailed = ref(false)
const advertisedStartTooltipId = useId()

function hasDelayedStart(result: SlotResult): boolean {
  return Date.parse(result.effective_start_time) !== Date.parse(result.showtime.start_time)
}

function bookingLabel(result: SlotResult): string {
  return `Réserver ${result.showtime.movie.title}, séance annoncée à ${formatParisTime(result.showtime.start_time)} au cinéma ${result.theater.name}`
}
</script>

<template>
  <article class="relative overflow-hidden p-4 hover:bg-[#f1efe8] sm:p-5">
    <img
      v-if="result.backdrop_url && !backdropFailed"
      :src="result.backdrop_url"
      alt=""
      width="640"
      height="180"
      loading="lazy"
      decoding="async"
      class="pointer-events-none absolute inset-y-0 right-0 h-full w-1/2 object-cover opacity-[0.06]"
      aria-hidden="true"
      @error="backdropFailed = true"
    >
    <div class="pointer-events-none absolute inset-0 bg-surface/80" aria-hidden="true" />

    <div class="relative grid grid-cols-[3rem_minmax(0,1fr)] gap-x-3 gap-y-2 sm:grid-cols-[3.25rem_minmax(10rem,auto)_minmax(0,1fr)_auto] sm:items-center sm:gap-4">
      <div class="row-span-2 flex aspect-[2/3] w-12 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] sm:row-span-1 sm:w-[3.25rem]">
        <img
          v-if="result.poster_url && !posterFailed"
          :src="result.poster_url"
          :alt="`Affiche de ${result.showtime.movie.title}`"
          width="52"
          height="78"
          loading="lazy"
          decoding="async"
          class="h-full w-full object-cover"
          @error="posterFailed = true"
        >
        <Film v-else :size="18" class="text-muted/60" aria-hidden="true" />
      </div>
      <div class="col-start-2 border-l-2 border-ink pl-3 sm:col-start-auto">
        <p class="text-xl font-black tabular-nums tracking-[-0.035em] text-ink">
          {{ formatParisTime(hasDelayedStart(result) ? result.effective_start_time : result.showtime.start_time) }}
          <span v-if="hasDelayedStart(result)" class="group relative inline-block text-sm font-normal tracking-normal">
            <span
              :aria-describedby="advertisedStartTooltipId"
              class="inline-block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-1"
              tabindex="0"
            >({{ formatParisTime(result.showtime.start_time) }})</span>
            <span
              :id="advertisedStartTooltipId"
              class="invisible absolute left-1/2 top-full z-20 mt-2 w-max max-w-48 -translate-x-1/2 border border-ink bg-ink px-2 py-1 text-center font-sans text-xs font-normal tracking-normal text-white opacity-0 shadow-sm transition-opacity group-hover:visible group-hover:opacity-100 group-focus-within:visible group-focus-within:opacity-100"
              role="tooltip"
            >Heure de début annoncée, publicités incluses</span>
          </span>
          → {{ formatParisTime(result.showtime.end_time) }}
        </p>
      </div>
      <div class="col-start-2 min-w-0 sm:col-start-auto">
        <h3 class="truncate text-base font-black tracking-[-0.02em] text-ink">
          <NuxtLink
            :to="`/film/${result.showtime.movie.slug}`"
            class="underline-offset-4 hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
          >
            {{ result.showtime.movie.title }}
          </NuxtLink>
        </h3>
        <div class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
          <span class="flex items-center gap-1.5"><MapPin :size="14" aria-hidden="true" /> <BrandedText :text="result.theater.name" /></span>
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
        class="col-span-2 mt-1 inline-flex min-h-10 items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em] sm:col-span-1 sm:mt-0"
        available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
        unavailable-class="text-muted"
      >
        <template #default="{ available }">{{ available ? 'Réserver' : 'Indisponible' }}</template>
      </BookingLink>
    </div>
  </article>
</template>
