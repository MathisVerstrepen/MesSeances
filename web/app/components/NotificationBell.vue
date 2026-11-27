<script setup lang="ts">
import { Bell, LoaderCircle } from '@lucide/vue'
import type { AccountNotification } from '~/types/notifications'
import { unreadBadge } from '~/utils/notifications'

const PANEL_ITEMS = 6
const center = useNotificationCenter()
const route = useRoute()
const open = ref(false)
const now = ref(new Date())
const root = useTemplateRef('root')
const trigger = useTemplateRef('trigger')
const badge = computed(() => unreadBadge(center.unreadCount.value))
const visible = computed(() => center.items.value.slice(0, PANEL_ITEMS))
const hasUnreadVisible = computed(() =>
  center.items.value.some((item) => !item.read),
)
const label = computed(() =>
  center.unreadCount.value
    ? `Notifications, ${center.unreadCount.value} non ${center.unreadCount.value === 1 ? 'lue' : 'lues'}`
    : 'Notifications',
)

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) trigger.value?.focus({ preventScroll: true })
}
function toggle() {
  if (open.value) return close()
  now.value = new Date()
  open.value = true
  void center.load()
}
function select(item: AccountNotification) {
  void center.markRead(item)
  close()
}
function outside(event: PointerEvent) {
  const target = event.target
  if (open.value && !(target instanceof Node && root.value?.contains(target)))
    close()
}
function escape(event: KeyboardEvent) {
  if (open.value && event.key === 'Escape') close(true)
}

watch(
  () => route.fullPath,
  () => close(),
)
watch(center.owner, (owner) => {
  if (!owner) close()
})
onMounted(() => {
  document.addEventListener('pointerdown', outside)
  document.addEventListener('keydown', escape)
})
onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', outside)
  document.removeEventListener('keydown', escape)
})
</script>

<template>
  <div v-if="center.owner.value" ref="root" class="relative">
    <button
      ref="trigger"
      type="button"
      class="relative grid size-11 place-items-center border-2 border-transparent text-ink transition-colors hover:border-ink hover:bg-highlight aria-expanded:border-ink aria-expanded:bg-highlight"
      :aria-label="label"
      aria-controls="notification-panel"
      :aria-expanded="open"
      @click="toggle"
    >
      <Bell :size="19" stroke-width="2.5" aria-hidden="true" />
      <span
        v-if="badge"
        data-notification-badge
        class="absolute -top-0.5 -right-0.5 min-w-5 rounded-full border-2 border-canvas bg-primary px-1 text-center font-mono text-[0.6rem] font-bold leading-4 text-white"
        aria-hidden="true"
        >{{
          badge
        }}</span
      >
    </button>
    <section
      v-if="open"
      id="notification-panel"
      aria-label="Notifications récentes"
      class="absolute top-full right-0 z-40 mt-2 flex max-h-[min(32rem,calc(100dvh-6rem))] w-[min(24rem,calc(100vw-2rem))] flex-col border-2 border-ink bg-surface text-ink shadow-[6px_6px_0_#27272a]"
    >
      <header
        class="flex shrink-0 items-center justify-between gap-3 border-b-2 border-ink px-4 py-3"
      >
        <h2
          class="[font-family:'Noto_Sans_Variable',sans-serif] text-lg font-black tracking-[-0.035em]"
        >
          Notifications
        </h2>
        <button
          v-if="hasUnreadVisible"
          type="button"
          class="min-h-11 font-mono text-[0.68rem] font-bold uppercase underline decoration-2 underline-offset-4 hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="center.pendingRead.value"
          @click="center.markAllRead()"
        >
          Tout marquer comme lu
        </button>
      </header>
      <div
        class="min-h-0 flex-1 overflow-y-auto overscroll-contain"
        :aria-busy="center.loading.value"
      >
        <p
          v-if="center.loading.value && !center.items.value.length"
          role="status"
          class="flex items-center gap-2 p-4 text-sm"
        >
          <LoaderCircle
            :size="16"
            class="motion-safe:animate-spin"
            aria-hidden="true"
          />Chargement…
        </p>
        <div v-else-if="center.error.value" class="space-y-3 p-4">
          <p role="alert" class="editorial-alert p-3">
            {{ center.error.value }}
          </p>
          <button
            type="button"
            class="editorial-button-outline"
            @click="center.load(true)"
          >
            Réessayer
          </button>
        </div>
        <div v-else-if="!center.items.value.length" class="space-y-2 p-4">
          <p class="text-sm font-semibold">
            Aucune notification pour l’instant.
          </p>
          <p class="text-sm text-muted">
            Créez une alerte pour être prévenu des nouveautés dans vos cinémas
            ou de la programmation des films de votre watchlist.
          </p>
        </div>
        <ul v-else class="divide-y divide-ink/15">
          <li v-for="item in visible" :key="item.id">
            <NotificationListItem
              :item="item"
              :now="now"
              compact
              @open="select"
            />
          </li>
        </ul>
        <p
          v-if="center.actionError.value"
          role="alert"
          class="editorial-alert m-4 p-3"
        >
          {{ center.actionError.value }}
        </p>
      </div>
      <footer class="grid shrink-0 grid-cols-2 border-t-2 border-ink">
        <NuxtLink
          to="/compte/notifications"
          :prefetch="false"
          class="flex min-h-12 items-center justify-center border-r-2 border-ink px-3 font-mono text-[0.68rem] font-bold uppercase hover:bg-highlight"
          >Tout voir</NuxtLink
        >
        <NuxtLink
          to="/compte/notifications?onglet=alertes"
          :prefetch="false"
          class="flex min-h-12 items-center justify-center px-3 font-mono text-[0.68rem] font-bold uppercase hover:bg-highlight"
          >Gérer les alertes</NuxtLink
        >
      </footer>
    </section>
  </div>
</template>
