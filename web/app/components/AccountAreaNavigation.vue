<script setup lang="ts">
import { Bookmark, Settings, Users } from '@lucide/vue'

const upcomingEntries = [
  { label: 'Watchlist', icon: Bookmark },
  { label: 'Amis', icon: Users },
]
</script>

<template>
  <nav
    aria-label="Espace personnel"
    class="account-area-navigation min-w-0 border-b-2 border-ink bg-[#f1efe8] p-4 lg:border-r-2 lg:border-b-0 lg:py-10"
  >
    <ul class="grid grid-cols-2 gap-2 lg:grid-cols-1">
      <li>
        <NuxtLink
          to="/compte"
          :prefetch="false"
          aria-current="page"
          class="flex h-full min-h-12 items-center border-l-4 border-ink bg-ink px-3 py-3 text-sm font-semibold text-white no-underline"
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
