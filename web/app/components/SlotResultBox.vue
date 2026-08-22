<script setup lang="ts">
import { Film, MapPin } from '@lucide/vue'
import type { SlotResult } from '~/types/api'
import { formatParisTime } from '~/utils/date'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'

const props = withDefaults(defineProps<{
  result: SlotResult
  showMovie?: boolean
}>(), {
  showMovie: false
})

const hasDelayedArrival = computed(() => Date.parse(props.result.effective_start_time) !== Date.parse(props.result.showtime.start_time))
const backdropFailed = ref(false)
const posterFailed = ref(false)
const backdropUrl = computed(() => safeBackdropUrl(props.result.backdrop_url))
const posterUrl = computed(() => safePosterUrl(props.result.poster_url))
const mediaKind = computed<'backdrop' | 'poster' | null>(() => {
  if (backdropUrl.value && !backdropFailed.value) return 'backdrop'
  if (posterUrl.value && !posterFailed.value) return 'poster'
  return null
})
const mediaUrl = computed(() => mediaKind.value === 'backdrop' ? backdropUrl.value : mediaKind.value === 'poster' ? posterUrl.value : null)

watch([() => props.result.backdrop_url, () => props.result.poster_url], () => {
  backdropFailed.value = false
  posterFailed.value = false
})

function handleMediaError() {
  if (mediaKind.value === 'backdrop') backdropFailed.value = true
  else if (mediaKind.value === 'poster') posterFailed.value = true
}

function bookingLabel(): string {
  return `Réserver ${props.result.showtime.movie.title}, séance annoncée à ${formatParisTime(props.result.showtime.start_time)} au cinéma ${props.result.theater.name}`
}

function formatRoom(room: string): string {
  const roomName = room.trim().replace(/^salle\b\s*/i, '')
  return roomName ? `Salle ${roomName}` : 'Salle'
}
</script>

<template>
  <article class="flex h-full min-h-32 min-w-0 flex-col border-2 border-ink bg-surface p-3 text-left shadow-[4px_4px_0_#27272a]">
    <div v-if="showMovie" class="relative -mx-3 -mt-3 mb-3 flex h-24 items-center justify-center overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
      <img
        v-if="mediaKind === 'backdrop' && mediaUrl"
        :src="mediaUrl"
        alt=""
        width="320"
        height="96"
        loading="lazy"
        decoding="async"
        class="size-full object-cover"
        aria-hidden="true"
        @error="handleMediaError"
      >
      <template v-else-if="mediaKind === 'poster' && mediaUrl">
        <img
          :src="mediaUrl"
          alt=""
          width="320"
          height="96"
          loading="lazy"
          decoding="async"
          class="absolute inset-0 size-full scale-110 object-cover blur-lg"
          aria-hidden="true"
        >
        <div class="absolute inset-0 bg-black/15" aria-hidden="true" />
        <img
          :src="mediaUrl"
          alt=""
          width="320"
          height="96"
          loading="lazy"
          decoding="async"
          class="relative z-10 size-full object-contain"
          aria-hidden="true"
          @error="handleMediaError"
        >
      </template>
      <span v-else class="flex size-11 items-center justify-center border-2 border-muted text-muted" aria-hidden="true">
        <Film :size="24" />
      </span>
    </div>
    <h3 v-if="showMovie" class="mb-3 line-clamp-2 text-sm font-black leading-tight tracking-[-0.02em] text-ink">
      <NuxtLink :to="`/film/${result.showtime.movie.slug}`" class="hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">
        {{ result.showtime.movie.title }}
      </NuxtLink>
    </h3>

    <div class="flex w-full items-baseline justify-between gap-2">
      <div>
        <span class="text-2xl font-black tracking-[-0.045em] text-ink">{{ formatParisTime(result.effective_start_time) }}</span>
        <p v-if="hasDelayedArrival" class="font-mono text-[8px] font-black uppercase tracking-[0.06em] text-primary">Arrivée conseillée</p>
      </div>
      <span class="font-mono text-[9px] font-bold uppercase text-muted">fin {{ formatParisTime(result.effective_end_time) }}</span>
    </div>
    <p v-if="hasDelayedArrival" class="mt-1 font-mono text-[9px] font-bold uppercase tabular-nums tracking-[0.06em] text-muted">Séance {{ formatParisTime(result.showtime.start_time) }}</p>

    <div class="mt-4 flex min-w-0 items-start gap-1.5 text-xs font-bold text-ink">
      <MapPin :size="13" class="mt-0.5 shrink-0" aria-hidden="true" />
      <BrandedText :text="result.theater.name" />
    </div>
    <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
      <span>{{ result.showtime.language }}</span>
      <span aria-hidden="true">·</span>
      <ShowtimeFormat :format="result.showtime.format" />
      <template v-if="result.showtime.room">
        <span aria-hidden="true">·</span>
        <span>{{ formatRoom(result.showtime.room) }}</span>
      </template>
    </div>

    <BookingLink
      :url="result.showtime.booking_url"
      :provider="result.showtime.provider"
      :aria-label="bookingLabel()"
      unstyled
      class="mt-auto inline-flex min-h-11 items-end pt-3 font-mono text-[10px] font-black uppercase tracking-[0.1em]"
      available-class="text-ink underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      unavailable-class="text-muted"
    >
      <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
    </BookingLink>
  </article>
</template>
