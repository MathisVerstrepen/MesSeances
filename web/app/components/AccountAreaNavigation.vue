<script setup lang="ts">
import { Activity, Bell, Settings, Users, Eye } from '@lucide/vue'
import { unreadBadge } from '~/utils/notifications'

const route = useRoute()
const { hasNew: hasNewActivity } = useActivityNotification()
const { unreadCount } = useNotificationCenter()
const notificationsActive = computed(
  () =>
    route.path.toLowerCase().replace(/\/+$/, '') === '/compte/notifications',
)
const settingsActive = computed(
  () => route.path.toLowerCase().replace(/\/+$/, '') === '/compte/parametres',
)
const watchlistActive = computed(
  () => route.path.toLowerCase().replace(/\/+$/, '') === '/compte/watchlist',
)
const upcomingEntries = [{ label: 'Amis', icon: Users }]
const watchedActive = computed(
  () => route.path.toLowerCase().replace(/\/+$/, '') === '/compte/films-vus',
)
const activityActive = computed(
  () => route.path.toLowerCase().replace(/\/+$/, '') === '/compte/activite',
)
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
      <li>
        <NuxtLink
          to="/compte/watchlist"
          :prefetch="false"
          :aria-current="watchlistActive ? 'page' : undefined"
          :class="watchlistActive ? 'border-ink bg-ink text-white' : 'border-transparent hover:bg-ink/10'"
          class="flex min-h-12 items-center gap-2 border-l-4 px-3 py-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
        >
          <WatchlistIcon :size="18" class="shrink-0" />Watchlist
        </NuxtLink>
      </li>
      <li>
        <NuxtLink
          to="/compte/films-vus"
          :prefetch="false"
          :aria-current="watchedActive ? 'page' : undefined"
          :class="watchedActive ? 'border-ink bg-ink text-white' : 'border-transparent hover:bg-ink/10'"
          class="flex min-h-12 items-center gap-2 border-l-4 px-3 py-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
        >
          <Eye :size="18" class="shrink-0" aria-hidden="true" />Films vus
        </NuxtLink>
      </li>
      <li>
        <NuxtLink
          to="/compte/activite"
          :prefetch="false"
          :aria-description="hasNewActivity ? 'Nouvelle activité disponible' : undefined"
          :aria-current="activityActive ? 'page' : undefined"
          :class="activityActive ? 'border-ink bg-ink text-white' : 'border-transparent hover:bg-ink/10'"
          class="flex min-h-12 items-center gap-2 border-l-4 px-3 py-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
        >
          <Activity :size="18" class="shrink-0" aria-hidden="true" />Activité
          <ActivityNotificationDot v-if="hasNewActivity" />
        </NuxtLink>
      </li>
      <li>
        <NuxtLink
          to="/compte/notifications"
          :prefetch="false"
          :aria-description="unreadCount ? `${unreadCount} non lues` : undefined"
          :aria-current="notificationsActive ? 'page' : undefined"
          :class="notificationsActive ? 'border-ink bg-ink text-white' : 'border-transparent hover:bg-ink/10'"
          class="flex min-h-12 items-center gap-2 border-l-4 px-3 py-3 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2"
        >
          <Bell :size="18" class="shrink-0" aria-hidden="true" />Notifications
          <span
            v-if="unreadCount"
            class="ml-auto min-w-5 rounded-full bg-primary px-1.5 text-center font-mono text-[0.65rem] font-bold leading-5 text-white"
            aria-hidden="true"
            >{{
              unreadBadge(unreadCount)
            }}</span
          >
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
