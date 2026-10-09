<script setup lang="ts">
import type { HistoryChainRank } from '~/types/api'
import { statisticsChainLabels, statisticsCount } from '~/utils/statistics'

withDefaults(
  defineProps<{
    rows: HistoryChainRank[]
    showMovieCount?: boolean
  }>(),
  { showMovieCount: true },
)
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
        Circuits classés par séances{{ showMovieCount ? ' puis films' : '' }},
        par ordre décroissant.
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
          <th
            v-if="showMovieCount"
            scope="col"
            class="px-3 py-3 text-right font-extrabold"
          >
            Films
          </th>
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
            <NuxtLink
              :to="`/cinemas?chains=${row.chain}`"
              class="inline-flex items-center underline decoration-2 underline-offset-4 hover:text-primary focus-visible:outline-solid focus-visible:outline-3 focus-visible:outline-offset-3 focus-visible:outline-ink"
            >
              <TheaterName
                :name="statisticsChainLabels[row.chain]"
                :provider="row.chain"
                class="grid grid-cols-[4rem_minmax(0,1fr)] items-center gap-3"
                logo-class="justify-self-center"
              />
            </NuxtLink>
          </th>
          <td class="px-3 py-4 text-right font-mono tabular-nums">
            {{ statisticsCount(row.showtime_count) }}
          </td>
          <td
            v-if="showMovieCount"
            class="px-3 py-4 text-right font-mono tabular-nums"
          >
            {{ statisticsCount(row.movie_count) }}
          </td>
          <td class="px-3 py-4 text-right font-mono tabular-nums">
            {{ statisticsCount(row.theater_count) }}
          </td>
        </tr>
        <tr v-if="!rows.length">
          <td :colspan="showMovieCount ? 4 : 3" class="p-4">
            Aucune donnée pour ces filtres.
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
