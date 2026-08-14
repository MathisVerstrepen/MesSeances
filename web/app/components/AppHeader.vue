<script setup lang="ts">
import { CalendarRange, Clapperboard, MapPin, Search } from '@lucide/vue'

const route = useRoute()

const links = [
  { to: '/', label: 'Planning', icon: CalendarRange },
  { to: '/recherche', label: 'Trouver une séance', icon: Search }
]
</script>

<template>
  <aside class="sticky top-0 hidden h-screen flex-col border-r border-line bg-surface px-4 py-6 lg:flex">
    <NuxtLink to="/" class="flex items-center gap-2.5 px-2 text-lg font-bold tracking-tight text-ink" aria-label="MovieFlow, accueil">
      <span class="grid size-8 place-items-center rounded-md bg-accent text-white">
        <Clapperboard :size="18" aria-hidden="true" />
      </span>
      <span>MovieFlow</span>
    </NuxtLink>

    <nav aria-label="Navigation principale" class="mt-10">
      <p class="px-3 text-sm text-muted">Navigation</p>
      <div class="mt-2 space-y-1">
        <NuxtLink
          v-for="link in links"
          :key="link.to"
          :to="link.to"
          class="flex items-center gap-3 rounded-md px-3 py-2.5 text-sm font-medium transition hover:bg-subtle hover:text-ink"
          :class="route.path === link.to ? 'bg-subtle text-ink' : 'text-muted'"
          :aria-current="route.path === link.to ? 'page' : undefined"
        >
          <component :is="link.icon" :size="17" aria-hidden="true" />
          <span>{{ link.label }}</span>
        </NuxtLink>
      </div>
    </nav>

    <div class="mt-auto flex items-center gap-2 border-t border-line px-3 pt-5 text-sm text-muted">
      <MapPin :size="16" class="text-accent" aria-hidden="true" />
      Lille
    </div>
  </aside>

  <header class="sticky top-0 z-30 border-b border-line bg-surface lg:hidden">
    <div class="flex h-14 items-center justify-between gap-4 px-4 sm:px-6">
      <NuxtLink to="/" class="flex items-center gap-2 text-base font-bold tracking-tight text-ink" aria-label="MovieFlow, accueil">
        <span class="grid size-8 place-items-center rounded-md bg-accent text-white">
          <Clapperboard :size="18" aria-hidden="true" />
        </span>
        <span>MovieFlow</span>
      </NuxtLink>
      <div class="flex items-center gap-1.5 text-sm text-muted">
        <MapPin :size="15" class="text-accent" aria-hidden="true" />
        Lille
      </div>
    </div>
    <nav aria-label="Navigation principale" class="flex border-t border-line px-2 sm:px-4">
      <NuxtLink
        v-for="link in links"
        :key="link.to"
        :to="link.to"
        class="flex min-h-11 flex-1 items-center justify-center gap-2 border-b-2 px-2 text-sm font-medium transition"
        :class="route.path === link.to ? 'border-accent text-ink' : 'border-transparent text-muted hover:text-ink'"
        :aria-current="route.path === link.to ? 'page' : undefined"
      >
        <component :is="link.icon" :size="16" aria-hidden="true" />
        <span>{{ link.label }}</span>
      </NuxtLink>
    </nav>
  </header>
</template>
