<script setup lang="ts">
import { MapPin } from '@lucide/vue'
import type { ShowtimeResultScope, ShowtimeResultViewModel } from '~/types/showtimeResults'
import { formatParisTime } from '~/utils/date'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'

const props = defineProps<{
  result: ShowtimeResultViewModel
  scope: ShowtimeResultScope
  showMovie: boolean
}>()

const advertisedStartTooltipId = useId()
const backdropFailed = ref(false)
const backdropImage = ref<HTMLImageElement | null>(null)
const posterUrl = computed(() => safePosterUrl(props.result.posterUrl))
const backdropUrl = computed(() => safeBackdropUrl(props.result.backdropUrl))
const hasDelayedStart = computed(() => Date.parse(props.result.effectiveStartTime) !== Date.parse(props.result.advertisedStartTime))
const displayedStartTime = computed(() => hasDelayedStart.value ? props.result.effectiveStartTime : props.result.advertisedStartTime)
const isChronological = computed(() => props.showMovie)

watch([() => props.result.posterUrl, () => props.result.backdropUrl], () => {
  backdropFailed.value = false
})

onMounted(() => nextTick(() => {
  if (backdropImage.value?.complete && backdropImage.value.naturalWidth === 0) backdropFailed.value = true
}))

function bookingLabel() {
  if (props.scope === 'single-theater') return `Séance de ${props.result.movieTitle} à ${formatParisTime(props.result.advertisedStartTime)} au ${props.result.theaterName}, réserver`
  return `Réserver ${props.result.movieTitle}, séance annoncée à ${formatParisTime(props.result.advertisedStartTime)} au cinéma ${props.result.theaterName}`
}

function formatRoom(room: string) {
  const roomName = room.trim().replace(/^salle\b\s*/i, '')
  return roomName ? `Salle ${roomName}` : 'Salle'
}
</script>

<template>
  <article v-if="isChronological" class="relative overflow-hidden p-4 hover:bg-[#f1efe8] sm:p-5">
    <img
      v-if="backdropUrl && !backdropFailed"
      ref="backdropImage"
      :src="backdropUrl"
      alt=""
      width="640"
      height="180"
      loading="lazy"
      decoding="async"
      class="pointer-events-none absolute inset-y-0 right-0 h-full w-1/2 object-cover opacity-[0.06]"
      aria-hidden="true"
      :data-media-url="scope === 'single-theater' ? backdropUrl : undefined"
      :data-movie-slug="scope === 'single-theater' ? result.movieSlug : undefined"
      :data-media-kind="scope === 'single-theater' ? 'backdrop' : undefined"
      @error="backdropFailed = true"
    >
    <div class="pointer-events-none absolute inset-0 bg-surface/80" aria-hidden="true" />

    <div class="relative grid grid-cols-[3rem_minmax(0,1fr)] gap-x-3 gap-y-2 sm:grid-cols-[3.25rem_minmax(10rem,auto)_minmax(0,1fr)_auto] sm:items-center sm:gap-4">
      <div class="row-span-2 flex aspect-[2/3] w-12 items-center justify-center overflow-hidden border-2 border-ink bg-[#e8e6de] sm:row-span-1 sm:w-[3.25rem]">
        <PosterImage
          :src="posterUrl"
          :alt="`Affiche de ${result.movieTitle}`"
          width="52"
          height="78"
          loading="lazy"
          decoding="async"
          class="h-full w-full"
          image-class="h-full w-full object-cover"
          fallback-variant="icon-only"
          fallback-class="text-muted/60"
          :fallback-icon-size="18"
          :fallback-text="null"
          :data-media-url="scope === 'single-theater' ? posterUrl : undefined"
          :data-movie-slug="scope === 'single-theater' ? result.movieSlug : undefined"
          :data-media-kind="scope === 'single-theater' ? 'poster' : undefined"
        />
      </div>
      <div class="col-start-2 border-l-2 border-ink pl-3 sm:col-start-auto">
        <p class="text-xl font-black tabular-nums tracking-[-0.035em] text-ink">
          {{ formatParisTime(displayedStartTime) }}
          <span v-if="hasDelayedStart" class="group relative inline-block text-sm font-normal tracking-normal">
            <span :aria-describedby="advertisedStartTooltipId" class="inline-block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-1" tabindex="0">({{ formatParisTime(result.advertisedStartTime) }})</span>
            <span :id="advertisedStartTooltipId" class="invisible absolute left-1/2 top-full z-20 mt-2 w-max max-w-48 -translate-x-1/2 border border-ink bg-ink px-2 py-1 text-center font-sans text-xs font-normal tracking-normal text-white opacity-0 shadow-sm transition-opacity group-hover:visible group-hover:opacity-100 group-focus-within:visible group-focus-within:opacity-100" role="tooltip">Heure de début annoncée, publicités incluses</span>
          </span>
          → {{ formatParisTime(result.endTime) }}
        </p>
      </div>
      <div class="col-start-2 min-w-0 sm:col-start-auto">
        <h3 class="truncate text-base font-black tracking-[-0.02em] text-ink">
          <NuxtLink :to="`/film/${encodeURIComponent(result.movieSlug)}`" class="underline-offset-4 hover:text-primary hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">{{ result.movieTitle }}</NuxtLink>
        </h3>
        <div v-if="scope === 'multi-theater'" class="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
          <span class="flex items-center gap-1.5"><MapPin :size="14" aria-hidden="true" /> <BrandedText :text="result.theaterName" /></span>
          <span>{{ result.room }}</span>
          <span class="font-medium text-muted">{{ result.language }}</span>
          <ShowtimeFormat :format="result.format" class="font-medium text-muted" />
        </div>
        <div v-else class="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
          <span>{{ result.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="result.format" />
          <template v-if="result.room"><span aria-hidden="true">·</span><span>{{ formatRoom(result.room) }}</span></template>
        </div>
      </div>
      <BookingLink
        :url="result.bookingUrl"
        :provider="result.provider"
        :aria-label="bookingLabel()"
        :data-showtime-id="result.showtimeId"
        unstyled
        class="col-span-2 mt-1 inline-flex items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em] sm:col-span-1 sm:mt-0"
        :class="scope === 'single-theater' ? 'min-h-11' : 'min-h-10'"
        available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
        unavailable-class="text-muted"
      >
        <template #default="{ available }">{{ available ? 'Réserver' : scope === 'single-theater' ? 'Réservation indisponible' : 'Indisponible' }}</template>
      </BookingLink>
    </div>
  </article>

  <li v-else class="grid gap-x-4 gap-y-2 p-4 hover:bg-[#f1efe8] sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center sm:px-5">
    <div class="min-w-0">
      <p class="text-xl font-black tabular-nums tracking-[-0.035em] text-ink">
        {{ formatParisTime(displayedStartTime) }}
        <span v-if="hasDelayedStart" class="group relative inline-block text-sm font-normal tracking-normal">
          <span :aria-describedby="advertisedStartTooltipId" class="inline-block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-1" tabindex="0">({{ formatParisTime(result.advertisedStartTime) }})</span>
          <span :id="advertisedStartTooltipId" class="invisible absolute left-1/2 top-full z-20 mt-2 w-max max-w-48 -translate-x-1/2 border border-ink bg-ink px-2 py-1 text-center font-sans text-xs font-normal tracking-normal text-white opacity-0 shadow-sm transition-opacity group-hover:visible group-hover:opacity-100 group-focus-within:visible group-focus-within:opacity-100" role="tooltip">Heure de début annoncée, publicités incluses</span>
        </span>
        → {{ formatParisTime(result.endTime) }}
      </p>
      <div v-if="scope === 'multi-theater'" class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted">
        <span class="flex min-w-0 items-center gap-1.5"><MapPin :size="14" class="shrink-0" aria-hidden="true" /> <BrandedText :text="result.theaterName" /></span>
        <span>{{ result.room }}</span>
        <span class="font-medium text-muted">{{ result.language }}</span>
        <ShowtimeFormat :format="result.format" class="font-medium text-muted" />
      </div>
      <div v-else class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
        <span>{{ result.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="result.format" />
        <template v-if="result.room"><span aria-hidden="true">·</span><span>{{ formatRoom(result.room) }}</span></template>
      </div>
    </div>
    <BookingLink
      :url="result.bookingUrl"
      :provider="result.provider"
      :aria-label="bookingLabel()"
      :data-showtime-id="result.showtimeId"
      unstyled
      class="inline-flex items-center justify-end border-b-2 border-transparent font-mono text-[10px] font-black uppercase tracking-[0.1em]"
      :class="scope === 'single-theater' ? 'min-h-11' : 'min-h-10'"
      available-class="text-ink hover:border-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      unavailable-class="text-muted"
    >
      <template #default="{ available }">{{ available ? 'Réserver' : scope === 'single-theater' ? 'Réservation indisponible' : 'Indisponible' }}</template>
    </BookingLink>
  </li>
</template>
