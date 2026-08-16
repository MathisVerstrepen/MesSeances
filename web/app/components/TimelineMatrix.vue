<script setup lang="ts">
import { Clock3, Film, MapPin } from '@lucide/vue'
import type { TimelineResponse, TimelineShowtime, TimelineTheater } from '~/types/api'
import { formatParisTime, todayInParis } from '~/utils/date'

type TimelineMode = 'theater' | 'movie'
type FormatFilter = 'ALL' | 'STANDARD' | 'IMAX'
type TimelineZoom = 15 | 30 | 60
type PlacedShowtime = { showtime: TimelineShowtime; theater: TimelineTheater }
type PositionedShowtime = PlacedShowtime & { lane: number }
type TimelineRow = { id: string; label: string; secondary: string; height: number; showtimes: PositionedShowtime[] }

const props = defineProps<{
  timeline: TimelineResponse
  mode: TimelineMode
  formatFilter: FormatFilter
  zoom: TimelineZoom
}>()

const selected = ref<PlacedShowtime | null>(null)
const scroller = ref<HTMLElement | null>(null)
const now = ref(new Date())
let clockTimer: ReturnType<typeof setInterval> | undefined
let dragPointerId: number | null = null
let dragStartX = 0
let dragStartScroll = 0
let didDrag = false
let suppressClick = false

const pixelsPerMinute = computed(() => 60 / props.zoom)
const windowMinutes = computed(() => Math.max(1, Math.round((new Date(props.timeline.window_end_time).getTime() - new Date(props.timeline.window_start_time).getTime()) / 60_000)))
const railWidth = computed(() => windowMinutes.value * pixelsPerMinute.value)
const hourLabels = computed(() => {
  const start = new Date(props.timeline.window_start_time)
  return Array.from({ length: Math.floor(windowMinutes.value / 60) + 1 }, (_, index) => {
    const value = new Date(start.getTime() + index * 60 * 60_000)
    return { offset: index * 60, label: formatParisTime(value.toISOString()) }
  })
})

function matchesFormat(format: string) {
  if (props.formatFilter === 'ALL') return true
  if (props.formatFilter === 'IMAX') return format.toUpperCase() === 'IMAX'
  return format.toUpperCase() === '2D'
}

function createRow(id: string, label: string, secondary: string, items: PlacedShowtime[]): TimelineRow {
  const laneEnds: number[] = []
  const showtimes = [...items]
    .sort((a, b) => a.showtime.start_offset_minutes - b.showtime.start_offset_minutes || a.showtime.duration_minutes - b.showtime.duration_minutes)
    .map((item) => {
      const start = item.showtime.start_offset_minutes
      const lane = laneEnds.findIndex((end) => end <= start)
      const targetLane = lane === -1 ? laneEnds.length : lane
      laneEnds[targetLane] = start + item.showtime.duration_minutes
      return { ...item, lane: targetLane }
    })
  return { id, label, secondary, showtimes, height: 32 + Math.max(1, laneEnds.length) * 72 }
}

const rows = computed<TimelineRow[]>(() => {
  const placed = props.timeline.theaters.flatMap((theater) => theater.showtimes.filter((showtime) => matchesFormat(showtime.format)).map((showtime) => ({ showtime, theater })))
  if (props.mode === 'theater') {
    return props.timeline.theaters
      .map((theater) => createRow(theater.id, theater.name, theater.city, placed.filter((item) => item.theater.id === theater.id)))
      .filter((row) => row.showtimes.length > 0)
  }

  const movies = new Map<string, PlacedShowtime[]>()
  for (const item of placed) {
    const key = item.showtime.movie.slug
    const showtimes = movies.get(key)
    if (showtimes) showtimes.push(item)
    else movies.set(key, [item])
  }
  return [...movies.entries()].map(([id, showtimes]) => {
    const theaterCount = new Set(showtimes.map((item) => item.theater.id)).size
    return createRow(id, showtimes[0]!.showtime.movie.title, `${theaterCount} cinéma${theaterCount > 1 ? 's' : ''}`, showtimes)
  }).sort((a, b) => a.label.localeCompare(b.label, 'fr'))
})

watch(rows, (visibleRows) => {
  if (!selected.value) return
  const selectedId = selected.value.showtime.id
  const selectedTheaterId = selected.value.theater.id
  const isVisible = visibleRows.some((row) => row.showtimes.some((item) => item.showtime.id === selectedId && item.theater.id === selectedTheaterId))
  if (!isVisible) selected.value = null
}, { flush: 'sync' })

const currentTimeOffset = computed(() => {
  if (props.timeline.date !== todayInParis()) return null
  const offset = (now.value.getTime() - new Date(props.timeline.window_start_time).getTime()) / 60_000
  return offset >= 0 && offset <= windowMinutes.value ? offset : null
})

function selectShowtime(item: PlacedShowtime) {
  selected.value = item
}

function safeBookingUrl(url: string | null) {
  if (!url) return null
  try {
    const parsed = new URL(url)
    const hostname = parsed.hostname.toLowerCase()
    return parsed.protocol === 'https:' && (hostname === 'ugc.fr' || hostname.endsWith('.ugc.fr')) ? parsed.href : null
  } catch {
    return null
  }
}

function pointerDown(event: PointerEvent) {
  if (event.pointerType === 'touch' || event.button !== 0 || !window.matchMedia('(pointer: fine)').matches || !scroller.value) return
  dragPointerId = event.pointerId
  dragStartX = event.clientX
  dragStartScroll = scroller.value.scrollLeft
  didDrag = false
  scroller.value.setPointerCapture(event.pointerId)
}

function pointerMove(event: PointerEvent) {
  if (dragPointerId !== event.pointerId || !scroller.value) return
  const distance = event.clientX - dragStartX
  if (Math.abs(distance) > 4) didDrag = true
  if (!didDrag) return
  event.preventDefault()
  scroller.value.scrollLeft = dragStartScroll - distance
}

function pointerEnd(event: PointerEvent) {
  if (dragPointerId !== event.pointerId) return
  suppressClick = didDrag
  dragPointerId = null
  didDrag = false
  window.setTimeout(() => { suppressClick = false }, 0)
}

function captureClick(event: MouseEvent) {
  if (!suppressClick) return
  event.preventDefault()
  event.stopPropagation()
}

onMounted(() => {
  clockTimer = window.setInterval(() => { now.value = new Date() }, 60_000)
})

onBeforeUnmount(() => {
  if (clockTimer) window.clearInterval(clockTimer)
})
</script>

<template>
  <section aria-label="Frise des séances">
    <div v-if="rows.length === 0" class="state-panel">
      <Film :size="28" class="text-muted" aria-hidden="true" />
      <p>Aucune séance pour ce format.</p>
    </div>

    <div
      v-else
      ref="scroller"
      class="cursor-grab select-none overflow-x-auto rounded-lg border border-line bg-surface active:cursor-grabbing [--timeline-label-width:136px] [scrollbar-color:#a8a69f_#f0efeb] max-md:snap-x max-md:snap-mandatory sm:[--timeline-label-width:176px]"
      @pointerdown="pointerDown"
      @pointermove="pointerMove"
      @pointerup="pointerEnd"
      @pointercancel="pointerEnd"
      @click.capture="captureClick"
      @dragstart.prevent
    >
      <div class="relative" :style="{ width: `calc(var(--timeline-label-width) + ${railWidth}px)` }">
        <div class="relative h-12 border-b border-line bg-subtle">
          <div class="sticky left-0 z-30 flex h-full items-center border-r border-line bg-subtle px-3 text-xs font-semibold text-muted sm:px-4" style="width: var(--timeline-label-width)">
            {{ mode === 'theater' ? 'Cinémas' : 'Films' }}
          </div>
          <span
            v-for="hour in hourLabels"
            :key="hour.offset"
            class="absolute top-0 flex h-full -translate-x-1/2 snap-start items-center text-xs font-medium text-muted"
            :style="{ left: `calc(var(--timeline-label-width) + ${hour.offset * pixelsPerMinute}px)` }"
          >
            {{ hour.label }}
          </span>
        </div>

        <div
          v-for="row in rows"
          :key="row.id"
          class="relative border-b border-line last:border-b-0"
          :style="{
            height: `${row.height}px`,
            backgroundImage: `repeating-linear-gradient(to right, transparent 0, transparent ${15 * pixelsPerMinute - 1}px, rgba(222,221,215,0.45) ${15 * pixelsPerMinute}px), repeating-linear-gradient(to right, transparent 0, transparent ${60 * pixelsPerMinute - 1}px, rgba(190,188,180,0.65) ${60 * pixelsPerMinute}px)`,
            backgroundPosition: 'var(--timeline-label-width) 0, var(--timeline-label-width) 0'
          }"
        >
          <div class="sticky left-0 z-20 flex h-full flex-col justify-center border-r border-line bg-surface px-3 sm:px-4" style="width: var(--timeline-label-width)">
            <strong class="line-clamp-2 text-sm leading-snug text-ink">{{ row.label }}</strong>
            <span class="mt-1 flex items-center gap-1 text-xs text-muted">
              <MapPin v-if="mode === 'theater'" :size="12" aria-hidden="true" />
              <Film v-else :size="12" aria-hidden="true" />
              {{ row.secondary }}
            </span>
          </div>

          <button
            v-for="item in row.showtimes"
            :key="`${item.theater.id}-${item.showtime.id}`"
            type="button"
            class="absolute h-16 overflow-hidden rounded-md border px-2.5 py-2 text-left transition focus:z-30"
            :class="[
              item.showtime.language === 'VOSTFR' ? 'border-orange-200 bg-orange-50 text-orange-950 hover:border-accent' : 'border-stone-300 bg-stone-100 text-stone-900 hover:border-stone-500',
              selected?.showtime.id === item.showtime.id ? 'border-accent ring-1 ring-accent' : ''
            ]"
            :style="{ top: `${16 + item.lane * 72}px`, left: `calc(var(--timeline-label-width) + ${item.showtime.start_offset_minutes * pixelsPerMinute}px)`, width: `${Math.max(item.showtime.duration_minutes * pixelsPerMinute, 56)}px` }"
            :aria-label="`${item.showtime.movie.title}, ${item.theater.name}, ${formatParisTime(item.showtime.start_time)}, ${item.showtime.language}`"
            @click="selectShowtime(item)"
          >
            <span class="block truncate text-xs font-semibold">{{ mode === 'theater' ? item.showtime.movie.title : item.theater.name }}</span>
            <span class="mt-1 block truncate text-[11px] text-current opacity-70">{{ formatParisTime(item.showtime.start_time) }} · {{ item.showtime.language }} · {{ item.showtime.format }}</span>
          </button>
        </div>

        <div v-if="currentTimeOffset !== null" class="pointer-events-none absolute bottom-0 top-0 z-10 w-px bg-red-500" :style="{ left: `calc(var(--timeline-label-width) + ${currentTimeOffset * pixelsPerMinute}px)` }">
          <span class="absolute top-1 -translate-x-1/2 rounded bg-red-600 px-1.5 py-0.5 text-[10px] font-semibold text-white">Maintenant</span>
        </div>
      </div>
    </div>

    <div v-if="selected" class="mt-4 border-y border-line border-l-2 border-l-accent bg-surface px-4 py-4" aria-live="polite">
      <div class="grid gap-4 lg:grid-cols-[minmax(0,1fr)_auto_auto] lg:items-center">
        <div class="min-w-0">
          <p class="text-sm font-medium text-accent">Séance sélectionnée</p>
          <h2 class="mt-1 text-lg font-semibold text-ink">{{ selected.showtime.movie.title }}</h2>
          <p class="mt-1 text-sm text-muted">{{ selected.theater.name }} · {{ selected.showtime.room }} · {{ selected.showtime.language }} · {{ selected.showtime.format }}</p>
        </div>
        <div class="flex items-center gap-2 text-sm font-semibold text-ink">
          <Clock3 :size="18" class="text-accent" aria-hidden="true" />
          {{ formatParisTime(selected.showtime.start_time) }} → {{ formatParisTime(selected.showtime.end_time) }}
        </div>
        <BookingLink :url="safeBookingUrl(selected.showtime.booking_url)" />
      </div>
    </div>
  </section>
</template>
