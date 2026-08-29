<script setup lang="ts">
import { Film, MapPin } from '@lucide/vue'
import type { ShowtimeResultScope, ShowtimeResultViewModel } from '~/types/showtimeResults'
import { formatParisTime } from '~/utils/date'
import { safeBackdropUrl, safePosterUrl } from '~/utils/safeImageUrl'

const props = withDefaults(defineProps<{
  result: ShowtimeResultViewModel
  scope: ShowtimeResultScope
  showMovie: boolean
  selected?: boolean
}>(), {
  selected: false
})

const emit = defineEmits<{
  toggleSelection: [key: string]
}>()

const advertisedStartTooltipId = useId()
const backdropFailed = ref(false)
const posterFailed = ref(false)
const mediaImage = ref<HTMLImageElement | null>(null)
const backdropUrl = computed(() => safeBackdropUrl(props.result.backdropUrl))
const posterUrl = computed(() => safePosterUrl(props.result.posterUrl))
const hasDelayedStart = computed(() => Date.parse(props.result.effectiveStartTime) !== Date.parse(props.result.advertisedStartTime))
const displayedStartTime = computed(() => hasDelayedStart.value ? props.result.effectiveStartTime : props.result.advertisedStartTime)
const mediaKind = computed<'backdrop' | 'poster' | null>(() => {
  if (backdropUrl.value && !backdropFailed.value) return 'backdrop'
  if (posterUrl.value && !posterFailed.value) return 'poster'
  return null
})
const mediaUrl = computed(() => mediaKind.value === 'backdrop' ? backdropUrl.value : mediaKind.value === 'poster' ? posterUrl.value : null)
const selectionLabel = computed(() => `${props.selected ? 'Retirer' : 'Ajouter'} la séance de ${props.result.movieTitle} à ${formatParisTime(displayedStartTime.value)} au cinéma ${props.result.theaterName}`)

watch([() => props.result.backdropUrl, () => props.result.posterUrl], () => {
  backdropFailed.value = false
  posterFailed.value = false
})

onMounted(() => nextTick(() => {
  if (mediaImage.value?.complete && mediaImage.value.naturalWidth === 0) handleMediaError()
}))

function handleMediaError() {
  if (mediaKind.value === 'backdrop') backdropFailed.value = true
  else if (mediaKind.value === 'poster') posterFailed.value = true
}

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
  <BookingLink
    v-if="scope === 'single-theater' && !showMovie"
    v-slot="{ available }"
    :url="result.bookingUrl"
    :provider="result.provider"
    :aria-label="bookingLabel()"
    :data-showtime-id="result.showtimeId"
    unstyled
    class="group flex h-full min-h-32 w-full flex-col items-start justify-between overflow-hidden border-2 p-3 text-left"
    available-class="border-ink bg-surface text-ink shadow-[4px_4px_0_#27272a] hover:bg-[#f1efe8]"
    unavailable-class="cursor-not-allowed border-dashed border-muted bg-[#e8e6de] text-muted shadow-none"
  >
    <div class="flex w-full items-baseline justify-between gap-2"><span class="text-2xl font-black tracking-[-0.045em]">{{ formatParisTime(displayedStartTime) }}</span><span class="font-mono text-[9px] font-bold uppercase text-muted">fin {{ formatParisTime(result.endTime) }}</span></div>
    <div class="mt-5 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
      <span>{{ result.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="result.format" />
      <template v-if="result.room"><span aria-hidden="true">·</span><span>{{ formatRoom(result.room) }}</span></template>
    </div>
    <span v-if="!available" class="mt-2 text-xs font-black">Réservation indisponible</span>
  </BookingLink>

  <article v-else-if="scope === 'single-theater'" class="flex h-full min-h-48 min-w-0 flex-col border-2 border-ink bg-surface p-3 text-left shadow-[4px_4px_0_#27272a]">
    <div class="relative -mx-3 -mt-3 mb-3 flex h-24 items-center justify-center overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
      <img
        v-if="mediaKind === 'backdrop' && mediaUrl"
        ref="mediaImage"
        :src="mediaUrl"
        alt=""
        width="320"
        height="96"
        loading="lazy"
        decoding="async"
        class="size-full object-cover"
        aria-hidden="true"
        :data-media-url="mediaUrl"
        :data-movie-slug="result.movieSlug"
        data-media-kind="backdrop"
        @error="handleMediaError"
      >
      <img
        v-else-if="mediaKind === 'poster' && mediaUrl"
        ref="mediaImage"
        :src="mediaUrl"
        alt=""
        width="320"
        height="96"
        loading="lazy"
        decoding="async"
        class="size-full object-contain"
        aria-hidden="true"
        :data-media-url="mediaUrl"
        :data-movie-slug="result.movieSlug"
        data-media-kind="poster"
        @error="handleMediaError"
      >
      <Film v-else :size="24" class="text-muted" aria-hidden="true" />
    </div>
    <h3 class="mb-3 line-clamp-2 text-sm font-black leading-tight tracking-[-0.02em] text-ink"><NuxtLink :to="`/film/${encodeURIComponent(result.movieSlug)}`" class="hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">{{ result.movieTitle }}</NuxtLink></h3>
    <p class="text-2xl font-black tabular-nums tracking-[-0.045em] text-ink">{{ formatParisTime(displayedStartTime) }} → {{ formatParisTime(result.endTime) }}</p>
    <div class="mt-4 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
      <span>{{ result.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="result.format" />
      <template v-if="result.room"><span aria-hidden="true">·</span><span>{{ formatRoom(result.room) }}</span></template>
    </div>
    <BookingLink
      :url="result.bookingUrl"
      :provider="result.provider"
      :aria-label="bookingLabel()"
      :data-showtime-id="result.showtimeId"
      unstyled
      class="mt-auto inline-flex min-h-11 items-end pt-3 font-mono text-[10px] font-black uppercase tracking-[0.1em]"
      available-class="text-ink underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      unavailable-class="text-muted"
    >
      <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
    </BookingLink>
  </article>

  <article v-else class="relative flex h-full min-h-32 min-w-0 flex-col border-2 p-3 text-left" :class="selected ? 'border-primary bg-[#fff0b3] shadow-[5px_5px_0_#991b1b]' : 'border-ink bg-surface shadow-[4px_4px_0_#27272a]'">
    <button
      type="button"
      class="absolute inset-0 z-10 cursor-pointer focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-inset focus-visible:ring-primary"
      :aria-label="selectionLabel"
      :aria-pressed="selected"
      @click="emit('toggleSelection', result.key)"
    />
    <div v-if="showMovie" class="pointer-events-none relative -mx-3 -mt-3 mb-3 flex h-24 items-center justify-center overflow-hidden border-b-2 border-ink bg-[#e8e6de]">
      <img v-if="mediaKind === 'backdrop' && mediaUrl" ref="mediaImage" :src="mediaUrl" alt="" width="320" height="96" loading="lazy" decoding="async" class="size-full object-cover" aria-hidden="true" @error="handleMediaError">
      <template v-else-if="mediaKind === 'poster' && mediaUrl">
        <img :src="mediaUrl" alt="" width="320" height="96" loading="lazy" decoding="async" class="absolute inset-0 size-full scale-110 object-cover blur-lg" aria-hidden="true">
        <div class="absolute inset-0 bg-black/15" aria-hidden="true" />
        <img ref="mediaImage" :src="mediaUrl" alt="" width="320" height="96" loading="lazy" decoding="async" class="relative z-10 size-full object-contain" aria-hidden="true" @error="handleMediaError">
      </template>
      <span v-else class="flex size-11 items-center justify-center border-2 border-muted text-muted" aria-hidden="true"><Film :size="24" /></span>
    </div>
    <h3 v-if="showMovie" class="mb-3 line-clamp-2 text-sm font-black leading-tight tracking-[-0.02em] text-ink"><NuxtLink :to="`/film/${encodeURIComponent(result.movieSlug)}`" class="relative z-20 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2">{{ result.movieTitle }}</NuxtLink></h3>

    <p class="text-2xl font-black tabular-nums tracking-[-0.045em] text-ink">
      {{ formatParisTime(displayedStartTime) }}
      <span v-if="hasDelayedStart" class="group relative inline-block text-sm font-normal tracking-normal">
        <span :aria-describedby="advertisedStartTooltipId" class="relative z-20 inline-block focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-1" tabindex="0">({{ formatParisTime(result.advertisedStartTime) }})</span>
        <span :id="advertisedStartTooltipId" class="invisible absolute left-1/2 top-full z-20 mt-2 w-max max-w-48 -translate-x-1/2 border border-ink bg-ink px-2 py-1 text-center font-sans text-xs font-normal tracking-normal text-white opacity-0 shadow-sm transition-opacity group-hover:visible group-hover:opacity-100 group-focus-within:visible group-focus-within:opacity-100" role="tooltip">Heure de début annoncée, publicités incluses</span>
      </span>
      → {{ formatParisTime(result.endTime) }}
    </p>

    <div class="mt-4 flex min-w-0 items-start gap-1.5 text-xs font-bold text-ink"><MapPin :size="13" class="mt-0.5 shrink-0" aria-hidden="true" /><BrandedText :text="result.theaterName" /></div>
    <div class="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-[9px] font-bold uppercase tracking-[0.08em] text-muted">
      <span>{{ result.language }}</span><span aria-hidden="true">·</span><ShowtimeFormat :format="result.format" />
      <template v-if="result.room"><span aria-hidden="true">·</span><span>{{ formatRoom(result.room) }}</span></template>
    </div>
    <BookingLink
      :url="result.bookingUrl"
      :provider="result.provider"
      :aria-label="bookingLabel()"
      :data-showtime-id="result.showtimeId"
      unstyled
      class="mt-auto inline-flex min-h-11 items-end pt-3 font-mono text-[10px] font-black uppercase tracking-[0.1em]"
      available-class="relative z-20 text-ink underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ink focus-visible:ring-offset-2"
      unavailable-class="pointer-events-none text-muted"
    >
      <template #default="{ available }">{{ available ? 'Réserver' : 'Réservation indisponible' }}</template>
    </BookingLink>
  </article>
</template>
