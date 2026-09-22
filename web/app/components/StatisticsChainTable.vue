<script setup lang="ts">
import type { HistoryChainRank } from '~/types/api'
import { statisticsChainLabels, statisticsCount } from '~/utils/statistics'

defineProps<{
  rows: HistoryChainRank[]
}>()
</script>

<template>
  <div
    class="max-w-full overflow-x-auto focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
    role="region"
    aria-label="Statistiques par circuit, tableau défilant"
    tabindex="0"
  >
    <table class="w-full min-w-[36rem] border-collapse text-left text-sm">
      <caption class="sr-only">
        Circuits classés par séances puis films, par ordre décroissant.
      </caption>
      <thead class="border-y-2 border-ink bg-[#e8e6de]">
        <tr>
          <th scope="col" class="px-3 py-3 font-extrabold">Circuit</th>
          <th
            scope="col"
            aria-sort="descending"
            class="px-3 py-3 text-right font-extrabold"
          >
            Séances
          </th>
          <th scope="col" class="px-3 py-3 text-right font-extrabold">Films</th>
          <th scope="col" class="px-3 py-3 text-right font-extrabold">
            Cinémas
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.chain" class="border-b border-ink/20">
          <th
            scope="row"
            class="max-w-96 whitespace-normal px-3 py-4 font-bold"
          >
            <TheaterName
              :name="statisticsChainLabels[row.chain]"
              :provider="row.chain"
            />
          </th>
          <td class="px-3 py-4 text-right font-mono tabular-nums">
            {{ statisticsCount(row.showtime_count) }}
          </td>
          <td class="px-3 py-4 text-right font-mono tabular-nums">
            {{ statisticsCount(row.movie_count) }}
          </td>
          <td class="px-3 py-4 text-right font-mono tabular-nums">
            {{ statisticsCount(row.theater_count) }}
          </td>
        </tr>
        <tr v-if="!rows.length">
          <td colspan="4" class="p-4">Aucune donnée pour ces filtres.</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
