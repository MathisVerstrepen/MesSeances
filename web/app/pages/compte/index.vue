<script setup lang="ts">
import { Activity, Bell, ChevronRight, Settings, Eye } from '@lucide/vue'
import { unreadBadge } from '~/utils/notifications'
import { accountDestination } from '~/utils/accountState'

definePageMeta({ middleware: 'account-auth' })
const account = useAccountSession()
const { hasNew: hasNewActivity } = useActivityNotification()
const { unreadCount } = useNotificationCenter()
const complete = computed(() => account.session.value?.state === 'complete')
useHead({ title: 'Mon compte - MesSeances' })
</script>

<template>
  <AccountShell title="Mon compte" hide-explore hide-logout account-area>
    <nav v-if="complete" aria-label="Rubriques du compte">
      <ul>
        <li>
          <NuxtLink
            to="/compte/parametres"
            :prefetch="false"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary"
          >
            <Settings :size="20" class="shrink-0" aria-hidden="true" />
            Paramètres
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            to="/compte/watchlist"
            :prefetch="false"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2"
          >
            <WatchlistIcon :size="20" class="shrink-0" />Watchlist
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            to="/compte/films-vus"
            :prefetch="false"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2"
          >
            <Eye :size="20" class="shrink-0" aria-hidden="true" />Films vus
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            to="/compte/activite"
            :prefetch="false"
            :aria-description="hasNewActivity ? 'Nouvelle activité disponible' : undefined"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2"
          >
            <Activity :size="20" class="shrink-0" aria-hidden="true" />Activité
            <ActivityNotificationDot v-if="hasNewActivity" />
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
        <li>
          <NuxtLink
            to="/compte/notifications"
            :prefetch="false"
            :aria-description="unreadCount ? `${unreadCount} non lues` : undefined"
            class="flex min-h-12 items-center gap-3 py-4 text-lg font-semibold hover:text-primary focus-visible:outline-2 focus-visible:outline-offset-2"
          >
            <Bell :size="20" class="shrink-0" aria-hidden="true" />Notifications
            <span
              v-if="unreadCount"
              class="min-w-6 rounded-full bg-primary px-1.5 text-center font-mono text-xs font-bold leading-6 text-white"
              aria-hidden="true"
              >{{
                unreadBadge(unreadCount)
              }}</span
            >
            <ChevronRight
              :size="20"
              class="ml-auto shrink-0"
              aria-hidden="true"
            />
          </NuxtLink>
        </li>
      </ul>
    </nav>
    <div v-else class="space-y-4">
      <p class="text-sm">
        Votre session n’est plus active ou votre inscription reste à terminer.
      </p>
      <NuxtLink
        :to="account.session.value ? accountDestination(account.session.value) : '/connexion'"
        :prefetch="false"
        class="account-link"
        >Reprendre la connexion</NuxtLink
      >
    </div>
  </AccountShell>
</template>
