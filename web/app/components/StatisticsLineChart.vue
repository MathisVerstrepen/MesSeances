<script setup lang="ts">
import type { StatisticsDailyShowtimes } from '~/types/api'
import { statisticsCount } from '~/utils/statistics'
import {
  statisticsLineChart,
  statisticsNearestPoint,
  statisticsWeekLabels,
} from '~/utils/statisticsLineChart'

const props = defineProps<{
  rows: StatisticsDailyShowtimes[]
  today: string
  film?: boolean
  frenchReleaseDate?: string | null
}>()
const chart = computed(() => statisticsLineChart(props.rows, props))
const plot = ref<HTMLElement | null>(null)
const width = ref(240)
const labels = computed(() =>
  statisticsWeekLabels(chart.value.weeks, width.value),
)
const activeIndex = ref(0)
const hoveredIndex = ref<number | null>(null)
const focused = ref(false)
const dismissed = ref(false)
const activePoint = computed(() =>
  dismissed.value
    ? null
    : chart.value.points[
        focused.value ? activeIndex.value : (hoveredIndex.value ?? -1)
      ],
)
const id = useId()
let observer: ResizeObserver | undefined
onMounted(() => {
  if (!plot.value) return
  observer = new ResizeObserver(([entry]) => {
    if (entry) width.value = entry.contentRect.width
  })
  observer.observe(plot.value)
})
onBeforeUnmount(() => observer?.disconnect())
watch(
  () => props.rows,
  () => {
    activeIndex.value = 0
    hoveredIndex.value = null
    dismissed.value = false
  },
)
function inspectPointer(event: PointerEvent) {
  if (!plot.value) return
  const bounds = plot.value.getBoundingClientRect()
  const index = statisticsNearestPoint(
    chart.value.points,
    ((event.clientX - bounds.left) / bounds.width) * 100,
  )
  if (hoveredIndex.value !== index) dismissed.value = false
  hoveredIndex.value = index
  activeIndex.value = index
}
function inspectKey(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    dismissed.value = true
    return
  }
  const last = chart.value.points.length - 1
  const next =
    event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? last
        : event.key === 'ArrowRight'
          ? Math.min(last, activeIndex.value + 1)
          : event.key === 'ArrowLeft'
            ? Math.max(0, activeIndex.value - 1)
            : null
  if (next === null) return
  event.preventDefault()
  activeIndex.value = next
  dismissed.value = false
}
</script>

<template>
  <div class="min-w-0">
    <p v-if="chart.points.length === 0" class="py-6 text-sm font-semibold">
      Aucune donnée pour ces filtres.
    </p>
    <template v-else>
      <div>
        <p class="mb-5 font-mono text-xs font-bold" aria-hidden="true">
          Séances
        </p>
        <div
          class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 pr-1 font-mono text-xs tabular-nums sm:gap-x-5"
        >
          <div></div>
          <div
            v-if="chart.today"
            class="relative h-8 text-xs font-extrabold"
            aria-hidden="true"
          >
            <span
              class="absolute whitespace-nowrap text-primary-hover"
              :class="chart.today.x < 20 ? '' : chart.today.x > 80 ? '-translate-x-full' : '-translate-x-1/2'"
              :style="{ left: `${chart.today.x}%` }"
              >Aujourd’hui</span
            >
          </div>
          <div v-else></div>
          <div></div>
          <div
            v-if="chart.weeks.length"
            class="relative h-8 font-sans text-xs font-semibold"
            aria-hidden="true"
          >
            <span
              v-for="marker in labels"
              :key="marker.date"
              data-week-label
              class="absolute -translate-x-1/2 whitespace-nowrap"
              :style="{ left: `${marker.labelX}%` }"
              :title="marker.description"
              >{{
                marker.label
              }}</span
            >
          </div>
          <div v-else></div>
          <div class="relative h-64" aria-hidden="true">
            <span class="invisible">{{ statisticsCount(chart.ceiling) }}</span>
            <span
              v-for="tick in chart.ticks"
              :key="tick.count"
              class="absolute right-0 -translate-y-1/2"
              :style="{ top: `${tick.y}%` }"
              >{{
                statisticsCount(tick.count)
              }}</span
            >
          </div>
          <div
            ref="plot"
            class="relative h-64 min-w-0 cursor-crosshair focus-visible:outline-3 focus-visible:outline-offset-8 focus-visible:outline-ink focus-visible:outline-solid"
            role="group"
            tabindex="0"
            aria-label="Évolution du nombre de séances par jour"
            :aria-describedby="`${id}-instructions ${id}-annotations`"
            @pointermove="inspectPointer"
            @pointerdown="inspectPointer"
            @pointerleave="hoveredIndex = null"
            @focus="focused = true; dismissed = false"
            @blur="focused = false; hoveredIndex = null"
            @keydown="inspectKey"
          >
            <svg
              class="h-full w-full overflow-visible text-ink"
              aria-hidden="true"
              focusable="false"
            >
              <line
                v-for="tick in chart.ticks"
                :key="tick.count"
                x1="0%"
                x2="100%"
                :y1="`${tick.y}%`"
                :y2="`${tick.y}%`"
                stroke="currentColor"
                :stroke-opacity="tick.count === 0 ? 1 : 0.15"
              />
              <line
                v-for="marker in chart.weeks"
                :key="marker.date"
                data-week-marker
                :data-date="marker.date"
                :x1="`${marker.x}%`"
                :x2="`${marker.x}%`"
                y1="0%"
                y2="100%"
                stroke="currentColor"
                stroke-opacity="0.25"
                stroke-dasharray="3 5"
              />
              <line
                v-if="chart.today"
                data-today-marker
                :data-date="chart.today.date"
                :x1="`${chart.today.x}%`"
                :x2="`${chart.today.x}%`"
                y1="0%"
                y2="100%"
                class="text-primary-hover"
                stroke="currentColor"
                stroke-width="2"
                stroke-dasharray="8 4"
              />
              <!-- Stretch only the line geometry; labels and markers keep their pixel size. -->
              <svg
                viewBox="0 0 100 100"
                preserveAspectRatio="none"
                width="100%"
                height="100%"
                class="overflow-visible"
              >
                <polyline
                  v-if="chart.points.length > 1"
                  :points="chart.line"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="3"
                  stroke-linejoin="round"
                  stroke-linecap="round"
                  vector-effect="non-scaling-stroke"
                />
              </svg>
              <circle
                v-for="point in chart.points"
                :key="point.date"
                :cx="`${point.x}%`"
                :cy="`${point.y}%`"
                :r="chart.points.length === 1 ? 5 : 3"
                fill="currentColor"
              >
                <title>
                  {{ point.label }} :
                  {{ statisticsCount(point.showtime_count) }}
                  {{ point.showtime_count === 1 ? 'séance' : 'séances' }}
                </title>
              </circle>
              <circle
                v-if="activePoint"
                :cx="`${activePoint.x}%`"
                :cy="`${activePoint.y}%`"
                r="7"
                fill="var(--color-surface)"
                stroke="currentColor"
                stroke-width="3"
              />
            </svg>
            <div
              :id="`${id}-value`"
              :aria-live="focused ? 'polite' : 'off'"
              aria-atomic="true"
            >
              <div
                v-if="activePoint"
                role="tooltip"
                class="pointer-events-none absolute z-10 w-48 max-w-full border-2 border-ink bg-surface px-3 py-2 font-sans text-xs shadow-[3px_3px_0_#27272a]"
                :style="{ left: `clamp(0px, calc(${activePoint.x}% - 6rem), calc(100% - 12rem))`, top: activePoint.y < 40 ? `calc(${activePoint.y}% + 14px)` : `calc(${activePoint.y}% - 76px)` }"
              >
                <time :datetime="activePoint.date">{{
                  activePoint.label
                }}</time>
                <p class="mt-1 text-sm font-extrabold">
                  {{ statisticsCount(activePoint.showtime_count) }}
                  {{ activePoint.showtime_count === 1 ? 'séance' : 'séances' }}
                </p>
              </div>
            </div>
          </div>
          <div></div>
          <div class="relative mt-4 h-8" aria-hidden="true">
            <time
              v-for="point in chart.dateTicks"
              :key="point.date"
              :datetime="point.date"
              class="absolute whitespace-nowrap"
              :class="[
                point.x === 0 ? '' : point.x === 100 ? '-translate-x-full' : '-translate-x-1/2',
                chart.points.length > 1 && point.x > 0 && point.x < 100 ? 'hidden sm:block' : '',
              ]"
              :style="{ left: `${point.x}%` }"
              >{{
                point.shortLabel
              }}</time
            >
          </div>
        </div>
      </div>
      <p :id="`${id}-instructions`" class="sr-only">
        Flèches gauche et droite : parcourir les jours. Début et Fin : premier
        et dernier jour. Échap : fermer l’infobulle. Données exactes dans le
        tableau suivant.
      </p>
      <ul :id="`${id}-annotations`" class="sr-only">
        <li v-if="chart.today">Aujourd’hui : {{ chart.today.label }}.</li>
        <li v-for="marker in chart.weeks" :key="marker.date">
          {{ marker.description }}.
        </li>
      </ul>
      <details class="mt-4">
        <summary
          class="w-fit min-h-11 cursor-pointer py-3 text-sm font-extrabold underline underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
        >
          Voir les données par jour
        </summary>
        <div
          class="mt-3 max-h-80 overflow-auto focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
          role="region"
          aria-label="Séances par jour, tableau défilant"
          tabindex="0"
        >
          <table class="w-full text-sm tabular-nums">
            <caption class="sr-only">
              Nombre de séances par jour de programmation
            </caption>
            <thead class="sticky top-0 bg-[#f8f7f2]">
              <tr class="border-b-2 border-ink">
                <th scope="col" class="py-3 pr-4 text-left">Date</th>
                <th scope="col" class="py-3 text-right">Séances</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="point in chart.points"
                :key="point.date"
                class="border-b border-ink/15"
              >
                <th scope="row" class="py-3 pr-4 text-left font-semibold">
                  <time :datetime="point.date">{{ point.label }}</time>
                </th>
                <td class="py-3 text-right font-mono">
                  {{ statisticsCount(point.showtime_count) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </details>
    </template>
  </div>
</template>
