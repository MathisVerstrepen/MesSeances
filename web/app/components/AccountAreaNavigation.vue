<script setup lang="ts">
import { Bookmark, Settings, Users } from '@lucide/vue'

const route = useRoute()
const settingsActive = computed(
  () => route.path.toLowerCase().replace(/\/+$/, '') === '/compte/parametres',
)
const upcomingEntries = [
  { label: 'Watchlist', icon: Bookmark },
  { label: 'Amis', icon: Users },
]
</script>

<template>
  <nav
    aria-label="Espace personnel"
    class="account-area-navigation hidden min-w-0 border-r-2 border-ink bg-[#f1efe8] p-4 lg:block lg:py-10"
  >
    <ul class="grid gap-2">
      <li>
        <NuxtLink
          to="/compte/parametres"
          :prefetch="false"
          :aria-current="settingsActive ? 'page' : undefined"
          :class="settingsActive ? 'border-ink bg-ink text-white' : 'border-transparent hover:bg-ink/10'"
          class="flex h-full min-h-12 items-center border-l-4 px-3 py-3 text-sm font-semibold no-underline"
        >
          <span class="inline-flex items-center gap-2">
            <Settings :size="18" class="shrink-0" aria-hidden="true" />
            Paramètres
          </span>
        </NuxtLink>
      </li>
      <li v-for="entry in upcomingEntries" :key="entry.label">
        <button
          type="button"
          disabled
          class="flex min-h-12 w-full flex-col items-start justify-between gap-x-2 border-l-4 border-transparent px-3 py-2 text-left text-sm text-ink/70 disabled:cursor-not-allowed lg:flex-row lg:items-center lg:py-3"
        >
          <span class="inline-flex items-center gap-2">
            <component
              :is="entry.icon"
              :size="18"
              class="shrink-0"
              aria-hidden="true"
            />
            {{ entry.label }}
          </span>
          <span class="text-xs">À venir</span>
        </button>
      </li>
    </ul>
  </nav>
</template>
