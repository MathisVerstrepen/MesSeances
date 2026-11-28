<script setup lang="ts">
import { ArrowRight, MapPin } from '@lucide/vue'
import type { CitiesResponse } from '~/types/api'

const CITY_LIMIT = 12

const api = useMesSeancesApi()
const { data: cities } = await useAsyncData('home-city-links', async () => {
  try {
    return await api.cities()
  } catch {
    // The homepage stays useful without this block.
    const empty: CitiesResponse = { generated_at: '', items: [] }
    return empty
  }
})

// Largest French cities by population, the likeliest searches.
const MAJOR_CITY_SLUGS = [
  'paris',
  'marseille',
  'lyon',
  'toulouse',
  'nice',
  'nantes',
  'montpellier',
  'strasbourg',
  'bordeaux',
  'lille',
  'rennes',
  'toulon',
  'reims',
  'saint-etienne',
  'le-havre',
  'grenoble',
  'dijon',
  'angers',
]

// Major cities we cover, then the cities with the most cinemas.
const topCities = computed(() => {
  const items = cities.value?.items ?? []
  const bySlug = new Map(items.map((city) => [city.slug, city]))
  const major = MAJOR_CITY_SLUGS.flatMap((slug) => {
    const city = bySlug.get(slug)
    return city ? [city] : []
  })
  const others = items
    .filter((city) => !MAJOR_CITY_SLUGS.includes(city.slug))
    .sort(
      (left, right) =>
        right.theaters.length - left.theaters.length ||
        left.name.localeCompare(right.name, 'fr'),
    )
  return [...major, ...others].slice(0, CITY_LIMIT)
})
</script>

<template>
  <section
    v-if="topCities.length"
    class="border-b-2 border-ink bg-surface"
    aria-labelledby="home-cities-title"
  >
    <div class="mx-auto max-w-[1440px] px-4 py-10 sm:px-6 lg:px-10 lg:py-14">
      <div
        class="flex flex-wrap items-end justify-between gap-4 border-b-2 border-ink pb-4"
      >
        <h2
          id="home-cities-title"
          class="text-4xl font-black tracking-[-0.05em] sm:text-5xl"
        >
          Séances par ville
        </h2>
        <NuxtLink
          to="/cinemas"
          class="inline-flex items-center gap-2 font-mono text-[0.7rem] font-black uppercase tracking-[0.1em] hover:text-primary"
        >
          Toutes les villes <ArrowRight :size="16" aria-hidden="true" />
        </NuxtLink>
      </div>
      <ul
        class="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-6"
      >
        <li v-for="city in topCities" :key="city.slug">
          <NuxtLink
            :to="`/ville/${encodeURIComponent(city.slug)}`"
            class="flex min-h-16 items-center gap-3 border-2 border-ink bg-[#f8f7f2] px-3 py-2 shadow-[4px_4px_0_#27272a] transition-colors hover:bg-highlight"
          >
            <MapPin
              :size="18"
              class="shrink-0 text-primary"
              aria-hidden="true"
            />
            <span class="min-w-0">
              <span class="block truncate font-black">{{ city.name }}</span>
              <span
                class="block font-mono text-[10px] font-bold uppercase tracking-[0.08em] text-muted"
                >{{ city.theaters.length }}
                cinéma{{ city.theaters.length === 1 ? '' : 's' }}</span
              >
            </span>
          </NuxtLink>
        </li>
      </ul>
    </div>
  </section>
</template>
