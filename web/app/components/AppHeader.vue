<script setup lang="ts">
import { Building2, CalendarRange, Clapperboard, Film, MapPin, Search } from '@lucide/vue'

const route = useRoute()
const { favoriteTheaters, favoriteTheaterIds, isInitialized, isLoading, initialize } = useCinemaPreferences()

const links = [
  { to: '/planning', label: 'Planning', icon: CalendarRange },
  { to: '/recherche', label: 'Trouver une séance', icon: Search },
  { to: '/films', label: 'Films', icon: Film },
  { to: '/cinemas', label: 'Mes cinémas', icon: Building2 }
]

const favoriteSummary = computed(() => {
  if (!isInitialized.value && isLoading.value) return 'Chargement…'

  const count = favoriteTheaterIds.value.length
  const cities = [...new Set(favoriteTheaters.value.map((theater) => theater.city))]
  if (count === 0) return 'Mes cinémas'
  if (cities.length === 1) return `${cities[0]} · ${count}`
  return `${count} cinémas · ${cities.length} villes`
})

function isActive(to: string) {
  if (to === '/films') return route.path === '/films' || route.path.startsWith('/film/')
  return route.path === to
}

onMounted(() => {
  void initialize()
})
</script>

<template>
  <header class="sticky top-0 z-30 border-b border-line bg-surface">
    <div class="mx-auto flex min-h-14 max-w-[1440px] flex-wrap items-center justify-between px-4 sm:px-6 lg:min-h-16 lg:flex-nowrap lg:px-10">
      <NuxtLink to="/" class="flex h-14 shrink-0 items-center gap-2 text-base font-bold tracking-tight text-ink lg:h-16 lg:text-lg" aria-label="MesSeances, accueil">
        <span class="grid size-8 place-items-center rounded-md bg-primary text-white">
          <Clapperboard :size="18" aria-hidden="true" />
        </span>
        <span>MesSeances</span>
      </NuxtLink>

      <nav aria-label="Navigation principale" class="order-3 grid w-full grid-cols-4 border-t border-line lg:order-none lg:ml-auto lg:flex lg:w-auto lg:border-t-0">
        <NuxtLink
          v-for="link in links"
          :key="link.to"
          :to="link.to"
          class="flex min-h-12 items-center justify-center gap-1.5 border-b-2 px-1 text-center text-xs font-medium leading-tight transition sm:gap-2 sm:px-2 sm:text-sm lg:min-h-16 lg:px-3"
          :class="isActive(link.to) ? 'border-accent text-ink' : 'border-transparent text-muted hover:text-ink'"
          :aria-current="isActive(link.to) ? 'page' : undefined"
        >
          <component :is="link.icon" :size="16" aria-hidden="true" />
          <span>{{ link.label }}</span>
        </NuxtLink>
      </nav>

      <NuxtLink to="/cinemas" class="order-2 flex min-w-0 items-center gap-1.5 text-sm text-muted transition hover:text-ink lg:order-none lg:ml-4" :aria-label="`Gérer mes cinémas, ${favoriteSummary}`">
        <MapPin :size="15" class="text-accent" aria-hidden="true" />
        <span class="max-w-32 truncate sm:max-w-40">{{ favoriteSummary }}</span>
      </NuxtLink>
    </div>
  </header>
</template>
