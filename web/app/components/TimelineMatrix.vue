<script setup lang="ts">
import { Clock3, MapPin } from '@lucide/vue'
import type { TimelineResponse, TimelineShowtime, TimelineTheater } from '~/types/api'
import { formatParisTime } from '~/utils/date'

defineProps<{ timeline: TimelineResponse }>()

const selected = ref<{ showtime: TimelineShowtime; theater: TimelineTheater } | null>(null)
const railWidth = 1080
const hourLabels = Array.from({ length: 19 }, (_, index) => {
  const absoluteHour = 8 + index
  return { offset: index * 60, label: `${absoluteHour % 24}h` }
})

function selectShowtime(showtime: TimelineShowtime, theater: TimelineTheater) {
  selected.value = { showtime, theater }
}
</script>

<template>
  <section aria-label="Frise des séances">
    <div class="overflow-x-auto rounded-lg border border-line bg-surface [--timeline-label-width:136px] [scrollbar-color:#a8a69f_#f0efeb] sm:[--timeline-label-width:176px]">
      <div :style="{ width: `calc(var(--timeline-label-width) + ${railWidth}px)` }">
        <div class="relative h-12 border-b border-line bg-subtle">
          <div class="sticky left-0 z-20 flex h-full items-center border-r border-line bg-subtle px-3 text-xs font-semibold text-muted sm:px-4" style="width: var(--timeline-label-width)">
            Cinémas
          </div>
          <span
            v-for="hour in hourLabels"
            :key="hour.offset"
            class="absolute top-0 flex h-full -translate-x-1/2 items-center text-xs font-medium text-muted"
            :style="{ left: `calc(var(--timeline-label-width) + ${hour.offset}px)` }"
          >
            {{ hour.label }}
          </span>
        </div>

        <div
          v-for="theater in timeline.theaters"
          :key="theater.id"
          class="relative min-h-24 border-b border-line bg-[repeating-linear-gradient(to_right,transparent_0,transparent_14px,rgba(222,221,215,0.45)_15px),repeating-linear-gradient(to_right,transparent_0,transparent_59px,rgba(190,188,180,0.65)_60px)] bg-[length:15px_100%,60px_100%] last:border-b-0"
          :style="{ backgroundPosition: 'var(--timeline-label-width) 0, var(--timeline-label-width) 0' }"
        >
          <div class="sticky left-0 z-10 flex min-h-24 flex-col justify-center border-r border-line bg-surface px-3 sm:px-4" style="width: var(--timeline-label-width)">
            <strong class="text-sm leading-snug text-ink">{{ theater.name }}</strong>
            <span class="mt-1 flex items-center gap-1 text-xs text-muted">
              <MapPin :size="12" aria-hidden="true" /> {{ theater.city }}
            </span>
          </div>

          <button
            v-for="showtime in theater.showtimes"
            :key="showtime.id"
            type="button"
            class="absolute top-4 h-16 overflow-hidden rounded-md border px-2.5 py-2 text-left transition focus:z-20"
            :class="[
              showtime.language === 'VOSTFR' ? 'border-orange-200 bg-orange-50 text-orange-950 hover:border-accent' : 'border-stone-300 bg-stone-100 text-stone-900 hover:border-stone-500',
              selected?.showtime.id === showtime.id ? 'border-accent' : ''
            ]"
            :style="{ left: `calc(var(--timeline-label-width) + ${showtime.start_offset_minutes}px)`, width: `${showtime.duration_minutes}px` }"
            :aria-label="`${showtime.movie.title}, ${formatParisTime(showtime.start_time)}, ${showtime.language}`"
            @click="selectShowtime(showtime, theater)"
          >
            <span class="block truncate text-xs font-semibold">{{ showtime.movie.title }}</span>
            <span class="mt-1 block truncate text-[11px] text-current opacity-70">{{ formatParisTime(showtime.start_time) }} · {{ showtime.language }}</span>
          </button>
        </div>
      </div>
    </div>

    <div v-if="selected" class="mt-4 border-y border-line border-l-2 border-l-accent bg-surface px-4 py-4" aria-live="polite">
      <div class="flex flex-col justify-between gap-3 sm:flex-row sm:items-center">
        <div>
          <p class="text-sm font-medium text-accent">Séance sélectionnée</p>
          <h2 class="mt-1 text-lg font-semibold text-ink">{{ selected.showtime.movie.title }}</h2>
          <p class="mt-1 text-sm text-muted">{{ selected.theater.name }} · {{ selected.showtime.room }} · {{ selected.showtime.language }} · {{ selected.showtime.format }}</p>
        </div>
        <div class="flex items-center gap-2 text-sm font-semibold text-ink">
          <Clock3 :size="18" class="text-accent" aria-hidden="true" />
          {{ formatParisTime(selected.showtime.start_time) }} → {{ formatParisTime(selected.showtime.end_time) }}
        </div>
      </div>
    </div>
  </section>
</template>
