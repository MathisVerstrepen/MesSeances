<script setup lang="ts">
import { Clock3, Film, MapPin } from '@lucide/vue'
import type { SlotResult } from '~/types/api'
import { formatParisTime } from '~/utils/date'

defineProps<{ result: SlotResult }>()
</script>

<template>
  <article class="p-5 transition hover:bg-subtle/50 sm:p-6">
    <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-start">
      <div class="min-w-0">
        <div class="flex flex-wrap items-center gap-2 text-xs font-semibold">
          <span :class="result.showtime.language === 'VOSTFR' ? 'bg-orange-50 text-orange-800' : 'bg-subtle text-stone-700'" class="rounded px-2 py-1">{{ result.showtime.language }}</span>
          <BrandedText :text="result.showtime.format" class="rounded bg-subtle px-2 py-1 text-stone-700" />
        </div>
        <h2 class="mt-3 text-xl font-semibold text-ink">{{ result.showtime.movie.title }}</h2>
        <div class="mt-2 flex flex-wrap gap-x-5 gap-y-2 text-sm text-muted">
          <span class="flex items-center gap-2"><MapPin :size="15" aria-hidden="true" /> <BrandedText :text="result.theater.name" /></span>
          <span class="flex items-center gap-2"><Film :size="15" aria-hidden="true" /> {{ result.showtime.movie.runtime_minutes }} min · {{ result.showtime.room }}</span>
        </div>
      </div>

      <div class="shrink-0 border-l-2 border-accent pl-4">
        <p class="flex items-center gap-2 text-lg font-semibold text-ink">
          <Clock3 :size="18" class="text-accent" aria-hidden="true" />
          {{ formatParisTime(result.showtime.start_time) }} → {{ formatParisTime(result.showtime.end_time) }}
        </p>
        <p class="mt-1 text-sm text-muted">Fin effective : {{ formatParisTime(result.effective_end_time) }}</p>
        <div class="mt-4">
          <BookingLink :url="result.showtime.booking_url" :provider="result.showtime.provider" />
        </div>
      </div>
    </div>

    <dl class="mt-5 grid grid-cols-2 border-t border-line pt-4 sm:grid-cols-3">
      <div class="pr-3">
        <dt class="text-xs text-muted">Avant la séance</dt>
        <dd class="mt-1 font-semibold text-ink">{{ result.slack_before_minutes }} min</dd>
      </div>
      <div class="border-l border-line px-3">
        <dt class="text-xs text-muted">Après la séance</dt>
        <dd class="mt-1 font-semibold text-ink">{{ result.slack_after_minutes }} min</dd>
      </div>
      <div class="col-span-2 mt-4 border-t border-line pt-4 sm:col-span-1 sm:mt-0 sm:border-l sm:border-t-0 sm:px-3 sm:pt-0">
        <dt class="text-xs text-muted">Publicités incluses</dt>
        <dd class="mt-1 font-semibold text-ink">{{ result.buffer_ads_minutes }} min</dd>
      </div>
    </dl>
  </article>
</template>
