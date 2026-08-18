<script setup lang="ts">
import { Building2, CalendarRange, Clapperboard, Film, Info, MapPin, Search } from '@lucide/vue'

const route = useRoute()
const { favoriteTheaters, favoriteTheaterIds, isInitialized, isLoading, initialize } = useCinemaPreferences()

const links = [
  { to: '/', label: 'Planning', icon: CalendarRange },
  { to: '/recherche', label: 'Trouver une séance', icon: Search },
  { to: '/films', label: 'Films', icon: Film },
  { to: '/cinemas', label: 'Mes cinémas', icon: Building2 },
  { to: '/credits', label: 'Crédits', icon: Info }
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
  <aside class="sticky top-0 hidden h-screen flex-col border-r border-line bg-surface px-4 py-6 lg:flex">
    <NuxtLink to="/" class="flex items-center gap-2.5 px-2 text-lg font-bold tracking-tight text-ink" aria-label="MesSeances, accueil">
      <span class="grid size-8 place-items-center rounded-md bg-primary text-white">
        <Clapperboard :size="18" aria-hidden="true" />
      </span>
      <span>MesSeances</span>
    </NuxtLink>

    <nav aria-label="Navigation principale" class="mt-10">
      <p class="px-3 text-sm text-muted">Navigation</p>
      <div class="mt-2 space-y-1">
        <NuxtLink
          v-for="link in links"
          :key="link.to"
          :to="link.to"
          class="flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition hover:bg-subtle hover:text-ink"
          :class="isActive(link.to) ? 'bg-subtle text-ink' : 'text-muted'"
          :aria-current="isActive(link.to) ? 'page' : undefined"
        >
          <component :is="link.icon" :size="17" aria-hidden="true" />
          <span>{{ link.label }}</span>
        </NuxtLink>
      </div>
    </nav>

    <NuxtLink to="/cinemas" class="mt-auto flex items-center gap-2 border-t border-line px-3 pt-5 text-sm text-muted transition hover:text-ink" :aria-label="`Gérer mes cinémas, ${favoriteSummary}`">
      <MapPin :size="16" class="shrink-0 text-accent" aria-hidden="true" />
      <span class="truncate">{{ favoriteSummary }}</span>
    </NuxtLink>
  </aside>

  <header class="sticky top-0 z-30 border-b border-line bg-surface lg:hidden">
    <div class="flex h-14 items-center justify-between gap-4 px-4 sm:px-6">
      <NuxtLink to="/" class="flex items-center gap-2 text-base font-bold tracking-tight text-ink" aria-label="MesSeances, accueil">
        <span class="grid size-8 place-items-center rounded-md bg-primary text-white">
          <Clapperboard :size="18" aria-hidden="true" />
        </span>
        <span>MesSeances</span>
      </NuxtLink>
      <NuxtLink to="/cinemas" class="flex min-w-0 items-center gap-1.5 text-sm text-muted transition hover:text-ink" :aria-label="`Gérer mes cinémas, ${favoriteSummary}`">
        <MapPin :size="15" class="text-accent" aria-hidden="true" />
        <span class="max-w-36 truncate">{{ favoriteSummary }}</span>
      </NuxtLink>
    </div>
    <nav aria-label="Navigation principale" class="grid grid-cols-5 border-t border-line px-1 sm:px-4">
      <NuxtLink
        v-for="link in links"
        :key="link.to"
        :to="link.to"
        class="flex min-h-12 items-center justify-center gap-1.5 border-b-2 px-1 text-center text-xs font-medium leading-tight transition sm:gap-2 sm:px-2 sm:text-sm"
        :class="isActive(link.to) ? 'border-accent text-ink' : 'border-transparent text-muted hover:text-ink'"
        :aria-current="isActive(link.to) ? 'page' : undefined"
      >
        <component :is="link.icon" :size="16" aria-hidden="true" />
        <span>{{ link.label }}</span>
      </NuxtLink>
    </nav>
  </header>
</template>
