<script setup lang="ts">
import { Bell } from '@lucide/vue'
import type { AccountNotification } from '~/types/notifications'
import {
  formatNotificationTime,
  presentNotification,
} from '~/utils/notifications'

const props = defineProps<{
  item: AccountNotification
  now: Date
  compact?: boolean
}>()
const emit = defineEmits<{ open: [item: AccountNotification] }>()
const view = computed(() => presentNotification(props.item))
const poster = computed(() => props.item.movies[0]?.poster_url ?? null)
</script>

<template>
  <NuxtLink
    :to="view.target"
    :prefetch="false"
    class="group flex min-h-16 items-start gap-3 border-l-4 px-3 py-3 text-left no-underline transition-colors hover:bg-highlight/40 focus-visible:outline-2 focus-visible:outline-offset-[-2px]"
    :class="item.read ? 'border-transparent' : 'border-primary bg-primary-soft/60'"
    :aria-description="item.read ? undefined : 'Non lue'"
    @click="emit('open', item)"
  >
    <span
      class="relative grid shrink-0 place-items-center overflow-hidden border-2 border-ink bg-subtle"
      :class="compact ? 'h-12 w-9' : 'h-16 w-12'"
      aria-hidden="true"
    >
      <img
        v-if="poster"
        :src="poster"
        alt=""
        loading="lazy"
        decoding="async"
        class="size-full object-cover"
      >
      <WatchlistIcon
        v-else-if="item.kind === 'watchlist_screening'"
        :size="18"
        class="text-ink/60"
      />
      <Bell v-else :size="18" class="text-ink/60" />
    </span>
    <span class="min-w-0 flex-1">
      <span
        class="flex flex-wrap items-baseline gap-x-2 font-mono text-[0.65rem] font-bold uppercase tracking-[0.08em] text-muted"
      >
        <span>{{ view.context }}</span>
        <time :datetime="item.created_at">{{
          formatNotificationTime(item.created_at, now)
        }}</time>
      </span>
      <span
        class="mt-0.5 block text-sm leading-snug [overflow-wrap:anywhere]"
        :class="item.read ? 'font-medium' : 'font-bold'"
        >{{
          view.headline
        }}</span
      >
      <span
        v-if="view.detail"
        class="mt-0.5 block text-xs leading-snug text-muted [overflow-wrap:anywhere]"
        :class="compact ? 'line-clamp-1' : 'line-clamp-2'"
        >{{
          view.detail
        }}</span
      >
    </span>
    <span
      v-if="!item.read"
      class="mt-1.5 inline-block size-2 shrink-0 rounded-full bg-primary"
      aria-hidden="true"
    />
  </NuxtLink>
</template>
