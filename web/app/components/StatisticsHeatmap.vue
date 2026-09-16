<script setup lang="ts">
import type { StatisticsHeatmapCell } from '~/types/api'
import { statisticsCount, statisticsHeatmapColors, statisticsHeatmapRows, statisticsHeatmapStyle, statisticsHours } from '~/utils/statistics'

const props = defineProps<{ cells: StatisticsHeatmapCell[] }>()
const rows = computed(() => statisticsHeatmapRows(props.cells))
const maximum = computed(() => props.cells.reduce((max, cell) => Math.max(max, cell.showtime_count), 0))
</script>

<template>
  <div class="max-w-full overflow-x-auto focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink" role="region" aria-label="Séances par jour et heure, tableau défilant" tabindex="0">
    <table class="w-full border-separate border-spacing-1 text-center font-mono text-xs tabular-nums">
      <caption class="caption-bottom pt-4 text-left font-sans text-sm leading-relaxed">
        <span class="mb-3 flex flex-wrap items-center gap-2 text-xs">
          <span>0 séance</span>
          <span class="flex gap-0.5" aria-hidden="true"><span v-for="color in statisticsHeatmapColors" :key="color" class="size-4 border border-ink/10" :style="{ backgroundColor: color }" /></span>
          <span>{{ statisticsCount(maximum) }} séances · Échelle racine carrée</span>
        </span>
        Nombre de séances par jour et heure. Heures Europe/Paris, de 08 h à 07 h. Après minuit, les séances restent rattachées au jour de programmation précédent. Les semaines sont additionnées, sans moyenne ; les heures répétées au changement d’heure sont cumulées.
      </caption>
      <thead><tr><th scope="col" class="p-2 text-left">Jour</th><th v-for="hour in statisticsHours" :key="hour" scope="col" class="min-w-11 p-2">{{ String(hour).padStart(2, '0') }} h</th></tr></thead>
      <tbody>
        <tr v-for="row in rows" :key="row.label">
          <th scope="row" class="pr-3 text-left font-bold">{{ row.label }}</th>
          <td v-for="cell in row.cells" :key="cell.hour" class="h-11 px-2" :style="statisticsHeatmapStyle(cell.count, maximum)">{{ statisticsCount(cell.count) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
