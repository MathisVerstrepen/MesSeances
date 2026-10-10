<script setup lang="ts">
import type { MovieShowtimesResponse } from '~/types/api'
import { formatLongDate } from '~/utils/date'
import { formatShowtimeCount } from '~/utils/formats'

defineProps<{
  discovery: MovieShowtimesResponse['discovery']
  collapsible?: boolean
}>()
</script>

<template>
  <section
    v-if="discovery.window && discovery.cities.length"
    class="mt-8 border-t-2 border-ink pt-6 sm:mt-12"
    aria-labelledby="film-discovery-heading"
  >
    <component :is="collapsible ? 'details' : 'div'">
      <component
        :is="collapsible ? 'summary' : 'div'"
        :class="collapsible ? 'min-h-11 cursor-pointer py-1 hover:text-primary marker:text-primary' : undefined"
      >
        <h2
          id="film-discovery-heading"
          class="font-black tracking-tight"
          :class="collapsible ? 'inline text-lg sm:text-xl' : 'text-2xl sm:text-3xl'"
        >
          Où voir ce film du
          {{ formatLongDate(discovery.window.from) }} au
          {{ formatLongDate(discovery.window.through) }}
        </h2>
      </component>
      <ul class="mt-5 grid gap-x-8 sm:grid-cols-2 lg:grid-cols-3">
        <li
          v-for="city in discovery.cities"
          :key="city.slug"
          class="border-b-2 border-ink"
        >
          <NuxtLink
            :to="`/ville/${encodeURIComponent(city.slug)}/cinemas`"
            class="flex min-h-11 flex-wrap items-baseline justify-between gap-x-4 gap-y-1 py-3 font-bold hover:text-primary"
          >
            <span class="break-words">{{ city.name }}</span>
            <span class="text-sm"
              >{{ city.theater_count }}
              cinéma{{ city.theater_count > 1 ? 's' : '' }}
              · {{ formatShowtimeCount(city.showtime_count) }}</span
            >
          </NuxtLink>
        </li>
      </ul>
    </component>
  </section>
</template>
