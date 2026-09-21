<script setup lang="ts">
import type { StatisticsDailyShowtimes } from '~/types/api'
import { statisticsCount } from '~/utils/statistics'
import { statisticsLineChart } from '~/utils/statisticsLineChart'

const props = defineProps<{ rows: StatisticsDailyShowtimes[] }>()
const chart = computed(() => statisticsLineChart(props.rows))
</script>

<template>
  <div class="min-w-0">
    <p v-if="chart.points.length === 0" class="py-6 text-sm font-semibold">
      Aucune donnée pour ces filtres.
    </p>
    <template v-else>
      <div
        role="img"
        aria-label="Évolution du nombre de séances par jour. Données exactes dans le tableau suivant."
      >
        <p class="mb-5 font-mono text-xs font-bold" aria-hidden="true">
          Séances
        </p>
        <div
          class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 pr-1 font-mono text-xs tabular-nums sm:gap-x-5"
          aria-hidden="true"
        >
          <div class="relative h-64">
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
          <svg class="h-64 w-full overflow-visible text-ink" focusable="false">
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
                {{ point.label }} : {{ statisticsCount(point.showtime_count) }}
                {{ point.showtime_count === 1 ? 'séance' : 'séances' }}
              </title>
            </circle>
          </svg>
          <div></div>
          <div class="relative mt-4 h-8">
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
