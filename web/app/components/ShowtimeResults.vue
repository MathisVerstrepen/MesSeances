<script setup lang="ts">
import type { ResultGrouping, ResultLayout, ShowtimeResultScope, ShowtimeResultViewModel } from '~/types/showtimeResults'
import { groupShowtimeResults, sortShowtimeResults } from '~/utils/showtimeResults'

const props = defineProps<{
  results: ShowtimeResultViewModel[]
  grouping: ResultGrouping
  layout: ResultLayout
  scope: ShowtimeResultScope
}>()

const sortedResults = computed(() => sortShowtimeResults(props.results))
const movieGroups = computed(() => groupShowtimeResults(sortedResults.value))
</script>

<template>
  <div v-if="grouping === 'movie'" :class="scope === 'single-theater' ? 'mt-8 space-y-8' : 'space-y-4'">
    <ShowtimeMovieGroup v-for="group in movieGroups" :key="group.key" :results="group.results" :layout="layout" :scope="scope" />
  </div>
  <div v-else-if="layout === 'lines'" :class="['divide-y-2 divide-ink border-2 border-ink bg-surface shadow-[6px_6px_0_#27272a]', scope === 'single-theater' ? 'mt-8' : '']" aria-label="Séances par ordre chronologique">
    <ShowtimeResultLine v-for="result in sortedResults" :key="result.key" :result="result" :scope="scope" :show-movie="true" />
  </div>
  <ul v-else :class="['grid grid-cols-[repeat(auto-fill,minmax(150px,1fr))] gap-3 sm:grid-cols-[repeat(auto-fill,minmax(180px,1fr))] sm:gap-4 lg:grid-cols-[repeat(auto-fill,minmax(210px,1fr))]', scope === 'single-theater' ? 'mt-8' : '']" :aria-label="scope === 'single-theater' ? 'Séances par ordre chronologique' : 'Séances compatibles par ordre chronologique'">
    <li v-for="result in sortedResults" :key="result.key" class="min-w-0"><ShowtimeResultBox :result="result" :scope="scope" :show-movie="true" /></li>
  </ul>
</template>
