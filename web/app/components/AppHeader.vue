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
  <header class="sticky top-0 z-30 border-b-2 border-ink bg-[#f8f7f2]/95 backdrop-blur-sm">
    <div class="mx-auto flex min-h-16 max-w-[1440px] flex-wrap items-center px-4 sm:px-6 lg:min-h-[4.5rem] lg:flex-nowrap lg:px-10">
      <NuxtLink to="/" class="brand-link flex h-16 shrink-0 items-center gap-2.5 font-black tracking-[-0.04em] text-ink lg:h-[4.5rem]" aria-label="MesSeances, accueil">
        <span class="grid size-9 rotate-[-2deg] place-items-center rounded-[3px] border-2 border-ink bg-[#ffcf3f] shadow-[3px_3px_0_#27272a]">
          <Clapperboard :size="19" stroke-width="2.5" aria-hidden="true" />
        </span>
        <span class="text-lg">MesSeances<span class="text-primary">.</span></span>
      </NuxtLink>

      <NuxtLink
        to="/cinemas"
        class="order-2 ml-auto flex min-w-0 items-center gap-1.5 border-l-2 border-ink pl-3 font-mono text-[10px] font-bold uppercase tracking-[0.08em] text-ink transition-colors hover:text-primary lg:order-none lg:ml-6 lg:pl-4"
        :aria-label="`Gérer mes cinémas, ${favoriteSummary}`"
      >
        <MapPin :size="15" class="shrink-0 text-primary" stroke-width="2.5" aria-hidden="true" />
        <span class="max-w-28 truncate sm:max-w-44">{{ favoriteSummary }}</span>
      </NuxtLink>

      <nav aria-label="Navigation principale" class="order-3 -mx-4 grid w-[calc(100%+2rem)] grid-cols-4 border-t-2 border-ink sm:-mx-6 sm:w-[calc(100%+3rem)] lg:order-none lg:ml-auto lg:flex lg:w-auto lg:border-t-0">
        <NuxtLink
          v-for="link in links"
          :key="link.to"
          :to="link.to"
          class="nav-link relative flex min-h-14 flex-col items-center justify-center gap-0.5 border-r border-ink/20 px-1 text-center text-[9px] font-extrabold uppercase leading-tight tracking-[-0.01em] transition-colors last:border-r-0 sm:px-3 sm:text-[10px] lg:min-h-[4.5rem] lg:flex-row lg:gap-2 lg:border-r-0 lg:px-4 lg:text-xs"
          :class="isActive(link.to) ? 'bg-ink text-white' : 'text-ink hover:bg-[#d7ff38]'"
          :aria-current="isActive(link.to) ? 'page' : undefined"
        >
          <component :is="link.icon" :size="15" stroke-width="2.5" aria-hidden="true" />
          <span>{{ link.label }}</span>
        </NuxtLink>
      </nav>
    </div>
  </header>
</template>

<style scoped>
@media (min-width: 1024px) {
  .nav-link[aria-current='page']::after {
    position: absolute;
    right: 1rem;
    bottom: 0.55rem;
    left: 1rem;
    height: 3px;
    background: #d7ff38;
    content: '';
  }
}
</style>
