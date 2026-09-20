<script setup lang="ts">
import type { StatisticsResponse } from '~/types/api'
import {
  nextStatisticsSort,
  statisticsCityName,
  statisticsCount,
  statisticsLocalPage,
  type StatisticsLocalColumn,
  type StatisticsLocalSort,
} from '~/utils/statistics'

const props = defineProps<{
  local: StatisticsResponse['local']
  limits?: { cities: boolean; theaters: boolean }
}>()
const mode = ref<'cities' | 'theaters'>('cities')
const sort = ref<StatisticsLocalSort | null>(null)
const page = ref(1)
const columns = computed<{ key: StatisticsLocalColumn; label: string }[]>(
  () => [
    { key: 'name', label: mode.value === 'cities' ? 'Ville' : 'Cinéma' },
    { key: 'movie_count', label: 'Films' },
    { key: 'showtime_count', label: 'Séances' },
    mode.value === 'cities'
      ? { key: 'theater_count', label: 'Cinémas' }
      : { key: 'city', label: 'Ville' },
  ],
)
const result = computed(() =>
  statisticsLocalPage(props.local[mode.value], sort.value, page.value),
)
watch(
  () => props.local,
  () => {
    page.value = 1
  },
)
watch(mode, () => {
  sort.value = null
  page.value = 1
})
function sortBy(column: StatisticsLocalColumn) {
  sort.value = nextStatisticsSort(sort.value, column)
  page.value = 1
}
const buttonClass =
  'min-h-11 border-2 border-ink px-4 py-2 text-sm font-extrabold hover:bg-highlight focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink disabled:cursor-not-allowed disabled:opacity-40'
</script>

<template>
  <div>
    <fieldset class="mb-6 flex flex-wrap gap-4">
      <legend class="sr-only">
        Afficher l’offre locale par ville ou cinéma
      </legend>
      <label
        v-for="option in [{ value: 'cities', label: 'Villes' }, { value: 'theaters', label: 'Cinémas' }]"
        :key="option.value"
        class="flex min-h-11 cursor-pointer items-center gap-2 font-extrabold"
      >
        <input
          v-model="mode"
          type="radio"
          name="statistics-local-mode"
          :value="option.value"
          class="size-5 accent-ink focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
        >{{ option.label }}
      </label>
    </fieldset>
    <p v-if="limits?.[mode]" class="mb-5 text-sm">
      <strong>{{
        mode === 'cities' ? '100 premières villes' : '100 premiers cinémas'
      }}</strong
      >. Tri et pagination limités à ces 100 résultats, classés initialement par
      séances puis films. Les totaux portent sur tous les résultats filtrés.
    </p>
    <div
      class="max-w-full overflow-x-auto focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
      role="region"
      :aria-label="`Offre par ${mode === 'cities' ? 'ville' : 'cinéma'}, tableau défilant`"
      tabindex="0"
    >
      <table class="w-full min-w-[36rem] border-collapse text-left text-sm">
        <caption class="sr-only">
          Offre locale. Activer un en-tête pour trier les résultats affichés.
          Tri initial : séances puis films, par ordre décroissant.
        </caption>
        <thead class="border-y-2 border-ink bg-[#e8e6de]">
          <tr>
            <th
              v-for="column in columns"
              :key="column.key"
              scope="col"
              :aria-sort="sort?.column === column.key ? sort.direction : !sort && column.key === 'showtime_count' ? 'descending' : 'none'"
              class="px-3"
            >
              <button
                type="button"
                class="min-h-11 py-2 font-extrabold underline decoration-dotted underline-offset-4 focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
                @click="sortBy(column.key)"
              >
                {{ column.label }}
                <span aria-hidden="true">{{
                  sort?.column === column.key ? sort.direction === 'ascending' ? '↑' : '↓' : !sort && column.key === 'showtime_count' ? '↓' : '↕'
                }}</span>
              </button>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in result.rows"
            :key="'id' in row ? row.id : row.slug"
            class="border-b border-ink/20"
          >
            <th scope="row" class="max-w-96 px-3 py-4 font-bold">
              {{
                'theater_count' in row ? statisticsCityName(row.name) : row.name
              }}
            </th>
            <td class="px-3 py-4 font-mono tabular-nums">
              {{ statisticsCount(row.movie_count) }}
            </td>
            <td class="px-3 py-4 font-mono tabular-nums">
              {{ statisticsCount(row.showtime_count) }}
            </td>
            <td
              class="px-3 py-4"
              :class="'theater_count' in row ? 'font-mono tabular-nums' : ''"
            >
              {{
                'theater_count' in row ? statisticsCount(row.theater_count) : statisticsCityName(row.city)
              }}
            </td>
          </tr>
          <tr v-if="!result.rows.length">
            <td colspan="4" class="p-4">Aucune donnée pour ces filtres.</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="mt-5 flex flex-wrap items-center justify-between gap-3">
      <p class="text-sm" role="status">
        {{ statisticsCount(result.total) }}
        {{ mode === 'cities' ? 'villes' : 'cinémas' }} · Page
        {{ result.page }} sur {{ result.pages }}
      </p>
      <nav
        v-if="result.pages > 1"
        class="flex gap-2"
        aria-label="Pagination de l’offre locale"
      >
        <button
          type="button"
          :class="buttonClass"
          :disabled="result.page === 1"
          @click="page = result.page - 1"
        >
          Précédent
        </button>
        <button
          type="button"
          :class="buttonClass"
          :disabled="result.page === result.pages"
          @click="page = result.page + 1"
        >
          Suivant
        </button>
      </nav>
    </div>
  </div>
</template>
